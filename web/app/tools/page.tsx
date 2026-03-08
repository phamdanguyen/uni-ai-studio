"use client";
import { useEffect, useState } from "react";
import { api, type ToolInfo } from "@/lib/api";

const toolMeta: Record<string, { icon: string; gradient: string }> = {
  fal_image:       { icon: "🎨", gradient: "linear-gradient(135deg, #3b82f6, #06b6d4)" },
  fal_video:       { icon: "🎥", gradient: "linear-gradient(135deg, #3b82f6, #06b6d4)" },
  ark_image:       { icon: "🌋", gradient: "linear-gradient(135deg, #f97316, #ef4444)" },
  ark_video:       { icon: "🌋", gradient: "linear-gradient(135deg, #f97316, #ef4444)" },
  google_image:    { icon: "🔍", gradient: "linear-gradient(135deg, #22c55e, #14b8a6)" },
  minimax_video:   { icon: "🎞", gradient: "linear-gradient(135deg, #a855f7, #ec4899)" },
  vidu_video:      { icon: "📹", gradient: "linear-gradient(135deg, #6366f1, #8b5cf6)" },
  qwen_tts:        { icon: "🔊", gradient: "linear-gradient(135deg, #f59e0b, #f97316)" },
  image_generator: { icon: "✨", gradient: "linear-gradient(135deg, #f5b240, #e8973f)" },
  video_generator: { icon: "✨", gradient: "linear-gradient(135deg, #e8973f, #f97316)" },
};

export default function ToolsPage() {
  const [tools, setTools] = useState<ToolInfo[]>([]);

  useEffect(() => {
    api.tools.list().then((data) => setTools(data.tools || []));
  }, []);

  return (
    <div style={{ padding: 32, maxWidth: 1200 }}>
      <div className="anim-in" style={{ marginBottom: 32 }}>
        <h1 style={{ fontSize: 24, fontWeight: 700, marginBottom: 4, letterSpacing: "-0.02em" }}>
          <span style={{ color: "#f5b240" }}>⚡</span> Tools
        </h1>
        <p style={{ fontSize: 14, color: "#8b8fa3" }}>
          13 AI generation tools — FAL, Ark, Google, MiniMax, Vidu, Qwen
        </p>
      </div>

      <div style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 16 }}>
        {tools.map((tool) => {
          const m = toolMeta[tool.name] || { icon: "🔧", gradient: "linear-gradient(135deg, #6b7280, #4b5563)" };
          return (
            <div key={tool.name} className="glass-card" style={{ padding: 20 }}>
              <div style={{ display: "flex", alignItems: "center", gap: 12, marginBottom: 12 }}>
                <div style={{
                  width: 40, height: 40, borderRadius: 10, display: "flex", alignItems: "center", justifyContent: "center",
                  fontSize: 18, background: m.gradient,
                }}>
                  {m.icon}
                </div>
                <div style={{ fontSize: 13, fontFamily: "'JetBrains Mono', monospace" }}>{tool.name}</div>
              </div>
              <p style={{ fontSize: 13, color: "#8b8fa3" }}>{tool.description}</p>
              <div style={{ marginTop: 12, display: "flex", gap: 8 }}>
                {tool.name.includes("image") && <span className="badge-accent">Image</span>}
                {tool.name.includes("video") && <span className="badge-accent">Video</span>}
                {tool.name.includes("tts") && <span className="badge-accent">Audio</span>}
                {tool.name.includes("generator") && <span className="badge-accent">Auto</span>}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
