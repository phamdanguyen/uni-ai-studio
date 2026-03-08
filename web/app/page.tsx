"use client";
import { useEffect, useState } from "react";
import { api, type HealthStatus, type AgentHealth } from "@/lib/api";
import Link from "next/link";

export default function Dashboard() {
  const [health, setHealth] = useState<HealthStatus | null>(null);
  const [agents, setAgents] = useState<AgentHealth[]>([]);
  const [error, setError] = useState("");

  useEffect(() => {
    Promise.all([api.health(), api.agents.health()])
      .then(([h, a]) => {
        setHealth(h);
        setAgents(a.health || []);
      })
      .catch((e) => setError(e.message));
  }, []);

  return (
    <div style={{ padding: "32px", maxWidth: 1200 }}>
      {/* Hero */}
      <div className="anim-in" style={{ marginBottom: 40 }}>
        <h1 style={{ fontSize: 28, fontWeight: 700, marginBottom: 6, letterSpacing: "-0.02em" }}>
          <span className="text-gradient">Uni AI Studio</span>
        </h1>
        <p style={{ color: "#8b8fa3", fontSize: 15 }}>
          Multi-Agent Filmmaking Platform — Từ kịch bản đến phim hoàn chỉnh
        </p>
      </div>

      {error && (
        <div className="anim-in" style={{
          marginBottom: 24, padding: 14, borderRadius: 12, fontSize: 13,
          background: "rgba(239,68,68,0.08)", border: "1px solid rgba(239,68,68,0.15)", color: "#ef4444",
        }}>
          {error}
        </div>
      )}

      {/* Status Row */}
      <div style={{ display: "grid", gridTemplateColumns: "repeat(4, 1fr)", gap: 16, marginBottom: 40 }}>
        <MetricCard label="Server" value={health?.status === "ok" ? "Online" : "Offline"}
          dotClass={health?.status === "ok" ? "online" : "error"} />
        <MetricCard label="Version" value={health?.version || "—"} />
        <MetricCard label="Database" value={health?.database || "—"}
          dotClass={health?.database === "connected" ? "online" : "warning"} />
        <MetricCard label="Pending Tasks" value={String(health?.pending ?? 0)} mono />
      </div>

      {/* Agent Health */}
      <section style={{ marginBottom: 40 }}>
        <div className="section-header">
          <span className="icon">⬡</span> Agent Health
        </div>
        <div style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 16 }}>
          {agents.map((a) => (
            <div key={a.name} className="glass-card" style={{ padding: 20 }}>
              <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: 16 }}>
                <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
                  <span style={{ fontSize: 18 }}>{agentIcon(a.name)}</span>
                  <span style={{ fontWeight: 600, fontSize: 14, textTransform: "capitalize" }}>{a.name}</span>
                </div>
                <span className={`badge-status ${a.status}`}>{a.status}</span>
              </div>
              <div style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 12 }}>
                <StatCell label="Tasks" value={String(a.tasksHandled)} />
                <StatCell label="Errors" value={String(a.tasksFailed)} isError={a.tasksFailed > 0} />
                <StatCell label="Latency" value={`${a.avgLatencyMs.toFixed(0)}ms`} />
              </div>
            </div>
          ))}
        </div>
      </section>

      {/* Quick Actions */}
      <section>
        <div className="section-header">
          <span className="icon">⚡</span> Quick Actions
        </div>
        <div style={{ display: "grid", gridTemplateColumns: "repeat(5, 1fr)", gap: 16 }}>
          <ActionCard href="/new" icon="▶" title="New Project" desc="Bắt đầu tạo phim AI" />
          <ActionCard href="/projects" icon="🎬" title="Projects" desc="Xem tất cả dự án" />
          <ActionCard href="/agents" icon="⬡" title="Agent Explorer" desc="6 agents • 22 skills" />
          <ActionCard href="/tools" icon="⚡" title="Tools Gallery" desc="13 AI generators" />
          <ActionCard href="/settings" icon="⚙" title="LLM Settings" desc="Cấu hình model & provider" />
        </div>
      </section>
    </div>
  );
}

function MetricCard({ label, value, dotClass, mono }: {
  label: string; value: string; dotClass?: string; mono?: boolean;
}) {
  return (
    <div className="glass-card" style={{ padding: 16 }}>
      <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 8 }}>
        {dotClass && <div className={`status-dot ${dotClass}`} />}
        <span style={{ fontSize: 10, fontWeight: 600, textTransform: "uppercase", letterSpacing: "0.08em", color: "#5c6079" }}>
          {label}
        </span>
      </div>
      <div style={{ fontSize: 22, fontWeight: 700, fontFamily: mono ? "'JetBrains Mono', monospace" : undefined }}>
        {value}
      </div>
    </div>
  );
}

function StatCell({ label, value, isError }: { label: string; value: string; isError?: boolean }) {
  return (
    <div>
      <div style={{ fontSize: 10, textTransform: "uppercase", letterSpacing: "0.08em", color: "#5c6079", marginBottom: 4 }}>
        {label}
      </div>
      <div style={{ fontSize: 13, fontWeight: 600, fontFamily: "'JetBrains Mono', monospace", color: isError ? "#ef4444" : "#f0f0f5" }}>
        {value}
      </div>
    </div>
  );
}

function ActionCard({ href, icon, title, desc }: { href: string; icon: string; title: string; desc: string }) {
  return (
    <Link href={href} className="glass-card" style={{ padding: 20, display: "block", textDecoration: "none", color: "inherit" }}>
      <div style={{
        fontSize: 16, marginBottom: 12, width: 36, height: 36, borderRadius: 10,
        display: "flex", alignItems: "center", justifyContent: "center",
        background: "rgba(245,178,64,0.1)", border: "1px solid rgba(245,178,64,0.2)", color: "#f5b240",
      }}>
        {icon}
      </div>
      <div style={{ fontWeight: 600, fontSize: 14, marginBottom: 4 }}>{title}</div>
      <div style={{ fontSize: 12, color: "#5c6079" }}>{desc}</div>
    </Link>
  );
}

function agentIcon(name: string): string {
  const icons: Record<string, string> = {
    director: "🎬", character: "👤", location: "🏔",
    storyboard: "🎞", media: "🖼", voice: "🎤",
  };
  return icons[name] || "🤖";
}
