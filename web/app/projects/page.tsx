"use client";
import { useEffect, useState } from "react";
import Link from "next/link";
import { api, type ProjectSummary } from "@/lib/api";

function statusColor(status: string) {
  switch (status) {
    case "completed": return { text: "#34d399", bg: "rgba(52,211,153,0.1)", border: "rgba(52,211,153,0.2)" };
    case "running":   return { text: "#a78bfa", bg: "rgba(139,92,246,0.1)", border: "rgba(139,92,246,0.2)" };
    case "failed":    return { text: "#f87171", bg: "rgba(239,68,68,0.1)",  border: "rgba(239,68,68,0.2)"  };
    default:          return { text: "#9ca3af", bg: "rgba(156,163,175,0.1)",border: "rgba(156,163,175,0.2)"};
  }
}

function ProgressBar({ completed, total }: { completed: number; total: number }) {
  const pct = total > 0 ? (completed / total) * 100 : 0;
  return (
    <div style={{ marginTop: 12 }}>
      <div style={{ display: "flex", justifyContent: "space-between", marginBottom: 4 }}>
        <span style={{ fontSize: 10, color: "#6b7280" }}>Progress</span>
        <span style={{ fontSize: 10, color: "#9ca3af", fontFamily: "monospace" }}>
          {completed}/{total}
        </span>
      </div>
      <div style={{ height: 4, background: "rgba(255,255,255,0.06)", borderRadius: 4, overflow: "hidden" }}>
        <div style={{
          height: "100%",
          background: pct === 100 ? "linear-gradient(90deg, #34d399, #059669)" : "linear-gradient(90deg, #7c3aed, #a21caf)",
          width: `${pct}%`,
          borderRadius: 4,
          transition: "width 0.5s",
        }} />
      </div>
    </div>
  );
}

export default function ProjectsPage() {
  const [projects, setProjects] = useState<ProjectSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    api.projects.list()
      .then(({ projects: p }) => setProjects(p || []))
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  }, []);

  return (
    <div style={{ padding: "32px", maxWidth: 1200, margin: "0 auto" }}>
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: 32 }}>
        <div>
          <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 4 }}>
            <Link href="/" style={{ color: "#6b7280", fontSize: 13, textDecoration: "none" }}>← Dashboard</Link>
          </div>
          <h1 style={{ fontSize: 24, fontWeight: 700, marginBottom: 4 }}>Projects</h1>
          <p style={{ fontSize: 13, color: "#6b7280" }}>Lịch sử pipeline của tất cả dự án</p>
        </div>
        <Link
          href="/new"
          style={{
            padding: "8px 20px", borderRadius: 8, fontSize: 13, fontWeight: 600,
            background: "rgba(124,58,237,0.15)", border: "1px solid rgba(124,58,237,0.3)",
            color: "#a78bfa", textDecoration: "none",
          }}
        >
          + New Project
        </Link>
      </div>

      {error && (
        <div style={{
          marginBottom: 24, padding: 14, borderRadius: 10, fontSize: 13,
          background: "rgba(239,68,68,0.08)", border: "1px solid rgba(239,68,68,0.15)", color: "#ef4444",
        }}>
          {error}
        </div>
      )}

      {loading && (
        <div style={{ textAlign: "center", color: "#6b7280", padding: 60 }}>Loading...</div>
      )}

      {!loading && projects.length === 0 && !error && (
        <div style={{
          textAlign: "center", padding: 60, color: "#4b5563",
          background: "rgba(255,255,255,0.02)", borderRadius: 12,
          border: "1px solid rgba(255,255,255,0.06)",
        }}>
          <div style={{ fontSize: 40, marginBottom: 12 }}>🎬</div>
          <p>Chưa có project nào.</p>
          <Link href="/new" style={{ color: "#a78bfa", marginTop: 8, display: "inline-block" }}>
            Tạo project đầu tiên →
          </Link>
        </div>
      )}

      <div style={{ display: "grid", gridTemplateColumns: "repeat(2, 1fr)", gap: 16 }}>
        {projects.map((p) => {
          const col = statusColor(p.overallStatus);
          return (
            <Link
              key={p.projectId}
              href={`/project/${p.projectId}`}
              style={{ textDecoration: "none", color: "inherit" }}
            >
              <div
                className="glass-card"
                style={{ padding: 20, cursor: "pointer", transition: "border-color 0.2s" }}
              >
                <div style={{ display: "flex", alignItems: "flex-start", justifyContent: "space-between", marginBottom: 8 }}>
                  <p style={{ fontFamily: "monospace", fontSize: 12, color: "#9ca3af", wordBreak: "break-all", flex: 1 }}>
                    {p.projectId}
                  </p>
                  <span style={{
                    marginLeft: 12, padding: "2px 10px", borderRadius: 20, fontSize: 11, fontWeight: 600,
                    whiteSpace: "nowrap", background: col.bg, border: `1px solid ${col.border}`, color: col.text,
                  }}>
                    {p.overallStatus}
                  </span>
                </div>

                <ProgressBar completed={p.completedStages} total={p.totalStages} />

                <div style={{ display: "flex", gap: 16, marginTop: 12 }}>
                  {p.failedStages > 0 && (
                    <span style={{ fontSize: 11, color: "#f87171" }}>
                      ✗ {p.failedStages} failed
                    </span>
                  )}
                  <span style={{ fontSize: 11, color: "#6b7280", marginLeft: "auto" }}>
                    {p.startedAt ? new Date(p.startedAt).toLocaleString() : "—"}
                  </span>
                </div>
              </div>
            </Link>
          );
        })}
      </div>
    </div>
  );
}
