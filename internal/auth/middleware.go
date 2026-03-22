// Package auth provides JWT authentication middleware for Keycloak OIDC.
package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// contextKey is an unexported type for context keys in this package.
type contextKey string

// ClaimsKey is the context key for JWT claims.
const ClaimsKey contextKey = "auth_claims"

// Claims represents the JWT claims extracted from a Keycloak access token.
type Claims struct {
	Sub               string `json:"sub"`
	PreferredUsername  string `json:"preferred_username"`
	Email             string `json:"email"`
	EmailVerified     bool   `json:"email_verified"`
	Name              string `json:"name"`
	GivenName         string `json:"given_name"`
	FamilyName        string `json:"family_name"`
	jwt.RegisteredClaims
}

// jwksKey represents a single JWK from the JWKS endpoint.
type jwksKey struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// jwksResponse represents the response from the JWKS endpoint.
type jwksResponse struct {
	Keys []jwksKey `json:"keys"`
}

// Middleware holds the JWKS cache and provides JWT validation.
type Middleware struct {
	keycloakURL string
	realm       string
	issuer      string
	jwksURL     string
	logger      *slog.Logger

	mu          sync.RWMutex
	keys        map[string]*rsa.PublicKey
	lastFetched time.Time
	cacheTTL    time.Duration

	// publicPrefixes are route prefixes that skip authentication.
	publicPrefixes []string
}

// New creates a new auth middleware that validates Keycloak JWT tokens.
func New(keycloakURL, realm string, logger *slog.Logger) *Middleware {
	// Keycloak issuer URL uses the external-facing base URL
	issuer := fmt.Sprintf("%s/realms/%s", keycloakURL, realm)
	jwksURL := fmt.Sprintf("%s/realms/%s/protocol/openid-connect/certs", keycloakURL, realm)

	return &Middleware{
		keycloakURL: keycloakURL,
		realm:       realm,
		issuer:      issuer,
		jwksURL:     jwksURL,
		logger:      logger,
		keys:        make(map[string]*rsa.PublicKey),
		cacheTTL:    1 * time.Hour,
		publicPrefixes: []string{
			"/health",
			"/webhooks/",
		},
	}
}

// Handler wraps an http.Handler with JWT authentication.
func (m *Middleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for public routes
		if m.isPublicRoute(r) {
			next.ServeHTTP(w, r)
			return
		}

		// Extract token from Authorization header or query param (for SSE)
		tokenStr := m.extractToken(r)
		if tokenStr == "" {
			m.writeUnauthorized(w, "missing authorization token")
			return
		}

		// Parse and validate the JWT
		claims, err := m.validateToken(tokenStr)
		if err != nil {
			m.logger.Debug("JWT validation failed", "error", err, "path", r.URL.Path)
			m.writeUnauthorized(w, "invalid or expired token")
			return
		}

		// Store claims in request context
		ctx := context.WithValue(r.Context(), ClaimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// isPublicRoute checks if the request path matches a public prefix.
func (m *Middleware) isPublicRoute(r *http.Request) bool {
	path := r.URL.Path

	// Root endpoint is public
	if path == "/" {
		return true
	}

	// OPTIONS requests (CORS preflight) are always public
	if r.Method == http.MethodOptions {
		return true
	}

	for _, prefix := range m.publicPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// extractToken gets the JWT from the Authorization header or query param.
func (m *Middleware) extractToken(r *http.Request) string {
	// Try Authorization header first
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimPrefix(authHeader, "Bearer ")
	}

	// Fallback to query param (for SSE EventSource which can't set headers)
	if token := r.URL.Query().Get("token"); token != "" {
		return token
	}

	return ""
}

// validateToken parses and validates a JWT against the Keycloak JWKS.
func (m *Middleware) validateToken(tokenStr string) (*Claims, error) {
	claims := &Claims{}

	token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (any, error) {
		// Ensure the signing method is RSA
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, fmt.Errorf("missing kid in token header")
		}

		key, err := m.getKey(kid)
		if err != nil {
			return nil, err
		}

		return key, nil
	}, jwt.WithIssuer(m.issuer), jwt.WithExpirationRequired())

	if err != nil {
		return nil, fmt.Errorf("token validation failed: %w", err)
	}

	if !token.Valid {
		return nil, fmt.Errorf("token is not valid")
	}

	return claims, nil
}

// getKey returns the RSA public key for the given kid, fetching JWKS if needed.
func (m *Middleware) getKey(kid string) (*rsa.PublicKey, error) {
	m.mu.RLock()
	key, found := m.keys[kid]
	expired := time.Since(m.lastFetched) > m.cacheTTL
	m.mu.RUnlock()

	if found && !expired {
		return key, nil
	}

	// Fetch fresh JWKS
	if err := m.fetchJWKS(); err != nil {
		// If we have a cached key, use it even if cache is expired
		if found {
			m.logger.Warn("JWKS fetch failed, using cached key", "kid", kid, "error", err)
			return key, nil
		}
		return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
	}

	m.mu.RLock()
	key, found = m.keys[kid]
	m.mu.RUnlock()

	if !found {
		return nil, fmt.Errorf("key not found for kid: %s", kid)
	}

	return key, nil
}

// fetchJWKS retrieves the JSON Web Key Set from Keycloak.
func (m *Middleware) fetchJWKS() error {
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Get(m.jwksURL)
	if err != nil {
		return fmt.Errorf("JWKS request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("JWKS endpoint returned status %d", resp.StatusCode)
	}

	var jwks jwksResponse
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return fmt.Errorf("failed to decode JWKS: %w", err)
	}

	newKeys := make(map[string]*rsa.PublicKey)
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" || k.Use != "sig" {
			continue
		}

		pubKey, err := parseRSAPublicKey(k.N, k.E)
		if err != nil {
			m.logger.Warn("failed to parse JWKS key", "kid", k.Kid, "error", err)
			continue
		}

		newKeys[k.Kid] = pubKey
	}

	m.mu.Lock()
	m.keys = newKeys
	m.lastFetched = time.Now()
	m.mu.Unlock()

	m.logger.Debug("JWKS refreshed", "keyCount", len(newKeys))
	return nil
}

// parseRSAPublicKey creates an RSA public key from base64url-encoded n and e values.
func parseRSAPublicKey(nStr, eStr string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nStr)
	if err != nil {
		return nil, fmt.Errorf("failed to decode modulus: %w", err)
	}

	eBytes, err := base64.RawURLEncoding.DecodeString(eStr)
	if err != nil {
		return nil, fmt.Errorf("failed to decode exponent: %w", err)
	}

	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes)

	return &rsa.PublicKey{
		N: n,
		E: int(e.Int64()),
	}, nil
}

// writeUnauthorized sends a 401 JSON response.
func (m *Middleware) writeUnauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]any{
		"error":   "unauthorized",
		"message": message,
	})
}

// GetClaims extracts auth claims from the request context.
func GetClaims(ctx context.Context) *Claims {
	claims, _ := ctx.Value(ClaimsKey).(*Claims)
	return claims
}
