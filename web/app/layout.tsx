import type { Metadata } from "next";
import "./globals.css";
import Sidebar from "./sidebar";
import { AuthProvider } from "./providers";

export const metadata: Metadata = {
  title: "Uni AI Studio — AI Filmmaking",
  description: "Multi-agent AI filmmaking platform",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="vi" className="dark">
      <head>
        <link
          href="https://fonts.googleapis.com/css2?family=Plus+Jakarta+Sans:wght@300;400;500;600;700;800&family=JetBrains+Mono:wght@400;500;600&display=swap"
          rel="stylesheet"
        />
      </head>
      <body className="antialiased">
        <AuthProvider>
          <div style={{ display: "flex", minHeight: "100vh" }}>
            <Sidebar />
            <main className="main-content" style={{ flex: 1 }}>
              {children}
            </main>
          </div>
        </AuthProvider>
      </body>
    </html>
  );
}
