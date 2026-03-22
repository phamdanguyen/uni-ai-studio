"use client";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useAuth } from "./providers";

const navItems = [
  { href: "/", icon: "◆", label: "Dashboard" },
  { href: "/agents", icon: "⬡", label: "Agents" },
  { href: "/tools", icon: "⚡", label: "Tools" },
  { href: "/new", icon: "▶", label: "New Project" },
  { href: "/settings", icon: "⚙", label: "Settings" },
];

export default function Sidebar() {
  const pathname = usePathname();
  const { username, logout } = useAuth();

  return (
    <aside className="sidebar">
      {/* Logo */}
      <div className="sidebar-logo">
        <div className="sidebar-logo-icon">U</div>
        <div>
          <div className="sidebar-logo-text">Uni AI Studio</div>
          <div className="sidebar-subtitle">Filmmaking AI</div>
        </div>
      </div>

      <div className="sidebar-divider" />
      <div className="sidebar-section-label">Main</div>

      {/* Nav Items */}
      <nav className="sidebar-nav">
        {navItems.map((item) => {
          const isActive = pathname === item.href;
          return (
            <Link
              key={item.href}
              href={item.href}
              className={`nav-link ${isActive ? "active" : ""}`}
            >
              <span className="nav-link-icon">{item.icon}</span>
              <span>{item.label}</span>
            </Link>
          );
        })}
      </nav>

      {/* Bottom Status */}
      <div className="sidebar-footer">
        <div style={{ display: "flex", alignItems: "center", gap: "10px" }}>
          <div className="status-dot online" />
          <div>
            <div style={{ fontSize: "12px", fontWeight: 500, color: "#8b8fa3" }}>
              {username || "Server Connected"}
            </div>
            <div style={{ fontSize: "10px", fontFamily: "'JetBrains Mono', monospace", color: "#5c6079" }}>
              v0.3.0
            </div>
          </div>
        </div>
        <button
          onClick={logout}
          style={{
            marginTop: "12px",
            width: "100%",
            padding: "8px 0",
            background: "rgba(255, 255, 255, 0.04)",
            border: "1px solid rgba(255, 255, 255, 0.06)",
            borderRadius: "8px",
            color: "#8b8fa3",
            fontSize: "12px",
            fontWeight: 500,
            cursor: "pointer",
            transition: "all 0.2s",
            fontFamily: "'Plus Jakarta Sans', sans-serif",
          }}
          onMouseEnter={(e) => {
            e.currentTarget.style.background = "rgba(245, 178, 64, 0.1)";
            e.currentTarget.style.color = "#f5b240";
            e.currentTarget.style.borderColor = "rgba(245, 178, 64, 0.2)";
          }}
          onMouseLeave={(e) => {
            e.currentTarget.style.background = "rgba(255, 255, 255, 0.04)";
            e.currentTarget.style.color = "#8b8fa3";
            e.currentTarget.style.borderColor = "rgba(255, 255, 255, 0.06)";
          }}
        >
          Logout
        </button>
      </div>
    </aside>
  );
}
