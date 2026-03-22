import Keycloak from 'keycloak-js';

// Keycloak singleton instance
let keycloak: Keycloak | null = null;
let initPromise: Promise<boolean> | null = null;

const KEYCLOAK_URL = process.env.NEXT_PUBLIC_KEYCLOAK_URL || 'http://localhost:8180';
const KEYCLOAK_REALM = process.env.NEXT_PUBLIC_KEYCLOAK_REALM || 'waoo-studio';
const KEYCLOAK_CLIENT_ID = process.env.NEXT_PUBLIC_KEYCLOAK_CLIENT_ID || 'waoo-web';

// Minimum token validity in seconds before auto-refresh
const MIN_TOKEN_VALIDITY = 30;

/**
 * Get or create the Keycloak instance.
 */
export function getKeycloakInstance(): Keycloak {
  if (!keycloak) {
    keycloak = new Keycloak({
      url: KEYCLOAK_URL,
      realm: KEYCLOAK_REALM,
      clientId: KEYCLOAK_CLIENT_ID,
    });
  }
  return keycloak;
}

/**
 * Initialize Keycloak with login-required.
 * Returns true if authenticated, false otherwise.
 * Calling multiple times returns the same promise (singleton).
 */
export function initKeycloak(): Promise<boolean> {
  if (initPromise) return initPromise;

  const kc = getKeycloakInstance();

  initPromise = kc.init({
    onLoad: 'login-required',
    checkLoginIframe: false,
    pkceMethod: 'S256',
  }).then((authenticated) => {
    if (authenticated) {
      // Set up auto token refresh
      setupTokenRefresh(kc);
    }
    return authenticated;
  }).catch((err) => {
    console.error('Keycloak init failed:', err);
    initPromise = null; // Allow retry
    return false;
  });

  return initPromise;
}

/**
 * Get the current access token. Returns empty string if not authenticated.
 * Synchronous — uses the cached token from Keycloak.
 */
export function getToken(): string {
  return keycloak?.token ?? '';
}

/**
 * Refresh the token if it expires within MIN_TOKEN_VALIDITY seconds.
 * Returns the fresh token or empty string on failure.
 */
export async function refreshToken(): Promise<string> {
  if (!keycloak?.authenticated) return '';

  try {
    const refreshed = await keycloak.updateToken(MIN_TOKEN_VALIDITY);
    if (refreshed) {
      console.debug('[Keycloak] Token refreshed');
    }
    return keycloak.token ?? '';
  } catch {
    console.warn('[Keycloak] Token refresh failed, redirecting to login');
    keycloak.login();
    return '';
  }
}

/**
 * Logout — redirects to Keycloak logout page.
 */
export function logout(): void {
  const redirectUri = typeof window !== 'undefined' ? window.location.origin : undefined;
  keycloak?.logout({ redirectUri });
}

/**
 * Get user info from the token.
 */
export function getUserInfo(): { username: string; email: string; name: string } | null {
  if (!keycloak?.tokenParsed) return null;

  const parsed = keycloak.tokenParsed as Record<string, unknown>;
  return {
    username: (parsed.preferred_username as string) ?? '',
    email: (parsed.email as string) ?? '',
    name: (parsed.name as string) ?? '',
  };
}

/**
 * Set up periodic token refresh.
 */
function setupTokenRefresh(kc: Keycloak): void {
  // Refresh token every 60 seconds
  const interval = setInterval(async () => {
    if (!kc.authenticated) {
      clearInterval(interval);
      return;
    }

    try {
      await kc.updateToken(MIN_TOKEN_VALIDITY);
    } catch {
      console.warn('[Keycloak] Periodic token refresh failed');
      clearInterval(interval);
      kc.login();
    }
  }, 60_000);

  // Handle token expiry event
  kc.onTokenExpired = () => {
    console.debug('[Keycloak] Token expired, refreshing...');
    kc.updateToken(MIN_TOKEN_VALIDITY).catch(() => {
      kc.login();
    });
  };

  // Handle auth error
  kc.onAuthError = (error) => {
    console.error('[Keycloak] Auth error:', error);
  };
}
