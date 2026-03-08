"use client";
import Link from "next/link";
import { usePathname } from "next/navigation";

const navItems = [
  { href: "/", icon: "◆", label: "Dashboard" },
  { href: "/agents", icon: "⬡", label: "Agents" },
  { href: "/tools", icon: "⚡", label: "Tools" },
  { href: "/new", icon: "▶", label: "New Project" },
  { href: "/settings", icon: "⚙", label: "Settings" },
];

export default function Sidebar() {
  const pathname = usePathname();

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
              Server Connected
            </div>
            <div style={{ fontSize: "10px", fontFamily: "'JetBrains Mono', monospace", color: "#5c6079" }}>
              v0.3.0
            </div>
          </div>
        </div>
      </div>
    </aside>
  );
}
