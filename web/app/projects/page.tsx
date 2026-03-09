"use client";
import { useEffect, useState } from "react";
import Link from "next/link";
import { api, type ProjectSummary } from "@/lib/api";

const STAGE_TOTAL = 9;

function statusMeta(status: string) {
  switch (status) {
    case "completed": return { label: "Completed", color: "#34d399", glow: "rgba(52,211,153,0.15)", dot: "#34d399" };
    case "running":   return { label: "Running",   color: "#a78bfa", glow: "rgba(167,139,250,0.15)", dot: "#a78bfa" };
    case "failed":    return { label: "Failed",    color: "#f87171", glow: "rgba(248,113,113,0.15)", dot: "#f87171" };
    default:          return { label: "Pending",   color: "#4b5563", glow: "rgba(75,85,99,0.10)",   dot: "#374151" };
  }
}

function timeAgo(iso?: string): string {
  if (!iso) return "—";
  const ms = Date.now() - new Date(iso).getTime();
  const m = Math.floor(ms / 60000);
  if (m < 1) return "just now";
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}

function duration(start?: string, end?: string): string {
  if (!start) return "—";
  const ms = new Date(end || new Date()).getTime() - new Date(start).getTime();
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  return `${m}m ${s % 60}s`;
}

function StageStrip({ completed, total, failed, status }: {
  completed: number; total: number; failed: number; status: string;
}) {
  const segments = Array.from({ length: total }, (_, i) => {
    if (i < completed - failed) return "done";
    if (failed > 0 && i === completed - 1) return "fail";
    if (status === "running" && i === completed) return "run";
    return "idle";
  });

  return (
    <div style={{ display: "flex", gap: 3, marginTop: 14 }}>
      {segments.map((s, i) => (
        <div key={i} style={{
          flex: 1, height: 4, borderRadius: 2,
          background:
            s === "done" ? "#34d399" :
            s === "fail" ? "#f87171" :
            s === "run"  ? "#a78bfa" : "rgba(255,255,255,0.06)",
          boxShadow: s === "run" ? "0 0 6px #a78bfa" : undefined,
          transition: "all 0.4s",
        }} />
      ))}
    </div>
  );
}

function ProjectCard({ project }: { project: ProjectSummary }) {
  const meta = statusMeta(project.overallStatus);
  const pct = project.totalStages > 0
    ? Math.round((project.completedStages / project.totalStages) * 100)
    : 0;
  const shortId = project.projectId.slice(0, 8).toUpperCase();

  return (
    <Link href={`/project/${project.projectId}`} style={{ textDecoration: "none", color: "inherit" }}>
      <div style={{
        background: "#161a26",
        border: `1px solid ${meta.color}22`,
        borderRadius: 16,
        padding: "20px 22px",
        cursor: "pointer",
        transition: "all 0.25s cubic-bezier(0.4,0,0.2,1)",
        position: "relative",
        overflow: "hidden",
      }}
        onMouseEnter={e => {
          (e.currentTarget as HTMLDivElement).style.background = "#1c2030";
          (e.currentTarget as HTMLDivElement).style.borderColor = `${meta.color}44`;
          (e.currentTarget as HTMLDivElement).style.transform = "translateY(-2px)";
          (e.currentTarget as HTMLDivElement).style.boxShadow = `0 8px 32px rgba(0,0,0,0.4), 0 0 0 1px ${meta.color}22`;
        }}
        onMouseLeave={e => {
          (e.currentTarget as HTMLDivElement).style.background = "#161a26";
          (e.currentTarget as HTMLDivElement).style.borderColor = `${meta.color}22`;
          (e.currentTarget as HTMLDivElement).style.transform = "translateY(0)";
          (e.currentTarget as HTMLDivElement).style.boxShadow = "none";
        }}
      >
        {/* Glow accent */}
        <div style={{
          position: "absolute", top: 0, right: 0,
          width: 120, height: 120,
          background: `radial-gradient(circle at 100% 0%, ${meta.glow}, transparent 70%)`,
          pointerEvents: "none",
        }} />

        {/* Top row */}
        <div style={{ display: "flex", alignItems: "flex-start", justifyContent: "space-between", marginBottom: 14 }}>
          <div>
            {/* Film ID badge */}
            <div style={{
              display: "inline-flex", alignItems: "center", gap: 6,
              background: "rgba(255,255,255,0.04)", border: "1px solid rgba(255,255,255,0.08)",
              borderRadius: 6, padding: "3px 8px", marginBottom: 8,
            }}>
              <span style={{ fontSize: 9, color: "#f5b240", letterSpacing: "0.12em", fontWeight: 700 }}>FILM</span>
              <span style={{ fontSize: 10, color: "#6b7280", fontFamily: "monospace" }}>{shortId}</span>
            </div>
            <div style={{
              fontSize: 11, color: "#9ca3af", fontFamily: "monospace",
              wordBreak: "break-all", maxWidth: 280, lineHeight: 1.4,
            }}>
              {project.projectId}
            </div>
          </div>

          {/* Status badge */}
          <div style={{
            display: "flex", flexDirection: "column", alignItems: "flex-end", gap: 4, flexShrink: 0,
          }}>
            <div style={{
              display: "flex", alignItems: "center", gap: 6,
              background: meta.glow, border: `1px solid ${meta.color}33`,
              borderRadius: 20, padding: "4px 12px",
            }}>
              {project.overallStatus === "running" && (
                <div style={{
                  width: 5, height: 5, borderRadius: "50%",
                  background: meta.color, animation: "pulse 1.4s infinite",
                }} />
              )}
              <span style={{ fontSize: 11, fontWeight: 600, color: meta.color, letterSpacing: "0.05em" }}>
                {meta.label.toUpperCase()}
              </span>
            </div>
            <span style={{ fontSize: 10, color: "#374151", fontFamily: "monospace" }}>
              {pct}%
            </span>
          </div>
        </div>

        {/* Stage strip */}
        <StageStrip
          completed={project.completedStages}
          total={project.totalStages || STAGE_TOTAL}
          failed={project.failedStages}
          status={project.overallStatus}
        />

        {/* Bottom meta */}
        <div style={{ display: "flex", alignItems: "center", gap: 16, marginTop: 14 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 12, flex: 1 }}>
            <MetaPill icon="◈" label="Stages" value={`${project.completedStages}/${project.totalStages || STAGE_TOTAL}`} />
            {project.failedStages > 0 && (
              <MetaPill icon="✗" label="Failed" value={String(project.failedStages)} color="#f87171" />
            )}
            <MetaPill icon="⏱" label="Duration" value={duration(project.startedAt, project.finishedAt)} />
          </div>
          <span style={{ fontSize: 10, color: "#374151", letterSpacing: "0.04em" }}>
            {timeAgo(project.startedAt)}
          </span>
        </div>
      </div>
    </Link>
  );
}

function MetaPill({ icon, label, value, color = "#6b7280" }: {
  icon: string; label: string; value: string; color?: string;
}) {
  return (
    <div style={{ display: "flex", alignItems: "center", gap: 5 }}>
      <span style={{ fontSize: 9, color: "#4b5563" }}>{icon}</span>
      <span style={{ fontSize: 10, color }}>
        <span style={{ color: "#374151" }}>{label} </span>{value}
      </span>
    </div>
  );
}

function StatsBar({ projects }: { projects: ProjectSummary[] }) {
  const total = projects.length;
  const completed = projects.filter(p => p.overallStatus === "completed").length;
  const running = projects.filter(p => p.overallStatus === "running").length;
  const failed = projects.filter(p => p.overallStatus === "failed").length;

  return (
    <div style={{
      display: "grid", gridTemplateColumns: "repeat(4, 1fr)", gap: 12, marginBottom: 32,
    }}>
      {[
        { label: "Total Projects", value: total, color: "#f0f0f5" },
        { label: "Completed", value: completed, color: "#34d399" },
        { label: "Running", value: running, color: "#a78bfa" },
        { label: "Failed", value: failed, color: failed > 0 ? "#f87171" : "#374151" },
      ].map(item => (
        <div key={item.label} style={{
          background: "#161a26", border: "1px solid rgba(255,255,255,0.06)",
          borderRadius: 12, padding: "14px 16px",
        }}>
          <div style={{ fontSize: 10, color: "#4b5563", letterSpacing: "0.1em", marginBottom: 8 }}>
            {item.label.toUpperCase()}
          </div>
          <div style={{ fontSize: 26, fontWeight: 700, color: item.color, fontFamily: "monospace" }}>
            {item.value}
          </div>
        </div>
      ))}
    </div>
  );
}

export default function ProjectsPage() {
  const [projects, setProjects] = useState<ProjectSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [filter, setFilter] = useState<"all" | "running" | "completed" | "failed">("all");

  useEffect(() => {
    api.projects.list()
      .then(({ projects: p }) => setProjects(p || []))
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  }, []);

  const filtered = filter === "all" ? projects : projects.filter(p => p.overallStatus === filter);

  return (
    <>
      <style>{`
        @keyframes pulse { 0%,100%{opacity:1} 50%{opacity:0.3} }
      `}</style>

      <div style={{ padding: "32px", maxWidth: 1200, margin: "0 auto" }}>

        {/* Header */}
        <div style={{ marginBottom: 32, animation: "fade-in-up 0.4s ease-out both" }}>
          <Link href="/" style={{
            color: "#4b5563", fontSize: 11, textDecoration: "none",
            letterSpacing: "0.08em", display: "inline-flex", alignItems: "center", gap: 6,
            marginBottom: 16, padding: "4px 10px", borderRadius: 6,
            border: "1px solid rgba(255,255,255,0.06)", background: "rgba(255,255,255,0.02)",
          }}>
            ← DASHBOARD
          </Link>

          <div style={{ display: "flex", alignItems: "flex-end", justifyContent: "space-between" }}>
            <div>
              <h1 style={{
                fontSize: 30, fontWeight: 800, letterSpacing: "-0.03em",
                color: "#f9fafb", marginBottom: 6,
              }}>
                Film Projects
              </h1>
              <p style={{ fontSize: 13, color: "#4b5563", letterSpacing: "0.02em" }}>
                Pipeline history — {projects.length} project{projects.length !== 1 ? "s" : ""}
              </p>
            </div>
            <Link href="/new" style={{
              display: "flex", alignItems: "center", gap: 8,
              padding: "10px 20px", borderRadius: 10, fontSize: 13, fontWeight: 600,
              background: "linear-gradient(135deg, rgba(124,58,237,0.2), rgba(162,28,175,0.2))",
              border: "1px solid rgba(124,58,237,0.35)",
              color: "#a78bfa", textDecoration: "none",
              transition: "all 0.2s",
            }}>
              <span style={{ fontSize: 16 }}>▶</span>
              New Project
            </Link>
          </div>
        </div>

        {/* Stats */}
        {projects.length > 0 && <StatsBar projects={projects} />}

        {/* Filter tabs */}
        {projects.length > 0 && (
          <div style={{
            display: "flex", gap: 4, marginBottom: 24,
            background: "rgba(0,0,0,0.25)", borderRadius: 10, padding: 4,
            border: "1px solid rgba(255,255,255,0.06)", width: "fit-content",
          }}>
            {(["all", "running", "completed", "failed"] as const).map(f => (
              <button key={f} onClick={() => setFilter(f)} style={{
                padding: "6px 16px", borderRadius: 7, border: "none",
                cursor: "pointer", fontSize: 11, fontWeight: 600, letterSpacing: "0.06em",
                background: filter === f ? "rgba(255,255,255,0.08)" : "transparent",
                color: filter === f ? "#f0f0f5" : "#4b5563",
                transition: "all 0.15s",
              }}>
                {f.toUpperCase()}
              </button>
            ))}
          </div>
        )}

        {/* Error */}
        {error && (
          <div style={{
            marginBottom: 24, padding: "12px 16px", borderRadius: 10, fontSize: 13,
            background: "rgba(239,68,68,0.08)", border: "1px solid rgba(239,68,68,0.2)", color: "#ef4444",
          }}>
            {error}
          </div>
        )}

        {/* Loading */}
        {loading && (
          <div style={{ textAlign: "center", padding: 80 }}>
            <div style={{
              display: "inline-block", width: 32, height: 32,
              border: "2px solid rgba(245,178,64,0.15)",
              borderTopColor: "#f5b240", borderRadius: "50%",
              animation: "spin-slow 0.8s linear infinite",
            }} />
            <p style={{ color: "#4b5563", fontSize: 12, marginTop: 16 }}>Loading projects...</p>
          </div>
        )}

        {/* Empty state */}
        {!loading && filtered.length === 0 && !error && (
          <div style={{
            textAlign: "center", padding: "64px 32px",
            background: "rgba(255,255,255,0.02)", borderRadius: 16,
            border: "1px dashed rgba(255,255,255,0.08)",
          }}>
            <div style={{ fontSize: 48, marginBottom: 16, opacity: 0.4 }}>🎬</div>
            <p style={{ color: "#4b5563", fontSize: 14, marginBottom: 8 }}>
              {filter === "all" ? "No projects yet" : `No ${filter} projects`}
            </p>
            {filter === "all" && (
              <Link href="/new" style={{ color: "#a78bfa", fontSize: 13, textDecoration: "none" }}>
                Start your first film →
              </Link>
            )}
          </div>
        )}

        {/* Project grid */}
        <div style={{ display: "grid", gridTemplateColumns: "repeat(2, 1fr)", gap: 16 }}>
          {filtered.map((p, i) => (
            <div key={p.projectId} style={{ animation: `fade-in-up 0.4s ease-out ${i * 0.05}s both` }}>
              <ProjectCard project={p} />
            </div>
          ))}
        </div>
      </div>
    </>
  );
}
