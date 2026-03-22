"use client";

import { createContext, useContext, useEffect, useState, useCallback } from "react";
import { initKeycloak, logout as kcLogout, getUserInfo } from "@/lib/keycloak";

interface AuthContextValue {
  authenticated: boolean;
  username: string;
  logout: () => void;
}

const AuthContext = createContext<AuthContextValue>({
  authenticated: false,
  username: "",
  logout: () => {},
});

export function useAuth() {
  return useContext(AuthContext);
}

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [authenticated, setAuthenticated] = useState(false);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    initKeycloak().then((auth) => {
      setAuthenticated(auth);
      setLoading(false);
    });
  }, []);

  const handleLogout = useCallback(() => {
    kcLogout();
  }, []);

  const userInfo = authenticated ? getUserInfo() : null;

  if (loading) {
    return (
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          minHeight: "100vh",
          background: "#0c0e14",
          color: "#8b8fa3",
          fontFamily: "'Plus Jakarta Sans', sans-serif",
        }}
      >
        <div style={{ textAlign: "center" }}>
          <div
            style={{
              width: 40,
              height: 40,
              border: "3px solid #1e2030",
              borderTop: "3px solid #f5b240",
              borderRadius: "50%",
              animation: "spin-slow 1s linear infinite",
              margin: "0 auto 16px",
            }}
          />
          <div style={{ fontSize: 14 }}>Authenticating...</div>
        </div>
      </div>
    );
  }

  return (
    <AuthContext.Provider
      value={{
        authenticated,
        username: userInfo?.username ?? "",
        logout: handleLogout,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
}
