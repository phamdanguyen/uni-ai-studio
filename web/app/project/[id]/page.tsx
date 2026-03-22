"use client";
import { useEffect, useState, useCallback } from "react";
import { useParams } from "next/navigation";
import Link from "next/link";
import { connectPipelineSSE, type PipelineEvent } from "@/lib/sse";
import { api, type StageInfo, type RunState } from "@/lib/api";

// Override API base for this page (server runs at 8082)
const PAGE_API_BASE = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8082";

// ─── Stage metadata ────────────────────────────────────────────────────────────
const STAGE_META: Record<string, { label: string; icon: string; color: string; desc: string }> = {
  analysis:      { label: "Story Analysis",    icon: "◈", color: "#f59e0b", desc: "Phân tích cấu trúc câu chuyện" },
  planning:      { label: "Pipeline Planning", icon: "⊞", color: "#a78bfa", desc: "Lập kế hoạch sản xuất" },
  characters:    { label: "Character Design",  icon: "◉", color: "#34d399", desc: "Thiết kế nhân vật" },
  locations:     { label: "Location Design",   icon: "◎", color: "#38bdf8", desc: "Thiết kế địa điểm & bối cảnh" },
  segmentation:  { label: "Clip Segmentation", icon: "✂", color: "#fb923c", desc: "Phân đoạn câu chuyện thành clips" },
  screenplay:    { label: "Screenplay",        icon: "📄", color: "#e879f9", desc: "Chuyển đổi clips thành kịch bản" },
  storyboard:    { label: "Storyboard",        icon: "▣", color: "#f472b6", desc: "Phân cảnh quay cho từng clip" },
  media_gen:     { label: "Media Generation",  icon: "◐", color: "#f472b6", desc: "Tạo ảnh & video AI" },
  quality_check: { label: "Quality Check",     icon: "◆", color: "#86efac", desc: "Kiểm tra chất lượng" },
  voice:         { label: "Voice & TTS",       icon: "♪", color: "#c084fc", desc: "Lồng tiếng tự động" },
  assembly:      { label: "Final Assembly",    icon: "▶", color: "#fbbf24", desc: "Ghép nối thành phẩm" },
};

const STAGE_ORDER = [
  "analysis", "planning", "characters", "locations",
  "segmentation", "screenplay", "storyboard",
  "media_gen", "quality_check", "voice", "assembly",
];

// ─── Stage static config (agents / skills / prompts) ──────────────────────────
type StageConfig = {
  agent: string;
  skill: string;
  promptFile: string;
  tools: string[];
  notes?: string;
};
const STAGE_CONFIG: Record<string, StageConfig> = {
  analysis:      { agent: "director",    skill: "analyze_story",      promptFile: "analyze_story.en.txt",       tools: ["LLM:standard"], notes: "Phân tích thể loại, nhân vật, địa điểm, cấu trúc cốt truyện" },
  planning:      { agent: "director",    skill: "plan_pipeline",      promptFile: "plan_pipeline.en.txt",       tools: ["LLM:flash"],    notes: "Lập kế hoạch thứ tự thực hiện, budget, quality level" },
  characters:    { agent: "character",   skill: "analyze_characters",  promptFile: "analyze_characters.en.txt",  tools: ["LLM:standard"], notes: "Thiết kế nhân vật: ngoại hình, tính cách, vai trò" },
  locations:     { agent: "location",    skill: "analyze_locations",   promptFile: "analyze_locations.en.txt",   tools: ["LLM:standard"], notes: "Thiết kế địa điểm: bầu không khí, mô tả hình ảnh" },
  segmentation:  { agent: "director",    skill: "segment_clips",      promptFile: "segment_clips.en.txt",       tools: ["LLM:standard"], notes: "Chia câu chuyện thành N clips độc lập có thể quay riêng" },
  screenplay:    { agent: "director",    skill: "convert_screenplay",  promptFile: "convert_screenplay.en.txt",  tools: ["LLM:standard"], notes: "Chuyển clip thành kịch bản quay (scene headers, action, dialog)" },
  storyboard:    { agent: "storyboard",  skill: "create_storyboard",  promptFile: "create_storyboard.en.txt",   tools: ["LLM:standard"], notes: "4 phases: Plan → Cinematography → Acting → Detail merge" },
  media_gen:     { agent: "media",       skill: "generate_batch",     promptFile: "—",                          tools: ["FAL.ai:flux", "FAL.ai:video"], notes: "Tạo ảnh/video từ image_prompt của từng panel" },
  quality_check: { agent: "media",       skill: "quality_review",     promptFile: "quality_review.en.txt",      tools: ["LLM:flash"],    notes: "Đánh giá chất lượng output, ghi nhận cảnh báo" },
  voice:         { agent: "voice",       skill: "analyze_voices",     promptFile: "analyze_voices.en.txt",      tools: ["LLM:standard", "TTS"], notes: "Phân tích giọng nói nhân vật, tạo TTS từng đoạn hội thoại" },
  assembly:      { agent: "—",           skill: "—",                  promptFile: "—",                          tools: [],               notes: "Ghép toàn bộ: media + voice + storyboard thành output cuối" },
};

type StageStatus = "pending" | "running" | "completed" | "failed" | "awaiting_approval";

// ─── Helpers ──────────────────────────────────────────────────────────────────
function fmt(iso?: string) {
  if (!iso) return "—";
  return new Date(iso).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

function dur(start?: string, end?: string) {
  if (!start) return null;
  const ms = new Date(end || new Date()).getTime() - new Date(start).getTime();
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  return `${Math.floor(s / 60)}m ${s % 60}s`;
}

function safeParseJson(str: string): unknown {
  try { return JSON.parse(str); } catch { return null; }
}

// ─── JSON Tree Viewer ─────────────────────────────────────────────────────────
function JsonNode({ data, depth = 0 }: { data: unknown; depth?: number }) {
  const [collapsed, setCollapsed] = useState(depth > 1);
  const indent = depth * 14;

  if (data === null) return <span style={{ color: "#6b7280" }}>null</span>;
  if (typeof data === "boolean") return <span style={{ color: "#f472b6" }}>{String(data)}</span>;
  if (typeof data === "number") return <span style={{ color: "#38bdf8" }}>{data}</span>;
  if (typeof data === "string") {
    if (data.startsWith("http")) {
      const isImg = /\.(jpg|jpeg|png|webp|gif)(\?|$)/i.test(data);
      const isVid = /\.(mp4|webm|mov)(\?|$)/i.test(data);
      return (
        <span>
          <a href={data} target="_blank" rel="noreferrer"
            style={{ color: "#fbbf24", textDecoration: "underline", wordBreak: "break-all" }}>
            {data.length > 60 ? data.slice(0, 60) + "…" : data}
          </a>
          {isImg && <span style={{ marginLeft: 6, fontSize: 9, color: "#f472b6", opacity: 0.7 }}>[img]</span>}
          {isVid && <span style={{ marginLeft: 6, fontSize: 9, color: "#38bdf8", opacity: 0.7 }}>[vid]</span>}
        </span>
      );
    }
    return <span style={{ color: "#86efac" }}>&quot;{data.length > 120 ? data.slice(0, 120) + "…" : data}&quot;</span>;
  }

  if (Array.isArray(data)) {
    if (data.length === 0) return <span style={{ color: "#4b5563" }}>[]</span>;
    return (
      <span>
        <button onClick={() => setCollapsed(v => !v)}
          style={{ background: "none", border: "none", color: "#f59e0b", cursor: "pointer", padding: "0 2px", fontSize: 10 }}>
          {collapsed ? `▶ [${data.length}]` : `▼ [${data.length}]`}
        </button>
        {!collapsed && (
          <div style={{ marginLeft: indent + 14 }}>
            {data.map((item, i) => (
              <div key={i} style={{ marginBottom: 2 }}>
                <span style={{ color: "#374151", fontSize: 9 }}>{i}: </span>
                <JsonNode data={item} depth={depth + 1} />
              </div>
            ))}
          </div>
        )}
      </span>
    );
  }

  if (typeof data === "object") {
    const keys = Object.keys(data as object);
    if (keys.length === 0) return <span style={{ color: "#4b5563" }}>{"{}"}</span>;
    return (
      <span>
        <button onClick={() => setCollapsed(v => !v)}
          style={{ background: "none", border: "none", color: "#a78bfa", cursor: "pointer", padding: "0 2px", fontSize: 10 }}>
          {collapsed ? `▶ {${keys.length}}` : `▼ {${keys.length}}`}
        </button>
        {!collapsed && (
          <div style={{ marginLeft: indent + 14 }}>
            {keys.map(k => (
              <div key={k} style={{ marginBottom: 3 }}>
                <span style={{ color: "#94a3b8", fontSize: 10 }}>{k}: </span>
                <JsonNode data={(data as Record<string, unknown>)[k]} depth={depth + 1} />
              </div>
            ))}
          </div>
        )}
      </span>
    );
  }
  return <span style={{ color: "#d1d5db" }}>{String(data)}</span>;
}

// ─── Status Badge ─────────────────────────────────────────────────────────────
function StatusBadge({ status }: { status: StageStatus | string }) {
  const cfg: Record<string, { color: string; bg: string; label: string }> = {
    pending:            { color: "#6b7280", bg: "rgba(107,114,128,0.15)", label: "PENDING" },
    running:            { color: "#60a5fa", bg: "rgba(96,165,250,0.15)",  label: "RUNNING" },
    completed:          { color: "#34d399", bg: "rgba(52,211,153,0.15)",  label: "DONE"    },
    failed:             { color: "#f87171", bg: "rgba(248,113,113,0.15)", label: "FAILED"  },
    awaiting_approval:  { color: "#fbbf24", bg: "rgba(251,191,36,0.15)", label: "AWAITING" },
  };
  const c = cfg[status] ?? cfg.pending;
  return (
    <span style={{
      fontSize: 9, padding: "2px 8px", borderRadius: 4, fontWeight: 700,
      letterSpacing: "0.08em", background: c.bg, color: c.color,
    }}>
      {c.label}
    </span>
  );
}

// ─── Status Dot ───────────────────────────────────────────────────────────────
function StatusDot({ status }: { status: StageStatus | string }) {
  const colors: Record<string, string> = {
    pending:            "#374151",
    running:            "#60a5fa",
    completed:          "#34d399",
    failed:             "#f87171",
    awaiting_approval:  "#fbbf24",
  };
  const color = colors[status] ?? "#374151";
  return (
    <div style={{
      width: 7, height: 7, borderRadius: "50%", background: color, flexShrink: 0,
      boxShadow: (status === "running" || status === "awaiting_approval") ? `0 0 6px ${color}` : "none",
      animation: (status === "running" || status === "awaiting_approval") ? "pulse 1.4s infinite" : "none",
    }} />
  );
}

// ─── Analysis Stage Renderer ──────────────────────────────────────────────────
function AnalysisRenderer({ output }: { output: Record<string, unknown> }) {
  const raw = output["analysis"] as string | undefined;
  const parsed = raw ? (safeParseJson(raw) as Record<string, unknown> | null) : null;
  const data = parsed ?? (output as Record<string, unknown>);

  const characters = data["characters"] as { name: string; role: string; description: string }[] | undefined;
  const locations  = data["locations"]  as { name: string; description: string }[] | undefined;
  const themes     = data["themes"]     as string[] | undefined;
  const plot       = data["plotStructure"] as Record<string, string> | undefined;

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 20 }}>
      {/* Meta row */}
      <div style={{ display: "grid", gridTemplateColumns: "repeat(3,1fr)", gap: 12 }}>
        {[
          { label: "TITLE", value: data["title"] as string },
          { label: "GENRE", value: data["genre"] as string },
          { label: "MOOD",  value: data["mood"]  as string },
        ].map(item => item.value ? (
          <div key={item.label} style={{
            background: "#151820", border: "1px solid #1e2028",
            borderRadius: 8, padding: "10px 14px",
          }}>
            <div style={{ fontSize: 9, color: "#6b7280", letterSpacing: "0.1em", marginBottom: 4 }}>{item.label}</div>
            <div style={{ fontSize: 13, color: "#e2e8f0", fontWeight: 600 }}>{item.value}</div>
          </div>
        ) : null)}
      </div>

      {/* Themes */}
      {Array.isArray(themes) && themes.length > 0 && (
        <div>
          <div style={{ fontSize: 10, color: "#6b7280", letterSpacing: "0.1em", marginBottom: 8 }}>THEMES</div>
          <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
            {themes.map((t, i) => (
              <span key={i} style={{
                fontSize: 11, padding: "3px 10px", borderRadius: 20,
                background: "rgba(245,158,11,0.1)", border: "1px solid rgba(245,158,11,0.25)", color: "#f59e0b",
              }}>{t}</span>
            ))}
          </div>
        </div>
      )}

      {/* Plot structure */}
      {plot && (
        <div>
          <div style={{ fontSize: 10, color: "#6b7280", letterSpacing: "0.1em", marginBottom: 8 }}>PLOT STRUCTURE</div>
          <div style={{ display: "grid", gridTemplateColumns: "repeat(2,1fr)", gap: 10 }}>
            {(["setup","conflict","climax","resolution"] as const).map(key => plot[key] ? (
              <div key={key} style={{
                background: "#151820", border: "1px solid #1e2028",
                borderRadius: 8, padding: "10px 14px",
              }}>
                <div style={{ fontSize: 9, color: "#f59e0b", letterSpacing: "0.1em", marginBottom: 4, textTransform: "uppercase" }}>{key}</div>
                <div style={{ fontSize: 11, color: "#9ca3af", lineHeight: 1.6 }}>{plot[key]}</div>
              </div>
            ) : null)}
          </div>
        </div>
      )}

      {/* Characters */}
      {Array.isArray(characters) && characters.length > 0 && (
        <div>
          <div style={{ fontSize: 10, color: "#6b7280", letterSpacing: "0.1em", marginBottom: 8 }}>CHARACTERS</div>
          <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
            {characters.map((c, i) => (
              <div key={i} style={{
                background: "#151820", border: "1px solid #1e2028",
                borderRadius: 8, padding: "12px 14px",
                display: "flex", alignItems: "flex-start", gap: 12,
              }}>
                <div style={{
                  width: 36, height: 36, borderRadius: "50%", flexShrink: 0,
                  background: "rgba(52,211,153,0.1)", border: "1px solid rgba(52,211,153,0.3)",
                  display: "flex", alignItems: "center", justifyContent: "center",
                  fontSize: 16, color: "#34d399",
                }}>◉</div>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 3 }}>
                    <span style={{ fontSize: 13, fontWeight: 700, color: "#e2e8f0" }}>{c.name}</span>
                    <span style={{
                      fontSize: 9, padding: "1px 7px", borderRadius: 3,
                      background: "rgba(52,211,153,0.1)", color: "#34d399", letterSpacing: "0.06em",
                    }}>{c.role?.toUpperCase()}</span>
                  </div>
                  <div style={{ fontSize: 11, color: "#9ca3af", lineHeight: 1.5 }}>{c.description}</div>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Locations */}
      {Array.isArray(locations) && locations.length > 0 && (
        <div>
          <div style={{ fontSize: 10, color: "#6b7280", letterSpacing: "0.1em", marginBottom: 8 }}>LOCATIONS</div>
          <div style={{ display: "grid", gridTemplateColumns: "repeat(2,1fr)", gap: 10 }}>
            {locations.map((loc, i) => (
              <div key={i} style={{
                background: "#151820", border: "1px solid #1e2028",
                borderRadius: 8, padding: "12px 14px",
              }}>
                <div style={{ fontSize: 12, fontWeight: 700, color: "#38bdf8", marginBottom: 4 }}>◎ {loc.name}</div>
                <div style={{ fontSize: 11, color: "#9ca3af", lineHeight: 1.5 }}>{loc.description}</div>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Fallback raw */}
      {!parsed && !characters && !locations && (
        <div style={{
          background: "rgba(0,0,0,0.3)", border: "1px solid #1e2028",
          borderRadius: 8, padding: 14, fontFamily: "monospace", fontSize: 11,
        }}>
          <JsonNode data={output} depth={0} />
        </div>
      )}
    </div>
  );
}

// ─── Planning Stage Renderer ──────────────────────────────────────────────────
function PlanningRenderer({ output }: { output: Record<string, unknown> }) {
  const raw  = output["plan"] as string | undefined;
  const parsed = raw ? (safeParseJson(raw) as Record<string, unknown> | null) : null;
  const data = parsed ?? output;
  const steps = data["steps"] as { agent?: string; skill?: string; depends?: string[] }[] | undefined;

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      {/* Strategy */}
      {!!data["strategy"] && (
        <div style={{
          background: "rgba(167,139,250,0.08)", border: "1px solid rgba(167,139,250,0.25)",
          borderRadius: 10, padding: "14px 16px",
        }}>
          <div style={{ fontSize: 9, color: "#a78bfa", letterSpacing: "0.1em", marginBottom: 6 }}>STRATEGY</div>
          <div style={{ fontSize: 14, fontWeight: 700, color: "#e2e8f0" }}>{String(data["strategy"])}</div>
        </div>
      )}

      {/* Reasoning */}
      {!!data["reasoning"] && (
        <div style={{ background: "#151820", border: "1px solid #1e2028", borderRadius: 8, padding: "12px 14px" }}>
          <div style={{ fontSize: 9, color: "#6b7280", letterSpacing: "0.1em", marginBottom: 6 }}>REASONING</div>
          <div style={{ fontSize: 12, color: "#9ca3af", lineHeight: 1.7 }}>{String(data["reasoning"])}</div>
        </div>
      )}

      {/* Steps */}
      {Array.isArray(steps) && steps.length > 0 && (
        <div>
          <div style={{ fontSize: 10, color: "#6b7280", letterSpacing: "0.1em", marginBottom: 10 }}>
            EXECUTION STEPS ({steps.length})
          </div>
          <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
            {steps.map((step, i) => (
              <div key={i} style={{
                background: "#151820", border: "1px solid #1e2028",
                borderRadius: 8, padding: "10px 14px",
                display: "flex", alignItems: "flex-start", gap: 12,
              }}>
                <div style={{
                  width: 24, height: 24, borderRadius: "50%", flexShrink: 0,
                  background: "rgba(167,139,250,0.15)", border: "1px solid rgba(167,139,250,0.3)",
                  display: "flex", alignItems: "center", justifyContent: "center",
                  fontSize: 10, color: "#a78bfa", fontWeight: 700,
                }}>{i + 1}</div>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div style={{ display: "flex", gap: 8, flexWrap: "wrap", marginBottom: 4 }}>
                    {step.agent && (
                      <code style={{ fontSize: 11, color: "#f9fafb", background: "rgba(167,139,250,0.1)", padding: "1px 7px", borderRadius: 4 }}>
                        {step.agent}
                      </code>
                    )}
                    {step.skill && (
                      <code style={{ fontSize: 11, color: "#34d399", background: "rgba(52,211,153,0.08)", padding: "1px 7px", borderRadius: 4 }}>
                        {step.skill}
                      </code>
                    )}
                  </div>
                  {Array.isArray(step.depends) && step.depends.length > 0 && (
                    <div style={{ fontSize: 10, color: "#4b5563" }}>
                      depends: {step.depends.join(", ")}
                    </div>
                  )}
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {!parsed && !steps && (
        <div style={{ background: "rgba(0,0,0,0.3)", border: "1px solid #1e2028", borderRadius: 8, padding: 14, fontFamily: "monospace", fontSize: 11 }}>
          <JsonNode data={output} depth={0} />
        </div>
      )}
    </div>
  );
}

// ─── Characters Stage Renderer ────────────────────────────────────────────────
function CharactersRenderer({ output }: { output: Record<string, unknown> }) {
  const raw = output["characters"] as string | undefined;
  const parsed = raw ? (safeParseJson(raw) as unknown) : null;
  const list = Array.isArray(parsed) ? parsed : (
    parsed && typeof parsed === "object" && Array.isArray((parsed as Record<string, unknown>)["characters"])
      ? (parsed as Record<string, unknown>)["characters"] as unknown[]
      : null
  );

  if (!list) {
    return (
      <div style={{ background: "rgba(0,0,0,0.3)", border: "1px solid #1e2028", borderRadius: 8, padding: 14, fontFamily: "monospace", fontSize: 11 }}>
        <JsonNode data={output} depth={0} />
      </div>
    );
  }

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      {(list as Record<string, unknown>[]).map((c, i) => (
        <div key={i} style={{
          background: "#151820", border: "1px solid #1e2028",
          borderRadius: 10, padding: "14px 16px",
          display: "flex", alignItems: "flex-start", gap: 14,
        }}>
          <div style={{
            width: 44, height: 44, borderRadius: "50%", flexShrink: 0,
            background: "rgba(52,211,153,0.1)", border: "2px solid rgba(52,211,153,0.3)",
            display: "flex", alignItems: "center", justifyContent: "center",
            fontSize: 20,
          }}>◉</div>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 6, flexWrap: "wrap" }}>
              <span style={{ fontSize: 14, fontWeight: 700, color: "#e2e8f0" }}>{String(c["name"] ?? `Character ${i + 1}`)}</span>
              {!!c["role_level"] && (
                <span style={{ fontSize: 9, padding: "2px 7px", borderRadius: 3, background: "rgba(52,211,153,0.1)", color: "#34d399", letterSpacing: "0.06em" }}>
                  {String(c["role_level"])}
                </span>
              )}
              {!!c["gender"] && <span style={{ fontSize: 10, color: "#6b7280" }}>{String(c["gender"])}</span>}
              {!!c["age_range"] && <span style={{ fontSize: 10, color: "#6b7280" }}>{String(c["age_range"])}</span>}
            </div>
            {!!c["introduction"] && (
              <div style={{ fontSize: 11, color: "#9ca3af", lineHeight: 1.6, marginBottom: 8 }}>{String(c["introduction"])}</div>
            )}
            <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
              {(Array.isArray(c["personality_tags"]) ? c["personality_tags"] as string[] : []).map((tag, ti) => (
                <span key={ti} style={{
                  fontSize: 10, padding: "2px 8px", borderRadius: 20,
                  background: "rgba(52,211,153,0.08)", border: "1px solid rgba(52,211,153,0.2)", color: "#34d399",
                }}>{tag}</span>
              ))}
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}

// ─── Locations Stage Renderer ─────────────────────────────────────────────────
function LocationsRenderer({ output }: { output: Record<string, unknown> }) {
  const raw = output["locations"] as string | undefined;
  const parsed = raw ? (safeParseJson(raw) as unknown) : null;
  const list = Array.isArray(parsed) ? parsed : (
    parsed && typeof parsed === "object" && Array.isArray((parsed as Record<string, unknown>)["locations"])
      ? (parsed as Record<string, unknown>)["locations"] as unknown[]
      : null
  );

  if (!list) {
    return (
      <div style={{ background: "rgba(0,0,0,0.3)", border: "1px solid #1e2028", borderRadius: 8, padding: 14, fontFamily: "monospace", fontSize: 11 }}>
        <JsonNode data={output} depth={0} />
      </div>
    );
  }

  return (
    <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(280px,1fr))", gap: 12 }}>
      {(list as Record<string, unknown>[]).map((loc, i) => (
        <div key={i} style={{
          background: "#151820", border: "1px solid #1e2028",
          borderRadius: 10, padding: "14px 16px",
        }}>
          <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 8 }}>
            <span style={{ fontSize: 16, color: "#38bdf8" }}>◎</span>
            <span style={{ fontSize: 13, fontWeight: 700, color: "#e2e8f0" }}>{String(loc["name"] ?? `Location ${i + 1}`)}</span>
          </div>
          {!!loc["summary"] && (
            <div style={{ fontSize: 11, color: "#9ca3af", lineHeight: 1.6, marginBottom: 8 }}>{String(loc["summary"])}</div>
          )}
          {Array.isArray(loc["descriptions"]) && (loc["descriptions"] as string[]).slice(0, 2).map((d, di) => (
            <div key={di} style={{
              fontSize: 10, color: "#4b5563", marginBottom: 4,
              padding: "4px 8px", background: "rgba(56,189,248,0.05)",
              borderLeft: "2px solid rgba(56,189,248,0.3)", borderRadius: "0 4px 4px 0",
            }}>{d}</div>
          ))}
        </div>
      ))}
    </div>
  );
}

// ─── Segmentation Stage Renderer ──────────────────────────────────────────────
function SegmentationRenderer({ output }: { output: Record<string, unknown> }) {
  const clips = output["clips"] as Record<string, unknown>[] | undefined;
  if (!Array.isArray(clips) || clips.length === 0) {
    return (
      <div style={{ background: "rgba(0,0,0,0.3)", border: "1px solid #1e2028", borderRadius: 8, padding: 14, fontFamily: "monospace", fontSize: 11 }}>
        <JsonNode data={output} depth={0} />
      </div>
    );
  }
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
      {clips.map((clip, i) => {
        const content = String(clip["content"] ?? clip["text"] ?? "");
        return (
          <div key={i} style={{
            background: "#151820", border: "1px solid #1e2028",
            borderRadius: 10, padding: "12px 16px",
            display: "flex", gap: 14, alignItems: "flex-start",
          }}>
            <div style={{
              width: 32, height: 32, borderRadius: 8, flexShrink: 0,
              background: "rgba(251,146,60,0.1)", border: "1px solid rgba(251,146,60,0.3)",
              display: "flex", alignItems: "center", justifyContent: "center",
              fontSize: 13, fontWeight: 700, color: "#fb923c", fontFamily: "monospace",
            }}>{String(clip["order"] ?? i + 1)}</div>
            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={{ fontSize: 13, fontWeight: 600, color: "#e2e8f0", marginBottom: 4 }}>
                {String(clip["title"] ?? `Clip ${i + 1}`)}
              </div>
              {content && (
                <div style={{ fontSize: 11, color: "#6b7280", lineHeight: 1.6 }}>
                  {content.length > 100 ? content.slice(0, 100) + "…" : content}
                </div>
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
}

// ─── Screenplay Stage Renderer ────────────────────────────────────────────────
function ScreenplayRenderer({ output }: { output: Record<string, unknown> }) {
  const screenplays = output["screenplays"] as Record<string, unknown>[] | undefined;
  const [activeIdx, setActiveIdx] = useState(0);
  if (!Array.isArray(screenplays) || screenplays.length === 0) {
    return (
      <div style={{ background: "rgba(0,0,0,0.3)", border: "1px solid #1e2028", borderRadius: 8, padding: 14, fontFamily: "monospace", fontSize: 11 }}>
        <JsonNode data={output} depth={0} />
      </div>
    );
  }
  const active = screenplays[activeIdx];
  return (
    <div>
      {/* Tab switcher */}
      <div style={{ display: "flex", gap: 6, marginBottom: 16, flexWrap: "wrap" }}>
        {screenplays.map((sp, i) => (
          <button key={i} onClick={() => setActiveIdx(i)} style={{
            fontSize: 11, padding: "5px 12px", borderRadius: 6, cursor: "pointer",
            background: activeIdx === i ? "rgba(232,121,249,0.15)" : "#151820",
            border: `1px solid ${activeIdx === i ? "rgba(232,121,249,0.4)" : "#1e2028"}`,
            color: activeIdx === i ? "#e879f9" : "#6b7280",
            fontWeight: activeIdx === i ? 700 : 400,
            transition: "all 0.15s",
          }}>
            {String(sp["title"] ?? `Clip ${i + 1}`)}
          </button>
        ))}
      </div>
      {/* Content */}
      <div style={{
        background: "#0a0c10", border: "1px solid #1e2028",
        borderRadius: 10, padding: "20px 24px",
      }}>
        <div style={{ fontSize: 11, color: "#e879f9", letterSpacing: "0.06em", marginBottom: 12 }}>
          CLIP {String(active["clipId"] ?? activeIdx + 1)} — SCREENPLAY
        </div>
        <pre style={{
          margin: 0, fontFamily: "monospace", fontSize: 12, lineHeight: 1.8,
          color: "#e2e8f0", whiteSpace: "pre-wrap", wordBreak: "break-word",
        }}>
          {String(active["screenplay"] ?? active["content"] ?? JSON.stringify(active, null, 2))}
        </pre>
      </div>
    </div>
  );
}

// ─── Storyboard Stage Renderer ────────────────────────────────────────────────
function StoryboardRenderer({ output }: { output: Record<string, unknown> }) {
  const storyboards = output["storyboards"] as Record<string, unknown>[] | undefined;
  const [activeIdx, setActiveIdx] = useState(0);
  const [selectedPanel, setSelectedPanel] = useState<number | null>(null);

  if (!Array.isArray(storyboards) || storyboards.length === 0) {
    return (
      <div style={{ background: "rgba(0,0,0,0.3)", border: "1px solid #1e2028", borderRadius: 8, padding: 14, fontFamily: "monospace", fontSize: 11 }}>
        <JsonNode data={output} depth={0} />
      </div>
    );
  }

  const activeSb = storyboards[activeIdx];
  const panels = activeSb["panels"] as Record<string, unknown>[] | undefined;

  return (
    <div>
      {/* Tab per storyboard/clip */}
      <div style={{ display: "flex", gap: 6, marginBottom: 16, flexWrap: "wrap" }}>
        {storyboards.map((sb, i) => (
          <button key={i} onClick={() => { setActiveIdx(i); setSelectedPanel(null); }} style={{
            fontSize: 11, padding: "5px 12px", borderRadius: 6, cursor: "pointer",
            background: activeIdx === i ? "rgba(244,114,182,0.15)" : "#151820",
            border: `1px solid ${activeIdx === i ? "rgba(244,114,182,0.4)" : "#1e2028"}`,
            color: activeIdx === i ? "#f472b6" : "#6b7280",
            fontWeight: activeIdx === i ? 700 : 400,
            transition: "all 0.15s",
          }}>
            {String(sb["title"] ?? `Clip ${i + 1}`)}
          </button>
        ))}
      </div>

      {/* Panel grid */}
      {Array.isArray(panels) && panels.length > 0 ? (
        <>
          <div style={{ display: "grid", gridTemplateColumns: "repeat(3,1fr)", gap: 10, marginBottom: 16 }}>
            {panels.map((panel, pi) => {
              const isSelected = selectedPanel === pi;
              return (
                <div key={pi} onClick={() => setSelectedPanel(isSelected ? null : pi)} style={{
                  background: isSelected ? "rgba(244,114,182,0.08)" : "#151820",
                  border: `1px solid ${isSelected ? "rgba(244,114,182,0.4)" : "#1e2028"}`,
                  borderRadius: 8, padding: "10px 12px", cursor: "pointer",
                  transition: "all 0.15s",
                }}>
                  <div style={{ fontSize: 9, color: "#f472b6", fontWeight: 700, letterSpacing: "0.1em", marginBottom: 6 }}>
                    PANEL {String(panel["panel_number"] ?? pi + 1)}
                  </div>
                  <div style={{ fontSize: 11, color: "#9ca3af", lineHeight: 1.5, marginBottom: 6 }}>
                    {(() => {
                      const desc = String(panel["description"] ?? "");
                      return desc.length > 80 ? desc.slice(0, 80) + "…" : desc;
                    })()}
                  </div>
                  <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
                    {!!panel["shot_type"] && (
                      <span style={{ fontSize: 9, padding: "1px 6px", borderRadius: 3, background: "rgba(244,114,182,0.08)", color: "#f472b6" }}>
                        {String(panel["shot_type"])}
                      </span>
                    )}
                    {!!panel["camera_move"] && (
                      <span style={{ fontSize: 9, padding: "1px 6px", borderRadius: 3, background: "rgba(56,189,248,0.08)", color: "#38bdf8" }}>
                        {String(panel["camera_move"])}
                      </span>
                    )}
                  </div>
                </div>
              );
            })}
          </div>

          {/* Expanded panel detail */}
          {selectedPanel !== null && panels[selectedPanel] && (
            <div style={{
              background: "#0a0c10", border: "1px solid rgba(244,114,182,0.3)",
              borderRadius: 10, padding: "16px 20px",
            }}>
              <div style={{ fontSize: 10, color: "#f472b6", letterSpacing: "0.1em", marginBottom: 12 }}>
                PANEL {String(panels[selectedPanel]["panel_number"] ?? selectedPanel + 1)} — DETAIL
              </div>
              <div style={{ display: "grid", gridTemplateColumns: "repeat(2,1fr)", gap: 12 }}>
                {[
                  { key: "description",   label: "Description" },
                  { key: "image_prompt",  label: "Image Prompt" },
                  { key: "composition",   label: "Composition" },
                  { key: "lighting",      label: "Lighting" },
                  { key: "color_palette", label: "Color Palette" },
                  { key: "atmosphere",    label: "Atmosphere" },
                ].map(({ key, label }) => panels[selectedPanel][key] ? (
                  <div key={key}>
                    <div style={{ fontSize: 9, color: "#6b7280", letterSpacing: "0.08em", marginBottom: 4 }}>{label.toUpperCase()}</div>
                    <div style={{ fontSize: 11, color: "#e2e8f0", lineHeight: 1.6 }}>{String(panels[selectedPanel][key])}</div>
                  </div>
                ) : null)}
              </div>
            </div>
          )}
        </>
      ) : (
        <div style={{ textAlign: "center", padding: "48px 24px", color: "#374151" }}>
          <div style={{ fontSize: 32, marginBottom: 12, opacity: 0.4 }}>▣</div>
          <p style={{ fontSize: 13 }}>No panels in this storyboard</p>
        </div>
      )}
    </div>
  );
}

// ─── Media Gen Stage Renderer ─────────────────────────────────────────────────
function MediaGenRenderer({ output }: { output: Record<string, unknown> }) {
  const [lightbox, setLightbox] = useState<string | null>(null);
  const panels = output["panels"] as Record<string, unknown>[] | undefined;
  const results = output["results"] as Record<string, unknown>[] | undefined;
  const items = panels ?? results ?? [];

  const images = items.filter(it => it["imageUrl"]);

  if (images.length === 0) {
    return (
      <div style={{ background: "rgba(0,0,0,0.3)", border: "1px solid #1e2028", borderRadius: 8, padding: 14, fontFamily: "monospace", fontSize: 11 }}>
        <JsonNode data={output} depth={0} />
      </div>
    );
  }

  return (
    <>
      <div style={{ display: "grid", gridTemplateColumns: "repeat(3,1fr)", gap: 10 }}>
        {images.map((item, i) => {
          const url = String(item["imageUrl"] ?? "");
          return url ? (
            <div key={i} onClick={() => setLightbox(url)} style={{
              borderRadius: 8, overflow: "hidden", cursor: "pointer",
              border: "1px solid rgba(244,114,182,0.2)", position: "relative",
              aspectRatio: "16/9", background: "#0d0f13",
            }}>
              <img src={url} alt={`Gen ${i + 1}`}
                style={{ width: "100%", height: "100%", objectFit: "cover", display: "block" }}
                onError={(e) => { (e.target as HTMLImageElement).style.display = "none"; }} />
              <div style={{
                position: "absolute", bottom: 0, left: 0, right: 0,
                padding: "4px 8px",
                background: "linear-gradient(transparent, rgba(0,0,0,0.8))",
                fontSize: 9, color: "#86efac",
              }}>Panel {i + 1}</div>
            </div>
          ) : (
            <div key={i} style={{
              borderRadius: 8, background: "#151820", border: "1px solid #1e2028",
              aspectRatio: "16/9", display: "flex", alignItems: "center", justifyContent: "center",
              fontSize: 11, color: "#374151",
            }}>
              No image
            </div>
          );
        })}
      </div>

      {lightbox && (
        <div onClick={() => setLightbox(null)} style={{
          position: "fixed", inset: 0, background: "rgba(0,0,0,0.92)",
          display: "flex", alignItems: "center", justifyContent: "center",
          zIndex: 2000, cursor: "zoom-out",
        }}>
          <img src={lightbox} alt="Preview"
            style={{ maxWidth: "88vw", maxHeight: "88vh", objectFit: "contain", borderRadius: 10 }} />
          <button onClick={() => setLightbox(null)} style={{
            position: "absolute", top: 20, right: 24,
            background: "none", border: "none", color: "#fff", fontSize: 28, cursor: "pointer",
          }}>✕</button>
        </div>
      )}
    </>
  );
}

// ─── Generic JSON Renderer (fallback) ─────────────────────────────────────────
function GenericRenderer({ output }: { output: Record<string, unknown> }) {
  return (
    <div style={{
      background: "rgba(0,0,0,0.3)", border: "1px solid #1e2028",
      borderRadius: 8, padding: 16, fontFamily: "monospace", fontSize: 11, lineHeight: 1.7,
      overflowX: "auto",
    }}>
      <JsonNode data={output} depth={0} />
    </div>
  );
}

// ─── Stage Content Router ─────────────────────────────────────────────────────
function StageContent({ stageKey, output }: { stageKey: string; output: Record<string, unknown> }) {
  switch (stageKey) {
    case "analysis":     return <AnalysisRenderer output={output} />;
    case "planning":     return <PlanningRenderer output={output} />;
    case "characters":   return <CharactersRenderer output={output} />;
    case "locations":    return <LocationsRenderer output={output} />;
    case "segmentation": return <SegmentationRenderer output={output} />;
    case "screenplay":   return <ScreenplayRenderer output={output} />;
    case "storyboard":   return <StoryboardRenderer output={output} />;
    case "media_gen":    return <MediaGenRenderer output={output} />;
    default:             return <GenericRenderer output={output} />;
  }
}

// ─── Left Panel: Timeline ─────────────────────────────────────────────────────
function TimelinePanel({
  stages, liveStatuses, selectedStage, onSelectStage, completedCount, projectId, runState,
}: {
  stages: Record<string, StageInfo>;
  liveStatuses: Record<string, StageStatus>;
  selectedStage: string;
  onSelectStage: (stage: string) => void;
  completedCount: number;
  projectId: string;
  runState: RunState | null;
}) {
  const progress = (completedCount / STAGE_ORDER.length) * 100;
  const shortId = projectId.slice(0, 8).toUpperCase();

  return (
    <div style={{
      width: 256, minWidth: 256, height: "100vh", position: "sticky", top: 0,
      background: "#111318", borderRight: "1px solid #1e2028",
      display: "flex", flexDirection: "column", overflow: "hidden",
    }}>
      {/* Header — chiều cao 56px để khớp với right panel header */}
      <div style={{
        height: 56, padding: "0 16px",
        borderBottom: "1px solid #1e2028",
        flexShrink: 0,
        display: "flex", flexDirection: "column", justifyContent: "center",
      }}>
        <div style={{ fontSize: 9, color: "#6b7280", letterSpacing: "0.12em", marginBottom: 4 }}>PIPELINE</div>
        <div style={{
          display: "flex", alignItems: "center", gap: 8,
        }}>
          <span style={{
            fontSize: 11, color: "#9ca3af", fontFamily: "monospace",
            overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap",
          }}>{shortId}</span>
          {runState && (
            <span style={{
              fontSize: 8, padding: "1px 6px", borderRadius: 3, fontWeight: 700,
              letterSpacing: "0.06em",
              background: runState.mode === "step_by_step" ? "rgba(251,191,36,0.12)" : "rgba(52,211,153,0.1)",
              color: runState.mode === "step_by_step" ? "#fbbf24" : "#34d399",
              border: `1px solid ${runState.mode === "step_by_step" ? "rgba(251,191,36,0.25)" : "rgba(52,211,153,0.25)"}`,
            }}>
              {runState.mode === "step_by_step" ? "STEP" : "AUTO"}
            </span>
          )}
        </div>
      </div>

      {/* Stage list — CHANGE 3: connector lines between stages */}
      <div style={{ flex: 1, overflowY: "auto", padding: "8px 0" }}>
        {STAGE_ORDER.map((key, idx) => {
          const meta = STAGE_META[key] || { label: key, icon: "⚙", color: "#6b7280", desc: "" };
          const liveStatus = liveStatuses[key];
          const dbStatus = stages[key]?.status as StageStatus | undefined;
          const status: StageStatus = liveStatus !== "pending" ? liveStatus : (dbStatus || "pending");
          const isSelected = selectedStage === key;
          const isLast = idx === STAGE_ORDER.length - 1;

          return (
            <div key={key}>
              <button onClick={() => onSelectStage(key)} style={{
                width: "100%", display: "flex", alignItems: "center", gap: 10,
                padding: "9px 16px", background: isSelected ? `${meta.color}12` : "transparent",
                border: "none", borderLeft: `3px solid ${isSelected ? meta.color : "transparent"}`,
                cursor: "pointer", textAlign: "left", transition: "all 0.15s",
              }}>
                <span style={{
                  fontSize: 14, width: 20, textAlign: "center", flexShrink: 0,
                  color: status === "pending" ? "#374151" : meta.color,
                }}>{meta.icon}</span>
                <span style={{
                  flex: 1, fontSize: 12, fontWeight: isSelected ? 700 : 400,
                  color: isSelected ? "#e2e8f0" : status === "pending" ? "#4b5563" : "#9ca3af",
                  overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap",
                }}>{meta.label}</span>
                {runState?.mode === "step_by_step"
                  && runState?.currentStatus === "awaiting_approval"
                  && runState?.currentStage === key && (
                  <span style={{
                    fontSize: 7, padding: "1px 5px", borderRadius: 3, fontWeight: 700,
                    letterSpacing: "0.06em", flexShrink: 0,
                    background: "rgba(251,191,36,0.15)", color: "#fbbf24",
                  }}>AWAIT</span>
                )}
                <StatusDot status={status} />
              </button>
              {/* Connector line between stages — không hiện sau stage cuối */}
              {!isLast && (
                <div style={{
                  width: 1, height: 12, background: "#1e2028",
                  margin: "0 auto",
                }} />
              )}
            </div>
          );
        })}
      </div>

      {/* Footer: progress */}
      <div style={{
        padding: "14px 16px", borderTop: "1px solid #1e2028", flexShrink: 0,
      }}>
        <div style={{ display: "flex", justifyContent: "space-between", marginBottom: 6 }}>
          <span style={{ fontSize: 10, color: "#4b5563" }}>Progress</span>
          <span style={{ fontSize: 10, color: "#e2e8f0", fontFamily: "monospace", fontWeight: 700 }}>
            {completedCount}/{STAGE_ORDER.length}
          </span>
        </div>
        <div style={{ height: 4, background: "#1e2028", borderRadius: 4, overflow: "hidden" }}>
          <div style={{
            height: "100%", borderRadius: 4, width: `${progress}%`,
            background: progress === 100
              ? "linear-gradient(90deg, #34d399, #059669)"
              : "linear-gradient(90deg, #f59e0b, #f472b6, #a78bfa)",
            transition: "width 0.6s ease",
          }} />
        </div>
      </div>
    </div>
  );
}

// ─── Right Panel: Stage Detail (split Input | Output) ────────────────────────
function StageDetailPanel({
  stageKey, stage, liveStatus, connected, events, projectId, onReload, runState,
}: {
  stageKey: string;
  stage: StageInfo | undefined;
  liveStatus: StageStatus;
  connected: boolean;
  events: PipelineEvent[];
  projectId: string;
  onReload: () => void;
  runState: RunState | null;
}) {
  const meta   = STAGE_META[stageKey]   || { label: stageKey, icon: "⚙", color: "#6b7280", desc: "" };
  const config = STAGE_CONFIG[stageKey] || { agent: "—", skill: "—", promptFile: "—", tools: [], notes: "" };
  const dbStatus = stage?.status as StageStatus | undefined;
  const status: StageStatus = liveStatus !== "pending" ? liveStatus : (dbStatus || "pending");
  const output = stage?.output as Record<string, unknown> | undefined;
  const input  = stage?.input  as Record<string, unknown> | undefined;

  const stageEvents = events.filter(e => e.stage === stageKey);
  const hasOutput = !!output && Object.keys(output).length > 0;
  const canRetry  = status === "completed" || status === "failed";
  const canCopy   = hasOutput;

  // Input edit state
  const [editingInput, setEditingInput]   = useState(false);
  const [editInputText, setEditInputText] = useState("");
  const [savedInput, setSavedInput]       = useState<Record<string, unknown> | null>(null); // override saved locally
  const [actionLoading, setActionLoading] = useState<"retry" | "save" | "edit-retry" | "next" | null>(null);
  const [copyDone, setCopyDone]           = useState(false);

  const isStepByStep = runState?.mode === "step_by_step";
  const isAwaitingApproval = isStepByStep
    && runState?.currentStatus === "awaiting_approval"
    && runState?.currentStage === stageKey;

  // The "effective" input shown: savedInput override > DB input
  const effectiveInput = savedInput ?? input;

  function startEdit() {
    setEditInputText(JSON.stringify(effectiveInput ?? {}, null, 2));
    setEditingInput(true);
  }

  // Save: only persist the input override locally (no retry)
  function handleSave() {
    setActionLoading("save");
    let parsed: unknown = editInputText;
    try { parsed = JSON.parse(editInputText); } catch { /* keep string */ }
    fetch(`${PAGE_API_BASE}/pipeline/${projectId}/stage/${stageKey}/input`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json", "Authorization": `Bearer ${getToken()}` },
      body: JSON.stringify(parsed),
    })
      .catch(() => {})
      .finally(() => {
        setSavedInput(typeof parsed === "object" && parsed !== null ? parsed as Record<string, unknown> : null);
        setEditingInput(false);
        setActionLoading(null);
        onReload();
      });
  }

  // Retry: re-run with current effective input (saved or original)
  function handleRetry() {
    setActionLoading("retry");
    fetch(`${PAGE_API_BASE}/pipeline/${projectId}/retry/${stageKey}`, {
      method: "POST",
      headers: { "Content-Type": "application/json", "Authorization": `Bearer ${getToken()}` },
      body: JSON.stringify(savedInput ?? {}),
    })
      .catch(() => {})
      .finally(() => {
        setActionLoading(null);
        onReload();
      });
  }

  function handleCopy() {
    if (!output) return;
    navigator.clipboard.writeText(JSON.stringify(output, null, 2)).then(() => {
      setCopyDone(true);
      setTimeout(() => setCopyDone(false), 2000);
    }).catch(() => {});
  }

  function handleNextStep() {
    setActionLoading("next");
    api.pipeline.nextStep(projectId)
      .catch(() => {})
      .finally(() => {
        setActionLoading(null);
        onReload();
      });
  }

  const btnBase: React.CSSProperties = {
    padding: "3px 10px", fontSize: 11, borderRadius: 5,
    border: "1px solid #2a2d3a", background: "transparent",
    color: "#6b7280", cursor: "pointer", display: "flex",
    alignItems: "center", gap: 5, transition: "all 0.15s",
    whiteSpace: "nowrap" as const,
  };

  return (
    <div style={{ flex: 1, overflow: "hidden", display: "flex", flexDirection: "column", background: "#0d0f13" }}>

      {/* ── Action bar ── */}
      <div style={{
        height: 36, padding: "0 20px", flexShrink: 0,
        borderBottom: "1px solid #151820",
        background: "#0f1116",
        display: "flex", alignItems: "center", gap: 6,
      }}>
        {/* Time info */}
        {stage?.startedAt && (
          <span style={{ fontSize: 10, color: "#374151", marginRight: 4 }}>
            {fmt(stage.startedAt)} → {fmt(stage.finishedAt)}
            {stage.startedAt && (
              <span style={{ color: "#4b5563", marginLeft: 6 }}>
                {dur(stage.startedAt, stage.finishedAt)}
              </span>
            )}
          </span>
        )}

        <div style={{ flex: 1 }} />

        {/* Copy Output */}
        {canCopy && (
          <button onClick={handleCopy} style={{ ...btnBase, color: copyDone ? "#34d399" : "#6b7280" }}>
            <span>⊕</span><span>{copyDone ? "Copied!" : "Copy Output"}</span>
          </button>
        )}

        {/* Edit / Cancel */}
        {canRetry && (
          <button
            onClick={editingInput ? () => setEditingInput(false) : startEdit}
            style={{ ...btnBase, color: editingInput ? "#f59e0b" : "#6b7280" }}
          >
            <span>✎</span><span>{editingInput ? "Cancel" : "Edit Input"}</span>
          </button>
        )}

        {/* Save (input override only, no retry) */}
        {editingInput && (
          <button
            onClick={handleSave}
            disabled={actionLoading !== null}
            style={{ ...btnBase, color: "#60a5fa", borderColor: "rgba(96,165,250,0.3)" }}
          >
            <span>↓</span>
            <span>{actionLoading === "save" ? "Saving…" : "Save Input"}</span>
          </button>
        )}

        {/* Retry (separate from Save) */}
        {canRetry && (
          <button
            onClick={handleRetry}
            disabled={actionLoading !== null}
            style={{ ...btnBase, color: "#f59e0b", borderColor: "rgba(245,158,11,0.3)" }}
          >
            <span style={{ fontSize: 14 }}>↺</span>
            <span>{actionLoading === "retry" ? "Running…" : "Retry"}</span>
          </button>
        )}

        {/* Approve & Run Next — only in step-by-step mode when awaiting approval */}
        {isStepByStep && isAwaitingApproval && (
          <button
            onClick={handleNextStep}
            disabled={actionLoading !== null}
            style={{
              ...btnBase,
              color: "#0d0f13", background: "#fbbf24",
              border: "1px solid #fbbf24", fontWeight: 700,
              padding: "4px 14px",
            }}
          >
            <span>▶</span>
            <span>{actionLoading === "next" ? "Starting…" : "Approve & Run Next"}</span>
          </button>
        )}
      </div>

      {/* ── Error banner ── */}
      {stage?.error && (
        <div style={{
          padding: "8px 20px", flexShrink: 0,
          background: "rgba(239,68,68,0.07)", borderBottom: "1px solid rgba(239,68,68,0.15)",
          display: "flex", alignItems: "center", gap: 10,
        }}>
          <span style={{ fontSize: 11, color: "#ef4444", fontWeight: 700 }}>✕</span>
          <span style={{ fontSize: 11, color: "#f87171", fontFamily: "monospace" }}>{stage.error}</span>
        </div>
      )}

      {/* ── SSE live events ── */}
      {stageEvents.length > 0 && (
        <div style={{
          padding: "5px 20px", flexShrink: 0,
          borderBottom: "1px solid #151820",
          display: "flex", gap: 6, flexWrap: "wrap",
        }}>
          {stageEvents.slice(-4).map((ev, i) => (
            <span key={i} style={{
              fontSize: 10, padding: "2px 8px", borderRadius: 20,
              background: ev.status === "completed" ? "rgba(52,211,153,0.08)"
                        : ev.status === "started"   ? "rgba(96,165,250,0.08)"
                        : ev.status === "awaiting_approval" ? "rgba(251,191,36,0.08)"
                                                    : "rgba(248,113,113,0.08)",
              border: `1px solid ${ev.status === "completed" ? "rgba(52,211,153,0.2)"
                                 : ev.status === "started"   ? "rgba(96,165,250,0.2)"
                                 : ev.status === "awaiting_approval" ? "rgba(251,191,36,0.2)"
                                                             : "rgba(248,113,113,0.2)"}`,
              color: ev.status === "completed" ? "#34d399" : ev.status === "started" ? "#60a5fa" : ev.status === "awaiting_approval" ? "#fbbf24" : "#f87171",
            }}>{ev.message}</span>
          ))}
        </div>
      )}

      {/* ── Split body: Left=Input  Right=Output ── */}
      <div style={{ flex: 1, display: "flex", overflow: "hidden", minHeight: 0 }}>

        {/* ═══ LEFT: Input panel ═══ */}
        <div style={{
          width: 340, minWidth: 280, maxWidth: 420,
          borderRight: "1px solid #151820",
          display: "flex", flexDirection: "column",
          overflow: "hidden", flexShrink: 0,
        }}>
          {/* Input panel header */}
          <div style={{
            padding: "10px 16px", flexShrink: 0,
            borderBottom: "1px solid #151820",
            display: "flex", alignItems: "center", gap: 8,
          }}>
            <span style={{ fontSize: 9, color: "#60a5fa", letterSpacing: "0.12em", fontWeight: 700 }}>INPUT</span>
            {savedInput && (
              <span style={{ fontSize: 9, color: "#f59e0b", padding: "1px 6px", borderRadius: 3, background: "rgba(245,158,11,0.1)", border: "1px solid rgba(245,158,11,0.2)" }}>
                overridden
              </span>
            )}
          </div>

          {/* Agent / Skill / Tools / Prompts info */}
          <div style={{
            padding: "12px 16px", flexShrink: 0,
            borderBottom: "1px solid #151820",
            display: "flex", flexDirection: "column", gap: 8,
          }}>
            {/* Agent + Skill */}
            <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
              <div style={{ display: "flex", flexDirection: "column", gap: 3 }}>
                <span style={{ fontSize: 8, color: "#374151", letterSpacing: "0.1em" }}>AGENT</span>
                <code style={{
                  fontSize: 11, color: "#a78bfa",
                  background: "rgba(167,139,250,0.1)", border: "1px solid rgba(167,139,250,0.2)",
                  padding: "2px 8px", borderRadius: 4,
                }}>{config.agent}</code>
              </div>
              <div style={{ display: "flex", flexDirection: "column", gap: 3 }}>
                <span style={{ fontSize: 8, color: "#374151", letterSpacing: "0.1em" }}>SKILL</span>
                <code style={{
                  fontSize: 11, color: "#34d399",
                  background: "rgba(52,211,153,0.08)", border: "1px solid rgba(52,211,153,0.2)",
                  padding: "2px 8px", borderRadius: 4,
                }}>{config.skill}</code>
              </div>
            </div>

            {/* Tools */}
            {config.tools.length > 0 && (
              <div>
                <span style={{ fontSize: 8, color: "#374151", letterSpacing: "0.1em", display: "block", marginBottom: 4 }}>TOOLS</span>
                <div style={{ display: "flex", gap: 4, flexWrap: "wrap" }}>
                  {config.tools.map((t, i) => (
                    <span key={i} style={{
                      fontSize: 10, padding: "2px 7px", borderRadius: 4,
                      background: "rgba(56,189,248,0.08)", border: "1px solid rgba(56,189,248,0.2)",
                      color: "#38bdf8",
                    }}>{t}</span>
                  ))}
                </div>
              </div>
            )}

            {/* Prompt file */}
            <div>
              <span style={{ fontSize: 8, color: "#374151", letterSpacing: "0.1em", display: "block", marginBottom: 3 }}>PROMPT</span>
              <code style={{ fontSize: 10, color: "#6b7280" }}>lib/prompts/{config.promptFile}</code>
            </div>

            {/* Notes */}
            {config.notes && (
              <div style={{
                fontSize: 10, color: "#4b5563", lineHeight: 1.5,
                padding: "6px 8px",
                background: "rgba(255,255,255,0.02)", borderRadius: 5,
                borderLeft: `2px solid ${meta.color}40`,
              }}>
                {config.notes}
              </div>
            )}
          </div>

          {/* Input data / edit textarea */}
          <div style={{ flex: 1, overflowY: "auto", padding: "12px 16px" }}>
            {editingInput ? (
              <textarea
                value={editInputText}
                onChange={e => setEditInputText(e.target.value)}
                style={{
                  width: "100%", height: "100%", minHeight: 240,
                  background: "#080a0e", border: "1px solid #374151",
                  borderRadius: 6, padding: "10px 12px",
                  fontFamily: "monospace", fontSize: 11, lineHeight: 1.6,
                  color: "#e2e8f0", resize: "none",
                }}
              />
            ) : effectiveInput ? (
              <div style={{ fontFamily: "monospace", fontSize: 11, lineHeight: 1.7 }}>
                <JsonNode data={effectiveInput} depth={0} />
              </div>
            ) : (
              <div style={{ color: "#374151", fontSize: 11, textAlign: "center", paddingTop: 32 }}>
                No input data
              </div>
            )}
          </div>
        </div>

        {/* ═══ RIGHT: Output panel ═══ */}
        <div style={{ flex: 1, display: "flex", flexDirection: "column", overflow: "hidden", minWidth: 0 }}>
          {/* Output panel header */}
          <div style={{
            padding: "10px 20px", flexShrink: 0,
            borderBottom: "1px solid #151820",
            display: "flex", alignItems: "center", gap: 8,
          }}>
            <span style={{ fontSize: 9, color: "#34d399", letterSpacing: "0.12em", fontWeight: 700 }}>OUTPUT</span>
            {hasOutput && (
              <span style={{ fontSize: 9, color: "#4b5563" }}>
                {Object.keys(output!).length} key{Object.keys(output!).length !== 1 ? "s" : ""}
              </span>
            )}
          </div>

          {/* Output content */}
          <div style={{ flex: 1, overflowY: "auto", padding: "20px" }}>
            {output ? (
              <StageContent stageKey={stageKey} output={output} />
            ) : (
              <div style={{
                display: "flex", flexDirection: "column",
                alignItems: "center", justifyContent: "center",
                height: "100%", minHeight: 280, color: "#374151",
              }}>
                {status === "running" ? (
                  <>
                    <div style={{
                      width: 44, height: 44, borderRadius: "50%",
                      border: `3px solid ${meta.color}30`, borderTopColor: meta.color,
                      animation: "spin 1s linear infinite", marginBottom: 16,
                    }} />
                    <div style={{ fontSize: 13, color: "#4b5563", fontWeight: 600 }}>Processing…</div>
                    <div style={{ fontSize: 11, color: "#374151", marginTop: 4 }}>{meta.label} is running</div>
                  </>
                ) : isAwaitingApproval ? (
                  <>
                    <div style={{ fontSize: 36, marginBottom: 14, color: "#fbbf24", opacity: 0.6 }}>⏸</div>
                    <div style={{ fontSize: 13, color: "#fbbf24", fontWeight: 600 }}>Awaiting Approval</div>
                    <div style={{ fontSize: 11, color: "#6b7280", marginTop: 4 }}>
                      Review output rồi nhấn &quot;Approve &amp; Run Next&quot; để tiếp tục
                    </div>
                  </>
                ) : status === "pending" ? (
                  <>
                    <div style={{ fontSize: 36, marginBottom: 14, opacity: 0.15 }}>{meta.icon}</div>
                    <div style={{ fontSize: 13, color: "#374151" }}>Waiting to start</div>
                  </>
                ) : (
                  <>
                    <div style={{ fontSize: 36, marginBottom: 14, opacity: 0.15 }}>—</div>
                    <div style={{ fontSize: 13, color: "#374151" }}>No output data</div>
                  </>
                )}
              </div>
            )}
          </div>

          {/* LIVE indicator */}
          {connected && (
            <div style={{
              padding: "6px 20px", flexShrink: 0,
              borderTop: "1px solid #151820",
              display: "flex", alignItems: "center", gap: 6,
            }}>
              <div style={{ width: 5, height: 5, borderRadius: "50%", background: "#34d399", animation: "pulse 2s infinite" }} />
              <span style={{ fontSize: 9, color: "#34d399", letterSpacing: "0.08em" }}>LIVE — SSE connected</span>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

// ─── Main Page ────────────────────────────────────────────────────────────────
export default function ProjectPage() {
  const params = useParams();
  const projectId = params.id as string;

  const [liveStatuses, setLiveStatuses] = useState<Record<string, StageStatus>>(
    Object.fromEntries(STAGE_ORDER.map(s => [s, "pending" as StageStatus]))
  );
  const [events, setEvents] = useState<PipelineEvent[]>([]);
  const [connected, setConnected] = useState(false);
  const [stages, setStages] = useState<Record<string, StageInfo>>({});
  const [selectedStage, setSelectedStage] = useState<string>("analysis");
  const [runState, setRunState] = useState<RunState | null>(null);

  const loadStages = useCallback(() => {
    // Use PAGE_API_BASE (8082) directly via fetch since api.ts uses 8080
    fetch(`${PAGE_API_BASE}/pipeline/${projectId}`, {
      headers: { "Content-Type": "application/json", "Authorization": `Bearer ${getToken()}` },
    })
      .then(r => r.ok ? r.json() : Promise.reject(r))
      .then((data: { stages?: StageInfo[] }) => {
        if (!data.stages) return;
        const map: Record<string, StageInfo> = {};
        for (const s of data.stages) map[s.stage] = s;
        setStages(map);
      })
      .catch(() => {
        // Fallback to api.ts if PAGE_API_BASE fails
        api.pipeline.getStages(projectId)
          .then(({ stages: stageList }) => {
            if (!stageList) return;
            const map: Record<string, StageInfo> = {};
            for (const s of stageList) map[s.stage] = s;
            setStages(map);
          })
          .catch(() => {});
      });
  }, [projectId]);

  const loadRunState = useCallback(() => {
    api.pipeline.getRunState(projectId)
      .then(setRunState)
      .catch(() => {}); // may 404 for old projects without step-by-step
  }, [projectId]);

  useEffect(() => { loadStages(); loadRunState(); }, [loadStages, loadRunState]);

  useEffect(() => {
    const source = connectPipelineSSE(
      projectId,
      (event) => {
        setConnected(true);
        setEvents(prev => [...prev, event]);
        setLiveStatuses(prev => {
          // awaiting_approval is a run-level status, not a stage status.
          // The stage itself is "completed" — don't overwrite it.
          if (event.status === "awaiting_approval") return prev;
          return {
            ...prev,
            [event.stage]: event.status === "started" ? "running" :
                            event.status === "completed" ? "completed" : "failed",
          };
        });
        if (event.status === "completed") {
          setSelectedStage(event.stage);
          loadStages();
          loadRunState();
        }
        if (event.status === "awaiting_approval") {
          setSelectedStage(event.stage);
          loadStages();
          loadRunState();
        }
        if (event.status === "failed") loadStages();
      },
      () => setConnected(false)
    );
    return () => source.close();
  }, [projectId, loadStages, loadRunState]);

  const completedCount = STAGE_ORDER.filter(
    s => liveStatuses[s] === "completed" || stages[s]?.status === "completed"
  ).length;

  const shortId = projectId.slice(0, 8).toUpperCase();

  const overallStatus: "completed" | "running" | "failed" | "pending" | "awaiting_approval" =
    STAGE_ORDER.every(s => liveStatuses[s] === "completed" || stages[s]?.status === "completed") ? "completed" :
    runState?.currentStatus === "awaiting_approval" ? "awaiting_approval" :
    STAGE_ORDER.some(s => liveStatuses[s] === "running") ? "running" :
    STAGE_ORDER.some(s => liveStatuses[s] === "failed" || stages[s]?.status === "failed") ? "failed" :
    "pending";

  // ── Selected stage meta for right panel header ──
  const selectedMeta = STAGE_META[selectedStage] || { label: selectedStage, icon: "⚙", color: "#6b7280", desc: "" };
  const selectedLiveStatus = liveStatuses[selectedStage] ?? "pending";
  const selectedDbStatus = stages[selectedStage]?.status as StageStatus | undefined;
  const selectedStatus: StageStatus = selectedLiveStatus !== "pending" ? selectedLiveStatus : (selectedDbStatus || "pending");

  return (
    <>
      <style>{`
        @keyframes pulse { 0%,100%{opacity:1} 50%{opacity:0.3} }
        @keyframes spin { from{transform:rotate(0deg)} to{transform:rotate(360deg)} }
        * { box-sizing: border-box; }
        ::-webkit-scrollbar { width: 6px; height: 6px; }
        ::-webkit-scrollbar-track { background: transparent; }
        ::-webkit-scrollbar-thumb { background: #1e2028; border-radius: 3px; }
        ::-webkit-scrollbar-thumb:hover { background: #2a2d3a; }
      `}</style>

      <div style={{
        display: "flex", height: "100vh", overflow: "hidden",
        fontFamily: "'Plus Jakarta Sans', sans-serif",
        background: "#0d0f13", color: "#e2e8f0",
      }}>

        {/* ── LEFT: Timeline ── */}
        <TimelinePanel
          stages={stages}
          liveStatuses={liveStatuses}
          selectedStage={selectedStage}
          onSelectStage={setSelectedStage}
          completedCount={completedCount}
          projectId={projectId}
          runState={runState}
        />

        {/* ── RIGHT: Detail ── */}
        <div style={{ flex: 1, display: "flex", flexDirection: "column", overflow: "hidden" }}>

          {/* CHANGE 1 & 2: Top header bar — height 56px khớp với timeline header
              Chứa: ← Projects | stage icon + label + desc | status badge + live dot */}
          <div style={{
            padding: "0 24px", height: 56,
            background: "#111318", borderBottom: "1px solid #1e2028",
            display: "flex", alignItems: "center", gap: 14,
            flexShrink: 0,
          }}>
            {/* Back button */}
            <Link href="/projects" style={{
              display: "flex", alignItems: "center", gap: 6,
              fontSize: 11, color: "#4b5563", textDecoration: "none",
              padding: "4px 10px", borderRadius: 6,
              border: "1px solid #1e2028", background: "rgba(255,255,255,0.02)",
              transition: "color 0.15s", whiteSpace: "nowrap", flexShrink: 0,
            }}>
              ← Projects
            </Link>

            <div style={{ fontSize: 11, color: "#374151", flexShrink: 0 }}>/</div>

            {/* Stage label + desc từ STAGE_META — CHANGE 2 */}
            <div style={{
              display: "flex", alignItems: "center", gap: 8,
              flex: 1, minWidth: 0, overflow: "hidden",
            }}>
              <span style={{
                fontSize: 16, color: selectedMeta.color, flexShrink: 0,
              }}>{selectedMeta.icon}</span>
              <div style={{ minWidth: 0 }}>
                <div style={{
                  fontSize: 13, fontWeight: 700, color: "#e2e8f0",
                  overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap",
                }}>
                  {selectedMeta.label}
                </div>
                <div style={{
                  fontSize: 10, color: "#6b7280",
                  overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap",
                }}>
                  {selectedMeta.desc || shortId}
                </div>
              </div>
              <StatusBadge status={selectedStatus} />
            </div>

            {/* Overall status badge */}
            <StatusBadge status={overallStatus} />

            {/* Live dot */}
            <div style={{
              display: "flex", alignItems: "center", gap: 6,
              padding: "4px 12px", borderRadius: 6,
              background: connected ? "rgba(52,211,153,0.08)" : "rgba(255,255,255,0.03)",
              border: `1px solid ${connected ? "rgba(52,211,153,0.2)" : "#1e2028"}`,
              flexShrink: 0,
            }}>
              <div style={{
                width: 6, height: 6, borderRadius: "50%",
                background: connected ? "#34d399" : "#374151",
                boxShadow: connected ? "0 0 6px #34d399" : "none",
                animation: connected ? "pulse 2s infinite" : "none",
              }} />
              <span style={{ fontSize: 10, color: connected ? "#34d399" : "#4b5563", fontWeight: 600, letterSpacing: "0.08em" }}>
                {connected ? "LIVE" : "OFFLINE"}
              </span>
            </div>
          </div>

          {/* Stage detail scrollable */}
          <StageDetailPanel
            stageKey={selectedStage}
            stage={stages[selectedStage]}
            liveStatus={liveStatuses[selectedStage] ?? "pending"}
            connected={connected}
            events={events}
            projectId={projectId}
            onReload={() => { loadStages(); loadRunState(); }}
            runState={runState}
          />
        </div>
      </div>
    </>
  );
}
