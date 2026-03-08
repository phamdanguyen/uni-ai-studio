"use client";
import { useEffect, useState, useRef, useCallback } from "react";
import { useParams } from "next/navigation";
import Link from "next/link";
import { connectPipelineSSE, type PipelineEvent } from "@/lib/sse";
import { api, type StageInfo } from "@/lib/api";

// ─── Stage metadata ───────────────────────────────────────────────────────────
const STAGE_META: Record<string, { label: string; icon: string; color: string }> = {
  analysis:     { label: "Story Analysis",    icon: "◈", color: "#f59e0b" },
  planning:     { label: "Pipeline Planning", icon: "⊞", color: "#a78bfa" },
  characters:   { label: "Character Design",  icon: "◉", color: "#34d399" },
  locations:    { label: "Location Design",   icon: "◎", color: "#38bdf8" },
  storyboard:   { label: "Storyboard",        icon: "▣", color: "#fb923c" },
  media_gen:    { label: "Media Generation",  icon: "◐", color: "#f472b6" },
  quality_check:{ label: "Quality Check",     icon: "◆", color: "#86efac" },
  voice:        { label: "Voice & TTS",       icon: "◉", color: "#c084fc" },
  assembly:     { label: "Final Assembly",    icon: "▶", color: "#fbbf24" },
};

const STAGE_ORDER = [
  "analysis","planning","characters","locations",
  "storyboard","media_gen","quality_check","voice","assembly",
];

type StageStatus = "pending" | "running" | "completed" | "failed";

// ─── JSON Tree Viewer ─────────────────────────────────────────────────────────
function JsonNode({ data, depth = 0 }: { data: unknown; depth?: number }) {
  const [collapsed, setCollapsed] = useState(depth > 1);
  const indent = depth * 16;

  if (data === null) return <span style={{ color: "#6b7280" }}>null</span>;
  if (typeof data === "boolean") return <span style={{ color: "#f472b6" }}>{String(data)}</span>;
  if (typeof data === "number") return <span style={{ color: "#38bdf8" }}>{data}</span>;
  if (typeof data === "string") {
    // URL detection
    if (data.startsWith("http")) {
      const isImg = /\.(jpg|jpeg|png|webp|gif)(\?|$)/i.test(data);
      const isVid = /\.(mp4|webm|mov)(\?|$)/i.test(data);
      return (
        <span>
          <a href={data} target="_blank" rel="noreferrer"
            style={{ color: "#fbbf24", textDecoration: "underline", wordBreak: "break-all" }}>
            {data}
          </a>
          {isImg && (
            <span style={{ marginLeft: 6, fontSize: 10, color: "#f472b6", cursor: "pointer" }}
              onClick={() => window.open(data, "_blank")}>
              [preview]
            </span>
          )}
          {isVid && (
            <span style={{ marginLeft: 6, fontSize: 10, color: "#38bdf8" }}>[video]</span>
          )}
        </span>
      );
    }
    return <span style={{ color: "#86efac" }}>&quot;{data}&quot;</span>;
  }

  if (Array.isArray(data)) {
    if (data.length === 0) return <span style={{ color: "#4b5563" }}>[]</span>;
    return (
      <span>
        <button onClick={() => setCollapsed(v => !v)}
          style={{ background: "none", border: "none", color: "#f59e0b", cursor: "pointer", padding: "0 4px", fontSize: 11 }}>
          {collapsed ? `▶ Array[${data.length}]` : `▼ Array[${data.length}]`}
        </button>
        {!collapsed && (
          <div style={{ marginLeft: indent + 16 }}>
            {data.map((item, i) => (
              <div key={i} style={{ marginBottom: 2 }}>
                <span style={{ color: "#4b5563", fontSize: 10 }}>{i}: </span>
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
          style={{ background: "none", border: "none", color: "#a78bfa", cursor: "pointer", padding: "0 4px", fontSize: 11 }}>
          {collapsed ? `▶ {${keys.length}}` : `▼ {${keys.length}}`}
        </button>
        {!collapsed && (
          <div style={{ marginLeft: indent + 16 }}>
            {keys.map(k => (
              <div key={k} style={{ marginBottom: 3 }}>
                <span style={{ color: "#94a3b8", fontSize: 11 }}>{k}: </span>
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

// ─── Storyboard Panel Grid ─────────────────────────────────────────────────────
function StoryboardView({ panels }: { panels: Record<string, unknown>[] }) {
  const [expanded, setExpanded] = useState<number | null>(null);
  return (
    <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(200px, 1fr))", gap: 12, marginTop: 12 }}>
      {panels.map((panel, i) => {
        const imgUrl = panel.imageUrl as string | undefined;
        const prompt = panel.imagePrompt as string | undefined;
        const desc = panel.description as string | undefined;
        return (
          <div key={i} onClick={() => setExpanded(expanded === i ? null : i)}
            style={{
              borderRadius: 10, overflow: "hidden", cursor: "pointer",
              border: "1px solid rgba(245,158,11,0.2)",
              background: "rgba(245,158,11,0.04)",
              transition: "all 0.2s",
            }}>
            <div style={{
              height: imgUrl ? 0 : 100,
              background: "rgba(0,0,0,0.4)",
              display: "flex", alignItems: "center", justifyContent: "center",
              fontSize: 11, color: "#4b5563",
            }}>
              {!imgUrl && `Panel ${i + 1}`}
            </div>
            {imgUrl && (
              <img src={imgUrl} alt={`Panel ${i + 1}`}
                style={{ width: "100%", aspectRatio: "16/9", objectFit: "cover", display: "block" }}
                onError={(e) => { (e.target as HTMLImageElement).style.display = "none"; }} />
            )}
            <div style={{ padding: "8px 10px" }}>
              <div style={{ fontSize: 10, color: "#f59e0b", fontWeight: 600, marginBottom: 4 }}>
                {`PANEL ${i + 1}`}
              </div>
              {desc && <p style={{ fontSize: 11, color: "#9ca3af", margin: 0, lineHeight: 1.4 }}>{desc.slice(0, 80)}{desc.length > 80 ? "…" : ""}</p>}
              {expanded === i && prompt && (
                <p style={{ fontSize: 10, color: "#6b7280", marginTop: 6, fontFamily: "monospace", lineHeight: 1.5 }}>{prompt}</p>
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
}

// ─── Image Gallery ─────────────────────────────────────────────────────────────
function ImageGallery({ results }: { results: Record<string, unknown>[] }) {
  const [lightbox, setLightbox] = useState<string | null>(null);
  const images = results.filter(r => r.imageUrl);
  return (
    <>
      <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(180px, 1fr))", gap: 10, marginTop: 12 }}>
        {images.map((item, i) => {
          const url = item.imageUrl as string;
          const status = item.status as string;
          return (
            <div key={i} onClick={() => setLightbox(url)}
              style={{
                borderRadius: 8, overflow: "hidden", cursor: "pointer",
                border: "1px solid rgba(244,114,182,0.2)",
                position: "relative",
              }}>
              <img src={url} alt={`Generated ${i + 1}`}
                style={{ width: "100%", aspectRatio: "1/1", objectFit: "cover", display: "block" }}
                onError={(e) => { (e.target as HTMLImageElement).style.display = "none"; }} />
              <div style={{
                position: "absolute", bottom: 0, left: 0, right: 0,
                padding: "4px 8px", background: "linear-gradient(transparent, rgba(0,0,0,0.7))",
                fontSize: 9, color: status === "completed" ? "#86efac" : "#f87171",
              }}>
                {status === "completed" ? "✓ done" : status}
              </div>
            </div>
          );
        })}
      </div>
      {lightbox && (
        <div onClick={() => setLightbox(null)} style={{
          position: "fixed", inset: 0, background: "rgba(0,0,0,0.9)",
          display: "flex", alignItems: "center", justifyContent: "center",
          zIndex: 1000, cursor: "zoom-out",
        }}>
          <img src={lightbox} alt="Preview"
            style={{ maxWidth: "90vw", maxHeight: "90vh", objectFit: "contain", borderRadius: 8 }} />
          <div style={{ position: "absolute", top: 16, right: 24, color: "#fff", fontSize: 28, cursor: "pointer" }}
            onClick={() => setLightbox(null)}>✕</div>
          <a href={lightbox} target="_blank" rel="noreferrer"
            style={{ position: "absolute", bottom: 24, color: "#fbbf24", fontSize: 12, textDecoration: "none" }}
            onClick={e => e.stopPropagation()}>
            Open full image ↗
          </a>
        </div>
      )}
    </>
  );
}

// ─── Video Player ──────────────────────────────────────────────────────────────
function VideoPlayer({ url }: { url: string }) {
  return (
    <div style={{ marginTop: 12, borderRadius: 10, overflow: "hidden", border: "1px solid rgba(56,189,248,0.2)" }}>
      <video controls style={{ width: "100%", maxHeight: 360, background: "#000", display: "block" }}>
        <source src={url} />
      </video>
      <div style={{ padding: "8px 12px", background: "rgba(0,0,0,0.4)", fontSize: 11 }}>
        <a href={url} target="_blank" rel="noreferrer" style={{ color: "#38bdf8", textDecoration: "none" }}>
          ↗ Open video
        </a>
      </div>
    </div>
  );
}

// ─── Smart Output Renderer ─────────────────────────────────────────────────────
function SmartOutput({ data }: { data: Record<string, unknown> }) {
  // Detect media gallery (media_gen results)
  const results = data.results as Record<string, unknown>[] | undefined;
  if (Array.isArray(results) && results.some(r => r.imageUrl)) {
    return <ImageGallery results={results} />;
  }

  // Detect storyboard panels
  const panels = data.panels as Record<string, unknown>[] | undefined;
  if (Array.isArray(panels) && panels.length > 0) {
    return <StoryboardView panels={panels} />;
  }

  // Detect video URL at top level
  const videoUrl = (data.videoUrl || data.url) as string | undefined;
  if (videoUrl && /\.(mp4|webm|mov)(\?|$)/i.test(videoUrl)) {
    return <VideoPlayer url={videoUrl} />;
  }

  // Detect user-uploaded media
  const userMedia = data.userMedia as Record<string, unknown> | undefined;
  if (userMedia?.url) {
    const url = userMedia.url as string;
    const isImg = /\.(jpg|jpeg|png|webp|gif)(\?|$)/i.test(url) || (userMedia.mimeType as string || "").startsWith("image/");
    const isVid = /\.(mp4|webm|mov)(\?|$)/i.test(url) || (userMedia.mimeType as string || "").startsWith("video/");
    return (
      <div style={{ marginTop: 12 }}>
        <div style={{ fontSize: 10, color: "#f59e0b", marginBottom: 8, letterSpacing: "0.08em" }}>USER MEDIA</div>
        {isImg && <img src={url} alt="user media" style={{ maxWidth: "100%", borderRadius: 8, maxHeight: 300, objectFit: "contain" }} />}
        {isVid && <VideoPlayer url={url} />}
        {!isImg && !isVid && <a href={url} target="_blank" rel="noreferrer" style={{ color: "#fbbf24" }}>{url}</a>}
      </div>
    );
  }

  // Default: JSON tree
  return (
    <div style={{
      marginTop: 10, padding: 14, borderRadius: 8,
      background: "rgba(0,0,0,0.35)", border: "1px solid rgba(255,255,255,0.06)",
      fontFamily: "monospace", fontSize: 11, lineHeight: 1.7, overflowX: "auto", maxHeight: 400, overflowY: "auto",
    }}>
      <JsonNode data={data} depth={0} />
    </div>
  );
}

// ─── Stage Detail Modal ────────────────────────────────────────────────────────
function StageDetailModal({
  stageKey, dbStage, meta, onClose,
}: {
  stageKey: string;
  dbStage: StageInfo;
  meta: { label: string; icon: string; color: string };
  onClose: () => void;
}) {
  const [tab, setTab] = useState<"input" | "output">("input");

  const input = dbStage.input ?? null;
  const output = dbStage.output ?? null;

  const tabStyle = (active: boolean, color: string) => ({
    padding: "7px 18px",
    borderRadius: 7,
    border: "none",
    cursor: "pointer",
    fontSize: 11,
    fontWeight: 700,
    letterSpacing: "0.07em",
    background: active ? `${color}22` : "transparent",
    color: active ? color : "#4b5563",
    transition: "all 0.15s",
  } as React.CSSProperties);

  return (
    <div style={{
      position: "fixed", inset: 0, background: "rgba(0,0,0,0.85)",
      display: "flex", alignItems: "center", justifyContent: "center", zIndex: 999,
    }} onClick={onClose}>
      <div onClick={(e) => e.stopPropagation()} style={{
        width: "min(860px, 96vw)", maxHeight: "88vh", borderRadius: 16,
        background: "#0a0c11", border: `1px solid ${meta.color}33`,
        display: "flex", flexDirection: "column", overflow: "hidden",
      }}>
        {/* Header */}
        <div style={{
          padding: "18px 22px", borderBottom: `1px solid rgba(255,255,255,0.06)`,
          display: "flex", alignItems: "center", gap: 10,
        }}>
          <span style={{ fontSize: 18, color: meta.color }}>{meta.icon}</span>
          <span style={{ fontWeight: 700, fontSize: 13, color: "#f9fafb", letterSpacing: "0.04em" }}>
            {meta.label.toUpperCase()}
          </span>
          {dbStage.startedAt && (
            <span style={{ fontSize: 10, color: "#374151", fontFamily: "monospace", marginLeft: 8 }}>
              {new Date(dbStage.startedAt).toLocaleTimeString()}
              {dbStage.finishedAt && ` → ${new Date(dbStage.finishedAt).toLocaleTimeString()}`}
            </span>
          )}
          <button onClick={onClose} style={{
            marginLeft: "auto", background: "none", border: "none",
            color: "#6b7280", cursor: "pointer", fontSize: 20, lineHeight: 1,
          }}>✕</button>
        </div>

        {/* Tabs */}
        <div style={{
          padding: "12px 22px 0", borderBottom: "1px solid rgba(255,255,255,0.06)",
          display: "flex", gap: 4,
        }}>
          <button style={tabStyle(tab === "input", "#38bdf8")} onClick={() => setTab("input")}>
            ↑ INPUT
          </button>
          <button style={tabStyle(tab === "output", meta.color)} onClick={() => setTab("output")}>
            ↓ OUTPUT
          </button>
        </div>

        {/* Body */}
        <div style={{ flex: 1, overflowY: "auto", padding: "18px 22px" }}>
          {tab === "input" && (
            <div>
              {input ? (
                <>
                  <p style={{ fontSize: 10, color: "#4b5563", letterSpacing: "0.08em", marginBottom: 10 }}>
                    PAYLOAD GỬI TỚI AGENT · {stageKey.toUpperCase()}
                  </p>
                  {/* Hiển thị trường "story" riêng nếu có vì thường rất dài */}
                  {typeof (input as Record<string, unknown>).story === "string" && (
                    <div style={{ marginBottom: 14 }}>
                      <div style={{ fontSize: 10, color: "#38bdf8", marginBottom: 6, letterSpacing: "0.06em" }}>
                        STORY TEXT
                      </div>
                      <pre style={{
                        margin: 0, padding: "12px 14px", borderRadius: 8,
                        background: "rgba(56,189,248,0.05)", border: "1px solid rgba(56,189,248,0.12)",
                        fontSize: 12, color: "#94a3b8", whiteSpace: "pre-wrap", wordBreak: "break-word",
                        fontFamily: "monospace", lineHeight: 1.6, maxHeight: 200, overflowY: "auto",
                      }}>
                        {(input as Record<string, unknown>).story as string}
                      </pre>
                    </div>
                  )}
                  {/* Phần còn lại của input */}
                  {(() => {
                    const rest = { ...(input as Record<string, unknown>) };
                    delete rest.story;
                    if (Object.keys(rest).length === 0) return null;
                    return (
                      <div>
                        <div style={{ fontSize: 10, color: "#38bdf8", marginBottom: 6, letterSpacing: "0.06em" }}>
                          CONTEXT DATA
                        </div>
                        <div style={{
                          padding: 14, borderRadius: 8,
                          background: "rgba(0,0,0,0.4)", border: "1px solid rgba(255,255,255,0.06)",
                          fontFamily: "monospace", fontSize: 11, lineHeight: 1.7,
                          overflowX: "auto", maxHeight: 380, overflowY: "auto",
                        }}>
                          <JsonNode data={rest} depth={0} />
                        </div>
                      </div>
                    );
                  })()}
                </>
              ) : (
                <div style={{ textAlign: "center", padding: 48, color: "#374151" }}>
                  <div style={{ fontSize: 24, marginBottom: 8 }}>—</div>
                  <p style={{ fontSize: 12 }}>Input chưa được ghi lại cho stage này</p>
                  <p style={{ fontSize: 10, color: "#1f2937", marginTop: 4 }}>
                    Cập nhật backend để lưu input payload
                  </p>
                </div>
              )}
            </div>
          )}

          {tab === "output" && (
            <div>
              {output ? (
                <>
                  <p style={{ fontSize: 10, color: "#4b5563", letterSpacing: "0.08em", marginBottom: 10 }}>
                    KẾT QUẢ TỪ AGENT · {stageKey.toUpperCase()}
                  </p>
                  <SmartOutput data={output} />
                </>
              ) : dbStage.error ? (
                <div style={{
                  padding: 14, borderRadius: 8,
                  background: "rgba(239,68,68,0.06)", border: "1px solid rgba(239,68,68,0.2)",
                }}>
                  <div style={{ fontSize: 10, color: "#ef4444", marginBottom: 6, letterSpacing: "0.06em" }}>ERROR</div>
                  <p style={{ fontSize: 12, color: "#f87171", margin: 0, fontFamily: "monospace" }}>{dbStage.error}</p>
                </div>
              ) : (
                <div style={{ textAlign: "center", padding: 48, color: "#374151" }}>
                  <div style={{ fontSize: 24, marginBottom: 8 }}>—</div>
                  <p style={{ fontSize: 12 }}>Chưa có output</p>
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

// ─── Edit Modal ────────────────────────────────────────────────────────────────
function EditModal({
  projectId, stage, initialData, onClose, onSaved,
}: {
  projectId: string;
  stage: string;
  initialData: Record<string, unknown>;
  onClose: () => void;
  onSaved: (data: Record<string, unknown>) => void;
}) {
  const [text, setText] = useState(JSON.stringify(initialData, null, 2));
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  async function handleSave() {
    let parsed: Record<string, unknown>;
    try {
      parsed = JSON.parse(text);
    } catch {
      setError("Invalid JSON");
      return;
    }
    setSaving(true);
    try {
      await fetch(
        `${process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080"}/pipeline/${projectId}/stage/${stage}/output`,
        { method: "PATCH", headers: { "Content-Type": "application/json" }, body: JSON.stringify(parsed) }
      );
      onSaved(parsed);
      onClose();
    } catch (e) {
      setError(String(e));
    } finally {
      setSaving(false);
    }
  }

  return (
    <div style={{
      position: "fixed", inset: 0, background: "rgba(0,0,0,0.8)",
      display: "flex", alignItems: "center", justifyContent: "center", zIndex: 999,
    }} onClick={onClose}>
      <div onClick={e => e.stopPropagation()} style={{
        width: "min(700px, 94vw)", borderRadius: 14,
        background: "#0f1117", border: "1px solid rgba(245,158,11,0.3)",
        padding: 24, display: "flex", flexDirection: "column", gap: 14,
      }}>
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
          <span style={{ fontSize: 13, fontWeight: 600, color: "#f59e0b", letterSpacing: "0.06em" }}>
            EDIT · {stage.toUpperCase()}
          </span>
          <button onClick={onClose} style={{ background: "none", border: "none", color: "#6b7280", cursor: "pointer", fontSize: 18 }}>✕</button>
        </div>
        <textarea
          value={text}
          onChange={e => { setText(e.target.value); setError(""); }}
          style={{
            width: "100%", height: 380, fontFamily: "monospace", fontSize: 12,
            background: "rgba(0,0,0,0.5)", border: "1px solid rgba(255,255,255,0.1)",
            borderRadius: 8, padding: 12, color: "#86efac", resize: "vertical", boxSizing: "border-box",
          }}
        />
        {error && <p style={{ color: "#f87171", fontSize: 11, margin: 0 }}>{error}</p>}
        <div style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
          <button onClick={onClose} style={{
            padding: "8px 18px", borderRadius: 8, border: "1px solid rgba(255,255,255,0.1)",
            background: "transparent", color: "#6b7280", cursor: "pointer", fontSize: 12,
          }}>Cancel</button>
          <button onClick={handleSave} disabled={saving} style={{
            padding: "8px 20px", borderRadius: 8, border: "none",
            background: saving ? "#6b7280" : "#f59e0b", color: "#000",
            fontWeight: 700, cursor: saving ? "not-allowed" : "pointer", fontSize: 12,
          }}>
            {saving ? "Saving…" : "Save"}
          </button>
        </div>
      </div>
    </div>
  );
}

// ─── Media Upload Modal ────────────────────────────────────────────────────────
function MediaModal({
  projectId, stage, onClose, onUploaded,
}: {
  projectId: string;
  stage: string;
  onClose: () => void;
  onUploaded: (data: Record<string, unknown>) => void;
}) {
  const [url, setUrl] = useState("");
  const [label, setLabel] = useState("");
  const [mime, setMime] = useState("image/png");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  async function handleSubmit() {
    if (!url.trim()) { setError("URL is required"); return; }
    setSaving(true);
    try {
      const res = await fetch(
        `${process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080"}/pipeline/${projectId}/stage/${stage}/media`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ url, label, mimeType: mime }),
        }
      );
      if (!res.ok) throw new Error(await res.text());
      onUploaded({ userMedia: { url, label, mimeType: mime } });
      onClose();
    } catch (e) {
      setError(String(e));
    } finally {
      setSaving(false);
    }
  }

  const mimeTypes = ["image/png","image/jpeg","image/webp","video/mp4","video/webm","audio/mpeg","audio/wav"];

  return (
    <div style={{
      position: "fixed", inset: 0, background: "rgba(0,0,0,0.8)",
      display: "flex", alignItems: "center", justifyContent: "center", zIndex: 999,
    }} onClick={onClose}>
      <div onClick={e => e.stopPropagation()} style={{
        width: "min(500px, 94vw)", borderRadius: 14,
        background: "#0f1117", border: "1px solid rgba(244,114,182,0.3)",
        padding: 24, display: "flex", flexDirection: "column", gap: 14,
      }}>
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
          <span style={{ fontSize: 13, fontWeight: 600, color: "#f472b6", letterSpacing: "0.06em" }}>
            UPLOAD MEDIA · {stage.toUpperCase()}
          </span>
          <button onClick={onClose} style={{ background: "none", border: "none", color: "#6b7280", cursor: "pointer", fontSize: 18 }}>✕</button>
        </div>
        <p style={{ fontSize: 11, color: "#6b7280", margin: 0 }}>
          Paste a public URL to a hosted image or video. This will override the AI-generated output for this stage.
        </p>

        {[
          { label: "Media URL *", value: url, onChange: setUrl, placeholder: "https://..." },
          { label: "Label (optional)", value: label, onChange: setLabel, placeholder: "e.g. hero image v2" },
        ].map(field => (
          <div key={field.label} style={{ display: "flex", flexDirection: "column", gap: 4 }}>
            <label style={{ fontSize: 10, color: "#9ca3af", letterSpacing: "0.06em" }}>{field.label.toUpperCase()}</label>
            <input value={field.value} onChange={e => field.onChange(e.target.value)} placeholder={field.placeholder}
              style={{
                padding: "9px 12px", borderRadius: 8, background: "rgba(0,0,0,0.4)",
                border: "1px solid rgba(255,255,255,0.08)", color: "#e5e7eb", fontSize: 12,
                fontFamily: "monospace", outline: "none",
              }} />
          </div>
        ))}

        <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
          <label style={{ fontSize: 10, color: "#9ca3af", letterSpacing: "0.06em" }}>MEDIA TYPE</label>
          <select value={mime} onChange={e => setMime(e.target.value)} style={{
            padding: "9px 12px", borderRadius: 8, background: "rgba(0,0,0,0.4)",
            border: "1px solid rgba(255,255,255,0.08)", color: "#e5e7eb", fontSize: 12,
          }}>
            {mimeTypes.map(m => <option key={m} value={m}>{m}</option>)}
          </select>
        </div>

        {error && <p style={{ color: "#f87171", fontSize: 11, margin: 0 }}>{error}</p>}

        <div style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
          <button onClick={onClose} style={{
            padding: "8px 18px", borderRadius: 8, border: "1px solid rgba(255,255,255,0.1)",
            background: "transparent", color: "#6b7280", cursor: "pointer", fontSize: 12,
          }}>Cancel</button>
          <button onClick={handleSubmit} disabled={saving} style={{
            padding: "8px 20px", borderRadius: 8, border: "none",
            background: saving ? "#6b7280" : "#f472b6", color: "#000",
            fontWeight: 700, cursor: saving ? "not-allowed" : "pointer", fontSize: 12,
          }}>
            {saving ? "Saving…" : "Add Media"}
          </button>
        </div>
      </div>
    </div>
  );
}

// ─── Stage Card ────────────────────────────────────────────────────────────────
function StageCard({
  projectId, stageKey, liveStatus, dbStage, onOutputUpdate,
}: {
  projectId: string;
  stageKey: string;
  liveStatus: StageStatus;
  dbStage?: StageInfo;
  onOutputUpdate: (stage: string, data: Record<string, unknown>) => void;
}) {
  const meta = STAGE_META[stageKey] || { label: stageKey, icon: "⚙", color: "#6b7280" };
  const status = liveStatus !== "pending" ? liveStatus : (dbStage?.status as StageStatus) || "pending";
  const [showEdit, setShowEdit] = useState(false);
  const [showMedia, setShowMedia] = useState(false);
  const [showDetail, setShowDetail] = useState(false);
  const [outputOverride, setOutputOverride] = useState<Record<string, unknown> | null>(null);

  const output = outputOverride ?? (dbStage?.output ? (dbStage.output as Record<string, unknown>) : null);

  const borderColor =
    status === "running"   ? `${meta.color}55` :
    status === "completed" ? `${meta.color}33` :
    status === "failed"    ? "rgba(239,68,68,0.3)" : "rgba(255,255,255,0.06)";

  const bgGlow =
    status === "running"   ? `${meta.color}08` :
    status === "completed" ? `${meta.color}05` : "transparent";

  return (
    <>
      <div style={{
        borderRadius: 12, border: `1px solid ${borderColor}`,
        background: bgGlow, padding: "16px 18px",
        transition: "all 0.3s", position: "relative",
      }}>
        {/* Header row */}
        <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 8 }}>
          <span style={{ fontSize: 16, color: meta.color, fontFamily: "monospace" }}>{meta.icon}</span>
          <span style={{ fontWeight: 600, fontSize: 12, letterSpacing: "0.04em", color: "#d1d5db" }}>
            {meta.label.toUpperCase()}
          </span>
          <span style={{ marginLeft: "auto", display: "flex", alignItems: "center", gap: 6 }}>
            {status === "running" && (
              <span style={{ fontSize: 10, color: meta.color, display: "flex", alignItems: "center", gap: 4 }}>
                <span style={{
                  width: 5, height: 5, borderRadius: "50%", background: meta.color,
                  display: "inline-block", animation: "pulse 1.4s infinite",
                }} />
                RUNNING
              </span>
            )}
            {status === "completed" && <span style={{ fontSize: 10, color: meta.color }}>✓ DONE</span>}
            {status === "failed"    && <span style={{ fontSize: 10, color: "#ef4444" }}>✗ FAILED</span>}
            {status === "pending"   && <span style={{ fontSize: 10, color: "#374151" }}>PENDING</span>}
          </span>
        </div>

        {/* Timing */}
        {dbStage?.startedAt && (
          <p style={{ fontSize: 9, color: "#374151", marginBottom: 6, fontFamily: "monospace" }}>
            {new Date(dbStage.startedAt).toLocaleTimeString()}
            {dbStage.finishedAt && ` → ${new Date(dbStage.finishedAt).toLocaleTimeString()}`}
          </p>
        )}

        {/* Error */}
        {dbStage?.error && (
          <p style={{ fontSize: 11, color: "#f87171", background: "rgba(239,68,68,0.06)", padding: "4px 8px", borderRadius: 6, marginBottom: 8 }}>
            {dbStage.error}
          </p>
        )}

        {/* Output */}
        {status === "completed" && output && (
          <SmartOutput data={output} />
        )}

        {/* Actions */}
        {(status === "completed" || status === "failed" || status === "running") && dbStage && (
          <div style={{ display: "flex", gap: 6, marginTop: 12 }}>
            <button onClick={() => setShowDetail(true)} style={{
              fontSize: 10, padding: "4px 10px", borderRadius: 6, cursor: "pointer",
              background: `${meta.color}10`, border: `1px solid ${meta.color}30`,
              color: meta.color, letterSpacing: "0.05em",
            }}>
              ⊙ Details
            </button>
            {(status === "completed" || status === "failed") && (<>
            <button onClick={() => setShowEdit(true)} style={{
              fontSize: 10, padding: "4px 10px", borderRadius: 6, cursor: "pointer",
              background: "rgba(245,158,11,0.08)", border: "1px solid rgba(245,158,11,0.2)",
              color: "#f59e0b", letterSpacing: "0.05em",
            }}>
              ✎ Edit JSON
            </button>
            <button onClick={() => setShowMedia(true)} style={{
              fontSize: 10, padding: "4px 10px", borderRadius: 6, cursor: "pointer",
              background: "rgba(244,114,182,0.08)", border: "1px solid rgba(244,114,182,0.2)",
              color: "#f472b6", letterSpacing: "0.05em",
            }}>
              ⊕ Add Media
            </button>
            </>)}
          </div>
        )}
      </div>

      {showDetail && dbStage && (
        <StageDetailModal
          stageKey={stageKey}
          dbStage={dbStage}
          meta={meta}
          onClose={() => setShowDetail(false)}
        />
      )}
      {showEdit && output && (
        <EditModal
          projectId={projectId}
          stage={stageKey}
          initialData={output}
          onClose={() => setShowEdit(false)}
          onSaved={(d) => { setOutputOverride(d); onOutputUpdate(stageKey, d); }}
        />
      )}
      {showMedia && (
        <MediaModal
          projectId={projectId}
          stage={stageKey}
          onClose={() => setShowMedia(false)}
          onUploaded={(d) => { setOutputOverride({ ...output, ...d }); onOutputUpdate(stageKey, { ...output, ...d }); }}
        />
      )}
    </>
  );
}

// ─── Main Page ─────────────────────────────────────────────────────────────────
export default function ProjectPage() {
  const params = useParams();
  const projectId = params.id as string;

  const [liveStatuses, setLiveStatuses] = useState<Record<string, StageStatus>>(
    Object.fromEntries(STAGE_ORDER.map((s) => [s, "pending" as StageStatus]))
  );
  const [events, setEvents] = useState<PipelineEvent[]>([]);
  const [connected, setConnected] = useState(false);
  const [dbStages, setDbStages] = useState<Record<string, StageInfo>>({});
  const logRef = useRef<HTMLDivElement>(null);

  const loadStages = useCallback(() => {
    api.pipeline.getStages(projectId)
      .then(({ stages }) => {
        if (!stages) return;
        const map: Record<string, StageInfo> = {};
        for (const s of stages) map[s.stage] = s;
        setDbStages(map);
      })
      .catch(() => {});
  }, [projectId]);

  useEffect(() => { loadStages(); }, [loadStages]);

  useEffect(() => {
    const source = connectPipelineSSE(
      projectId,
      (event) => {
        setConnected(true);
        setEvents((prev) => [...prev, event]);
        setLiveStatuses((prev) => ({
          ...prev,
          [event.stage]: event.status === "started" ? "running" :
                         event.status === "completed" ? "completed" : "failed",
        }));
        if (event.status === "completed" || event.status === "failed") {
          loadStages();
        }
      },
      () => setConnected(false)
    );
    return () => source.close();
  }, [projectId, loadStages]);

  useEffect(() => {
    logRef.current?.scrollTo(0, logRef.current.scrollHeight);
  }, [events]);

  const handleOutputUpdate = useCallback((stage: string, data: Record<string, unknown>) => {
    setDbStages(prev => ({
      ...prev,
      [stage]: { ...prev[stage], output: data as StageInfo["output"] },
    }));
  }, []);

  const completedCount = STAGE_ORDER.filter(
    (s) => liveStatuses[s] === "completed" || dbStages[s]?.status === "completed"
  ).length;
  const progress = (completedCount / STAGE_ORDER.length) * 100;
  const hasAnyData = STAGE_ORDER.some(s => dbStages[s]);

  return (
    <>
      <style>{`
        @keyframes pulse { 0%,100%{opacity:1} 50%{opacity:0.3} }
        @keyframes scanline { 0%{top:0} 100%{top:100%} }
        * { box-sizing: border-box; }
        ::-webkit-scrollbar { width: 4px; height: 4px; }
        ::-webkit-scrollbar-track { background: rgba(0,0,0,0.2); }
        ::-webkit-scrollbar-thumb { background: rgba(245,158,11,0.3); border-radius: 2px; }
      `}</style>

      <div style={{
        padding: "28px 32px", maxWidth: 1280, margin: "0 auto",
        fontFamily: "'Plus Jakarta Sans', sans-serif", color: "#e5e7eb",
        minHeight: "100vh",
      }}>

        {/* Header */}
        <div style={{ marginBottom: 32 }}>
          <Link href="/projects" style={{
            color: "#4b5563", fontSize: 11, textDecoration: "none",
            letterSpacing: "0.08em", alignItems: "center", gap: 4,
            marginBottom: 12, display: "block",
          }}>
            ← PROJECTS
          </Link>
          <div style={{ display: "flex", alignItems: "flex-start", justifyContent: "space-between" }}>
            <div>
              <h1 style={{
                fontSize: 28, fontWeight: 800, marginBottom: 6, letterSpacing: "-0.02em",
                color: "#f9fafb",
              }}>
                Pipeline Monitor
              </h1>
              <p style={{ fontSize: 11, color: "#374151", fontFamily: "monospace", letterSpacing: "0.06em" }}>
                {projectId}
              </p>
            </div>
            <div style={{ display: "flex", alignItems: "center", gap: 8, paddingTop: 4 }}>
              <span style={{
                width: 7, height: 7, borderRadius: "50%",
                background: connected ? "#34d399" : "#374151",
                display: "inline-block",
                boxShadow: connected ? "0 0 8px #34d399" : "none",
              }} />
              <span style={{ fontSize: 11, color: "#4b5563", letterSpacing: "0.06em" }}>
                {connected ? "LIVE" : hasAnyData ? "HISTORICAL" : "AWAITING"}
              </span>
            </div>
          </div>
        </div>

        {/* Progress Bar */}
        <div style={{ marginBottom: 32 }}>
          <div style={{ display: "flex", justifyContent: "space-between", marginBottom: 8 }}>
            <span style={{ fontSize: 10, color: "#374151", letterSpacing: "0.1em" }}>PROGRESS</span>
            <span style={{ fontSize: 11, fontFamily: "monospace", color: "#f59e0b" }}>
              {completedCount} / {STAGE_ORDER.length}
            </span>
          </div>
          <div style={{ height: 3, background: "rgba(255,255,255,0.05)", borderRadius: 4, overflow: "hidden" }}>
            <div style={{
              height: "100%",
              background: "linear-gradient(90deg, #f59e0b, #f472b6)",
              borderRadius: 4, width: `${progress}%`,
              transition: "width 0.6s ease",
              boxShadow: "0 0 12px rgba(245,158,11,0.5)",
            }} />
          </div>
        </div>

        {/* Stage Grid */}
        <div style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 12, marginBottom: 32 }}>
          {STAGE_ORDER.map((key) => (
            <StageCard
              key={key}
              projectId={projectId}
              stageKey={key}
              liveStatus={liveStatuses[key]}
              dbStage={dbStages[key]}
              onOutputUpdate={handleOutputUpdate}
            />
          ))}
        </div>

        {/* Live Event Log */}
        {events.length > 0 && (
          <section>
            <div style={{
              fontSize: 10, fontWeight: 700, letterSpacing: "0.12em",
              color: "#374151", marginBottom: 12,
            }}>
              LIVE LOG
            </div>
            <div ref={logRef} style={{
              height: 220, overflowY: "auto", borderRadius: 10,
              border: "1px solid rgba(255,255,255,0.05)",
              background: "rgba(0,0,0,0.35)", padding: "12px 16px",
              fontFamily: "monospace", fontSize: 11,
            }}>
              {events.map((event, i) => (
                <div key={i} style={{ display: "flex", gap: 12, marginBottom: 5, alignItems: "baseline" }}>
                  <span style={{ color: "#374151", minWidth: 68, fontSize: 10 }}>
                    {new Date(event.timestamp).toLocaleTimeString()}
                  </span>
                  <span style={{
                    fontSize: 9, padding: "1px 7px", borderRadius: 3, letterSpacing: "0.06em",
                    background: event.status === "started"   ? "rgba(245,158,11,0.1)" :
                                 event.status === "completed" ? "rgba(52,211,153,0.1)" : "rgba(239,68,68,0.1)",
                    color: event.status === "started"   ? "#f59e0b" :
                           event.status === "completed" ? "#34d399" : "#f87171",
                  }}>
                    {event.status.toUpperCase()}
                  </span>
                  <span style={{ color: "#6b7280" }}>{event.message}</span>
                </div>
              ))}
            </div>
          </section>
        )}
      </div>
    </>
  );
}
