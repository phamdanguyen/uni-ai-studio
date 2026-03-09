"use client";
import { useEffect, useState, useRef, useCallback } from "react";
import { useParams } from "next/navigation";
import Link from "next/link";
import { connectPipelineSSE, type PipelineEvent } from "@/lib/sse";
import { api, type StageInfo } from "@/lib/api";

// ─── Stage metadata ────────────────────────────────────────────────────────────
const STAGE_META: Record<string, { label: string; icon: string; color: string; desc: string }> = {
  analysis:     { label: "Story Analysis",    icon: "◈", color: "#f59e0b", desc: "Parse và phân tích kịch bản" },
  planning:     { label: "Pipeline Planning", icon: "⊞", color: "#a78bfa", desc: "Lập kế hoạch sản xuất" },
  characters:   { label: "Character Design",  icon: "◉", color: "#34d399", desc: "Thiết kế nhân vật" },
  locations:    { label: "Location Design",   icon: "◎", color: "#38bdf8", desc: "Thiết kế địa điểm" },
  storyboard:   { label: "Storyboard",        icon: "▣", color: "#fb923c", desc: "Phân cảnh quay" },
  media_gen:    { label: "Media Generation",  icon: "◐", color: "#f472b6", desc: "Tạo ảnh & video AI" },
  quality_check:{ label: "Quality Check",     icon: "◆", color: "#86efac", desc: "Kiểm tra chất lượng" },
  voice:        { label: "Voice & TTS",       icon: "◉", color: "#c084fc", desc: "Lồng tiếng tự động" },
  assembly:     { label: "Final Assembly",    icon: "▶", color: "#fbbf24", desc: "Ghép nối thành phẩm" },
};

const STAGE_ORDER = [
  "analysis","planning","characters","locations",
  "storyboard","media_gen","quality_check","voice","assembly",
];

type StageStatus = "pending" | "running" | "completed" | "failed";

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

// ─── Image Lightbox Gallery ───────────────────────────────────────────────────
function ImageGallery({ results }: { results: Record<string, unknown>[] }) {
  const [lightbox, setLightbox] = useState<string | null>(null);
  const images = results.filter(r => r.imageUrl);
  return (
    <>
      <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(140px, 1fr))", gap: 8, marginTop: 10 }}>
        {images.map((item, i) => {
          const url = item.imageUrl as string;
          const status = item.status as string;
          return (
            <div key={i} onClick={() => setLightbox(url)} style={{
              borderRadius: 8, overflow: "hidden", cursor: "pointer",
              border: "1px solid rgba(244,114,182,0.2)", position: "relative",
            }}>
              <img src={url} alt={`Gen ${i + 1}`}
                style={{ width: "100%", aspectRatio: "1/1", objectFit: "cover", display: "block" }}
                onError={(e) => { (e.target as HTMLImageElement).style.display = "none"; }} />
              <div style={{
                position: "absolute", bottom: 0, left: 0, right: 0,
                padding: "3px 6px",
                background: "linear-gradient(transparent, rgba(0,0,0,0.75))",
                fontSize: 9, color: status === "completed" ? "#86efac" : "#f87171",
              }}>
                {status === "completed" ? "✓" : status}
              </div>
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
          <a href={lightbox} target="_blank" rel="noreferrer" onClick={e => e.stopPropagation()}
            style={{ position: "absolute", bottom: 24, color: "#fbbf24", fontSize: 12, textDecoration: "none" }}>
            Open full ↗
          </a>
        </div>
      )}
    </>
  );
}

// ─── Storyboard Grid ──────────────────────────────────────────────────────────
function StoryboardView({ panels }: { panels: Record<string, unknown>[] }) {
  const [expanded, setExpanded] = useState<number | null>(null);
  return (
    <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(180px, 1fr))", gap: 10, marginTop: 10 }}>
      {panels.map((panel, i) => {
        const imgUrl = panel.imageUrl as string | undefined;
        const desc = panel.description as string | undefined;
        const prompt = panel.imagePrompt as string | undefined;
        return (
          <div key={i} onClick={() => setExpanded(expanded === i ? null : i)} style={{
            borderRadius: 9, overflow: "hidden", cursor: "pointer",
            border: `1px solid ${expanded === i ? "rgba(251,146,60,0.4)" : "rgba(251,146,60,0.15)"}`,
            background: "rgba(251,146,60,0.03)", transition: "all 0.2s",
          }}>
            {imgUrl
              ? <img src={imgUrl} alt={`Panel ${i+1}`}
                  style={{ width: "100%", aspectRatio: "16/9", objectFit: "cover", display: "block" }}
                  onError={(e) => { (e.target as HTMLImageElement).style.display = "none"; }} />
              : <div style={{ height: 80, display: "flex", alignItems: "center", justifyContent: "center", background: "rgba(0,0,0,0.3)", fontSize: 10, color: "#374151" }}>
                  Panel {i + 1}
                </div>
            }
            <div style={{ padding: "7px 9px" }}>
              <div style={{ fontSize: 9, color: "#fb923c", fontWeight: 700, marginBottom: 3 }}>PANEL {i + 1}</div>
              {desc && <p style={{ fontSize: 10, color: "#9ca3af", margin: 0, lineHeight: 1.4 }}>{desc.slice(0, 70)}{desc.length > 70 ? "…" : ""}</p>}
              {expanded === i && prompt && <p style={{ fontSize: 9, color: "#6b7280", marginTop: 5, fontFamily: "monospace", lineHeight: 1.5 }}>{prompt}</p>}
            </div>
          </div>
        );
      })}
    </div>
  );
}

// ─── Smart Output ─────────────────────────────────────────────────────────────
function SmartOutput({ data }: { data: Record<string, unknown> }) {
  const results = data.results as Record<string, unknown>[] | undefined;
  if (Array.isArray(results) && results.some(r => r.imageUrl)) {
    return <ImageGallery results={results} />;
  }
  const panels = data.panels as Record<string, unknown>[] | undefined;
  if (Array.isArray(panels) && panels.length > 0) {
    return <StoryboardView panels={panels} />;
  }
  const videoUrl = (data.videoUrl || data.url) as string | undefined;
  if (videoUrl && /\.(mp4|webm|mov)(\?|$)/i.test(videoUrl)) {
    return (
      <div style={{ marginTop: 10, borderRadius: 8, overflow: "hidden", border: "1px solid rgba(56,189,248,0.2)" }}>
        <video controls style={{ width: "100%", maxHeight: 280, background: "#000", display: "block" }}>
          <source src={videoUrl} />
        </video>
      </div>
    );
  }
  return (
    <div style={{
      marginTop: 8, padding: 12, borderRadius: 8,
      background: "rgba(0,0,0,0.3)", border: "1px solid rgba(255,255,255,0.06)",
      fontFamily: "monospace", fontSize: 11, lineHeight: 1.7,
      overflowX: "auto", maxHeight: 320, overflowY: "auto",
    }}>
      <JsonNode data={data} depth={0} />
    </div>
  );
}

// ─── Stage prompt reference (system prompts used by each stage) ───────────────
const STAGE_PROMPTS: Record<string, { title: string; prompts: { name: string; content: string }[] }> = {
  analysis: {
    title: "System Prompt (inline)",
    prompts: [{
      name: "analyze_story",
      content: `You are an expert film director analyzing a story for video production.

Extract and return as JSON:
{
  "title": "story title or suggested title",
  "genre": "drama/comedy/action/horror/romance/fantasy/sci-fi",
  "mood": "overall mood",
  "themes": ["theme1", "theme2"],
  "characters": [
    {"name": "...", "role": "protagonist/antagonist/supporting", "description": "..."}
  ],
  "locations": [
    {"name": "...", "description": "...", "mood": "..."}
  ],
  "plotStructure": {
    "setup": "...",
    "conflict": "...",
    "climax": "...",
    "resolution": "..."
  },
  "estimatedEpisodes": 1,
  "estimatedDurationMinutes": 5,
  "suggestedArtStyle": "realistic/anime/comic/watercolor"
}`,
    }],
  },
  planning: {
    title: "System Prompt (inline)",
    prompts: [{
      name: "plan_pipeline",
      content: `You are a production planner for an AI filmmaking studio.
Given the story analysis, decide the optimal execution pipeline.

Return a JSON plan:
{
  "strategy": "full/skip-analysis/screenplay-direct/incremental",
  "reasoning": "why this strategy",
  "steps": [
    {"agent": "character", "skill": "analyze_characters", "depends": []},
    {"agent": "location", "skill": "analyze_locations", "depends": []},
    {"agent": "storyboard", "skill": "create_storyboard", "depends": ["character.analyze_characters", "location.analyze_locations"]}
  ]
}

Rules:
- Short story (<2000 chars): skip episode split
- Has screenplay already: skip story_to_script, go direct to storyboard
- Has characters defined: skip character analysis`,
    }],
  },
  characters: {
    title: "Prompt File: agent_character_profile.en.txt",
    prompts: [{
      name: "agent_character_profile",
      content: `You are a casting and character-asset analyst.
Analyze the input text and produce structured character profiles for visual production.

Input text: {input}
Existing character library info: {characters_lib_info}

Goals:
1. Identify characters that should appear visually.
2. Exclude pure background extras and abstract entities.
3. Build profile fields needed for downstream visual generation.
4. Capture naming/alias mapping, especially first-person references.

Output format (JSON only):
{
  "characters": [{ "name": "...", "aliases": [], "introduction": "...", "gender": "...", "age_range": "...", "role_level": "S", "archetype": "...", "personality_tags": [], "era_period": "...", "social_class": "...", "occupation": "...", "costume_tier": 3, "suggested_colors": [], "primary_identifier": "...", "visual_keywords": [], "expected_appearances": [{"id":1,"change_reason":"initial appearance"}] }],
  "new_characters": [],
  "updated_characters": []
}`,
    }],
  },
  locations: {
    title: "Prompt File: select_location.en.txt + location_create.en.txt",
    prompts: [{
      name: "select_location",
      content: `You are a location analyst.
Extract locations from story text that need dedicated background art assets.

Return JSON:
{
  "locations": [
    { "name": "...", "summary": "...", "has_crowd": false, "crowd_description": "", "descriptions": ["cinematic description 1", "..."] }
  ]
}`,
    }],
  },
  storyboard: {
    title: "4-Phase Prompts: Plan → Cinematography → Acting → Detail",
    prompts: [
      {
        name: "Phase 1 — agent_storyboard_plan",
        content: `You are a storyboard planning director.
Generate an initial panel sequence for one clip.

Inputs: character library, location library, character introductions, character appearances, clip metadata JSON, clip content.

Output format (JSON array only):
[{ "panel_number": 1, "description": "visual action description", "characters": [{"name":"...","appearance":"..."}], "location": "...", "scene_type": "daily", "source_text": "...", "shot_type": "medium shot", "camera_move": "static", "video_prompt": "short visual motion prompt", "duration": 3 }]`,
      },
      {
        name: "Phase 2 — agent_cinematographer",
        content: `You are a cinematography planner.
For each panel, generate a concise photography rule package.

Inputs: panel count, panels JSON, location context, character context.

Output format (JSON array only):
[{ "panel_number": 1, "composition": "framing and layout rule", "lighting": "light direction and quality", "color_palette": "dominant palette", "atmosphere": "visual mood", "technical_notes": "camera/depth/motion notes" }]`,
      },
      {
        name: "Phase 3 — agent_acting_direction",
        content: `You are an experienced Acting Director.
Generate acting notes for each character in each panel.

Inputs: total panel count, panels JSON, character info.

Output format (JSON array only):
[{ "panel_number": 1, "characters": [{"name":"...","acting":"One-sentence visual acting direction"}] }]`,
      },
      {
        name: "Phase 4 — agent_storyboard_detail",
        content: `You are a storyboard detail composer.
Merge plan + cinematography + acting into final enriched panel data.

Output: final panels JSON with all fields merged: description, shot_type, camera_move, composition, lighting, color_palette, atmosphere, acting, image_prompt.`,
      },
    ],
  },
  media_gen: {
    title: "Prompt File: single_panel_image.en.txt",
    prompts: [{
      name: "single_panel_image",
      content: `You are a professional storyboard image artist.
Generate exactly one high-quality image for one panel.

Constraints:
1. No text, subtitles, labels, numbers, watermarks, or symbols in the image.
2. Output exactly one frame — no collage or multi-frame.

Inputs: aspect_ratio, storyboard panel data JSON, source text, style requirement.

Execution rules:
1. Respect panel composition, character placement, and action logic.
2. Use reference images for style/identity consistency only.
3. Repaint the background according to shot type and angle.
4. If storyboard conflicts with source text, keep narrative logic from source text.`,
    }],
  },
  quality_check: {
    title: "System Prompt (inline)",
    prompts: [{
      name: "quality_review",
      content: `You are a quality review specialist for AI-generated media.
Evaluate the generated media against the storyboard expectations.

For each panel result, assess:
- Visual quality (composition, clarity, artifacts)
- Prompt adherence (does it match the storyboard description?)
- Character consistency (if characters are present)
- Overall production readiness

Return JSON with scores and recommendations.`,
    }],
  },
  voice: {
    title: "Prompt File: voice_analysis.en.txt",
    prompts: [{
      name: "voice_analysis",
      content: `You are a dialogue voice-line analyzer.
Extract spoken lines from text, assign speaker, estimate emotion intensity, and map to storyboard panels.

Inputs: input text, character library, character introductions, storyboard JSON.

Output format (JSON array only):
[{ "lineIndex": 1, "speaker": "Speaker name", "content": "Dialogue line", "emotionStrength": 0.3, "matchedPanel": {"storyboardId": "...", "panelIndex": 0} }]

Rules:
1. Extract spoken dialogue only (quoted speech, direct speech, inner speech to be voiced).
2. Exclude pure narration and action-only description.
3. emotionStrength must be between 0.1 and 0.5.
4. Match panel by order + speaker consistency + semantic relevance.`,
    }],
  },
  assembly: {
    title: "No LLM prompt — pure data assembly",
    prompts: [{
      name: "assembly",
      content: `No LLM call for this stage.

Assembly combines all pipeline outputs into final structured result:
- panels: from storyboard stage
- media: from media_gen stage (images/videos per panel)
- voices: from voice stage (TTS audio per dialogue line)
- characters: from characters stage
- locations: from locations stage

Output is the merged object ready for video rendering.`,
    }],
  },
};

// ─── Stage production context: agent, skill, tools ────────────────────────────
const STAGE_CONTEXT: Record<string, {
  agent: string; agentDesc: string;
  skill: string; skillDesc: string;
  tools: { name: string; desc: string }[];
}> = {
  analysis: {
    agent: "director", agentDesc: "Đạo diễn AI — phân tích truyện, lập kế hoạch sản xuất",
    skill: "analyze_story", skillDesc: "Phân tích nhân vật, bối cảnh, cảm xúc, cấu trúc narrative",
    tools: [],
  },
  planning: {
    agent: "director", agentDesc: "Đạo diễn AI — phân tích truyện, lập kế hoạch sản xuất",
    skill: "plan_pipeline", skillDesc: "Quyết định strategy phù hợp dựa trên input type",
    tools: [],
  },
  characters: {
    agent: "character", agentDesc: "Họa sĩ nhân vật AI — thiết kế, duy trì consistency nhân vật",
    skill: "analyze_characters", skillDesc: "Extract structured character profiles from story text",
    tools: [],
  },
  locations: {
    agent: "location", agentDesc: "Họa sĩ bối cảnh AI — thiết kế, duy trì consistency địa điểm",
    skill: "analyze_locations", skillDesc: "Extract locations needing dedicated background assets",
    tools: [],
  },
  storyboard: {
    agent: "storyboard", agentDesc: "Chuyên gia phân cảnh và visual storytelling",
    skill: "create_storyboard", skillDesc: "4-phase pipeline: Plan → Cinematography → Acting → Detail merge",
    tools: [],
  },
  media_gen: {
    agent: "media", agentDesc: "Nhà sản xuất media AI — sinh ảnh, video từ storyboard",
    skill: "generate_batch", skillDesc: "Batch generate images for multiple storyboard panels",
    tools: [
      { name: "image_generator", desc: "Auto-dispatch image generation to best available provider" },
      { name: "image_fal", desc: "FAL Banana Pro/2: Sinh ảnh AI 2K/4K" },
      { name: "image_ark", desc: "Ark Seedream 4.5: Sinh ảnh 4K từ Volcengine" },
      { name: "image_google", desc: "Google Gemini/Imagen: Sinh ảnh qua Google AI" },
      { name: "video_generator", desc: "Auto-dispatch video generation to best available provider" },
    ],
  },
  quality_check: {
    agent: "media", agentDesc: "Nhà sản xuất media AI — đánh giá chất lượng output",
    skill: "quality_review", skillDesc: "LLM-based quality evaluation of generated media",
    tools: [],
  },
  voice: {
    agent: "voice", agentDesc: "Đạo diễn âm thanh AI — phân tích giọng, TTS, lip sync",
    skill: "analyze_voices", skillDesc: "Extract spoken lines, assign speakers, estimate emotions",
    tools: [
      { name: "voice_designer", desc: "Thiết kế giọng nói cho nhân vật dựa trên personality" },
      { name: "tts_generator", desc: "Qwen TTS: Text-to-Speech với nhiều giọng, hỗ trợ SSML" },
      { name: "lip_sync", desc: "Lip sync audio với video" },
    ],
  },
  assembly: {
    agent: "pipeline", agentDesc: "Orchestrator — ghép nối tất cả assets thành phẩm cuối",
    skill: "assemble", skillDesc: "Combine storyboard + media + voices + characters → final output",
    tools: [],
  },
};

// ─── Stage Input Schema ────────────────────────────────────────────────────────
const STAGE_INPUT_SCHEMA: Record<string, { key: string; label: string; type: "string" | "object" }[]> = {
  analysis:     [{ key: "story", label: "Story / Script", type: "string" }, { key: "inputType", label: "Input Type", type: "string" }],
  planning:     [{ key: "analysis", label: "Story Analysis", type: "object" }, { key: "budget", label: "Budget", type: "string" }, { key: "quality", label: "Quality Level", type: "string" }],
  characters:   [{ key: "story", label: "Story", type: "string" }, { key: "analysis", label: "Story Analysis", type: "object" }],
  locations:    [{ key: "story", label: "Story", type: "string" }, { key: "analysis", label: "Story Analysis", type: "object" }],
  storyboard:   [{ key: "story", label: "Story", type: "string" }, { key: "analysis", label: "Story Analysis", type: "object" }, { key: "characters", label: "Characters", type: "object" }, { key: "locations", label: "Locations", type: "object" }],
  media_gen:    [{ key: "storyboard", label: "Storyboard", type: "object" }, { key: "characters", label: "Characters", type: "object" }, { key: "locations", label: "Locations", type: "object" }],
  quality_check:[{ key: "media", label: "Media Output", type: "object" }, { key: "storyboard", label: "Storyboard", type: "object" }],
  voice:        [{ key: "story", label: "Story", type: "string" }, { key: "characters", label: "Characters", type: "object" }, { key: "storyboard", label: "Storyboard", type: "object" }],
  assembly:     [{ key: "media", label: "Media Output", type: "object" }, { key: "voices", label: "Voice Data", type: "object" }, { key: "storyboard", label: "Storyboard", type: "object" }],
};

function StagePromptViewer({ stageKey, color }: { stageKey: string; color: string }) {
  const [open, setOpen] = useState(false);
  const [activePrompt, setActivePrompt] = useState(0);
  const info = STAGE_PROMPTS[stageKey];
  if (!info) return null;
  const prompt = info.prompts[activePrompt];
  return (
    <div style={{ borderTop: `1px solid ${color}14` }}>
      <button
        onClick={() => setOpen(v => !v)}
        style={{
          width: "100%", padding: "10px 16px", background: "none", border: "none",
          cursor: "pointer", display: "flex", alignItems: "center", justifyContent: "space-between",
          color: "#4b5563", fontSize: 10,
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <span style={{ fontSize: 11 }}>📋</span>
          <span style={{ fontWeight: 600, letterSpacing: "0.06em" }}>SYSTEM PROMPTS</span>
          <span style={{
            fontSize: 8, padding: "1px 6px", borderRadius: 3,
            background: `${color}15`, color,
          }}>{info.prompts.length}</span>
        </div>
        <span style={{ fontSize: 10, transition: "transform 0.2s", transform: open ? "rotate(180deg)" : "none" }}>▾</span>
      </button>

      {open && (
        <div style={{ padding: "0 16px 14px" }}>
          <div style={{ fontSize: 9, color: "#374151", marginBottom: 8 }}>{info.title}</div>

          {/* Prompt selector tabs (if multiple) */}
          {info.prompts.length > 1 && (
            <div style={{ display: "flex", gap: 4, marginBottom: 10, flexWrap: "wrap" }}>
              {info.prompts.map((p, i) => (
                <button key={i} onClick={() => setActivePrompt(i)} style={{
                  fontSize: 9, padding: "3px 9px", borderRadius: 4, cursor: "pointer",
                  background: activePrompt === i ? `${color}18` : "rgba(255,255,255,0.03)",
                  border: `1px solid ${activePrompt === i ? color + "40" : "rgba(255,255,255,0.06)"}`,
                  color: activePrompt === i ? color : "#4b5563",
                  fontWeight: activePrompt === i ? 700 : 400,
                }}>
                  {p.name.replace(/Phase \d+ — /, "").replace("agent_", "")}
                </button>
              ))}
            </div>
          )}

          <div style={{ fontSize: 9, color: color, letterSpacing: "0.06em", marginBottom: 6, opacity: 0.8 }}>
            {prompt.name}
          </div>
          <pre style={{
            margin: 0, padding: "10px 12px", borderRadius: 7, overflowY: "auto",
            maxHeight: 260, background: "rgba(0,0,0,0.4)",
            border: "1px solid rgba(255,255,255,0.06)",
            fontFamily: "monospace", fontSize: 10, lineHeight: 1.65,
            color: "#94a3b8", whiteSpace: "pre-wrap", wordBreak: "break-word",
            boxSizing: "border-box",
          }}>
            {prompt.content}
          </pre>
        </div>
      )}
    </div>
  );
}

// ─── Stage Detail Panel (slide-in from right) ─────────────────────────────────
function StageDetailPanel({
  stageKey, dbStage, meta, projectId, onClose, onRetried,
}: {
  stageKey: string;
  dbStage: StageInfo;
  meta: { label: string; icon: string; color: string; desc: string };
  projectId: string;
  onClose: () => void;
  onRetried: () => void;
}) {
  const [tab, setTab] = useState<"output" | "input" | "retry">("input");
  const input  = dbStage.input  ?? null;
  const output = dbStage.output ?? null;

  const schema  = STAGE_INPUT_SCHEMA[stageKey] ?? [];
  const context = STAGE_CONTEXT[stageKey];

  // ── Editable input fields ──
  function buildInitialFields(): Record<string, string> {
    const r: Record<string, string> = {};
    for (const f of schema) {
      const raw = input ? (input as Record<string, unknown>)[f.key] : undefined;
      r[f.key] = raw === undefined ? "" : f.type === "object"
        ? JSON.stringify(raw, null, 2)
        : String(raw ?? "");
    }
    return r;
  }

  const [fieldValues, setFieldValues] = useState<Record<string, string>>(buildInitialFields);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [dirty,       setDirty]       = useState(false);
  const [saving,      setSaving]      = useState(false);
  const [saveOk,      setSaveOk]      = useState(false);
  const [retrying,    setRetrying]    = useState(false);
  const [actionError, setActionError] = useState("");

  function handleFieldChange(key: string, value: string) {
    setFieldValues(prev => ({ ...prev, [key]: value }));
    setFieldErrors(prev => ({ ...prev, [key]: "" }));
    setDirty(true);
    setSaveOk(false);
    setActionError("");
  }

  function handleFormatJson(key: string) {
    try {
      const p = JSON.parse(fieldValues[key]);
      setFieldValues(prev => ({ ...prev, [key]: JSON.stringify(p, null, 2) }));
      setFieldErrors(prev => ({ ...prev, [key]: "" }));
    } catch {
      setFieldErrors(prev => ({ ...prev, [key]: "JSON không hợp lệ" }));
    }
  }

  // ── Parse fields → object, returns null if validation fails ──
  function parseFields(): Record<string, unknown> | null {
    const parsed: Record<string, unknown> = {};
    const errors: Record<string, string>  = {};
    for (const f of schema) {
      const raw = fieldValues[f.key] ?? "";
      if (f.type === "object") {
        if (raw.trim() === "") { parsed[f.key] = null; continue; }
        try { parsed[f.key] = JSON.parse(raw); }
        catch { errors[f.key] = "JSON không hợp lệ"; }
      } else {
        parsed[f.key] = raw;
      }
    }
    if (Object.keys(errors).length > 0) { setFieldErrors(errors); return null; }
    return parsed;
  }

  // ── Save input only (no re-run) ──
  async function handleSaveInput() {
    const parsed = parseFields();
    if (!parsed) { setActionError("Vui lòng sửa lỗi bên dưới."); return; }
    setSaving(true); setActionError("");
    try {
      await api.pipeline.updateStageInput(projectId, stageKey, parsed);
      setDirty(false); setSaveOk(true);
      setTimeout(() => setSaveOk(false), 2500);
    } catch (e) { setActionError(String(e)); }
    finally { setSaving(false); }
  }

  // ── Retry with current (possibly edited) input ──
  async function handleRetryWithInput() {
    const parsed = parseFields();
    if (!parsed) { setActionError("Vui lòng sửa lỗi bên dưới."); return; }
    setRetrying(true); setActionError("");
    try {
      await api.pipeline.retryStage(projectId, stageKey, parsed);
      onClose(); onRetried();
    } catch (e) { setActionError(String(e)); setRetrying(false); }
  }

  // ── Retry with original stored input (no changes) ──
  async function handleRetryOriginal() {
    setRetrying(true); setActionError("");
    try {
      await api.pipeline.retryStage(projectId, stageKey, {});
      onClose(); onRetried();
    } catch (e) { setActionError(String(e)); setRetrying(false); }
  }

  const tabs = [
    { key: "input",  label: "Input & Agent", color: "#38bdf8" },
    { key: "output", label: "Output",        color: meta.color },
    { key: "retry",  label: "Retry",         color: "#f87171"  },
  ] as const;

  return (
    <>
      {/* Backdrop */}
      <div onClick={onClose} style={{
        position: "fixed", inset: 0, background: "rgba(0,0,0,0.5)",
        zIndex: 800, backdropFilter: "blur(2px)",
      }} />

      {/* Panel */}
      <div style={{
        position: "fixed", top: 0, right: 0, bottom: 0,
        width: "min(680px, 96vw)",
        background: "#0e1118",
        borderLeft: `1px solid ${meta.color}33`,
        zIndex: 801,
        display: "flex", flexDirection: "column",
        animation: "slide-in-right 0.25s cubic-bezier(0.4,0,0.2,1) both",
      }}>

        {/* ── Header ── */}
        <div style={{
          padding: "20px 24px",
          borderBottom: "1px solid rgba(255,255,255,0.06)",
          background: `linear-gradient(135deg, ${meta.color}08, transparent)`,
          flexShrink: 0,
        }}>
          <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: 8 }}>
            <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
              <div style={{
                width: 36, height: 36, borderRadius: 10,
                background: `${meta.color}15`, border: `1px solid ${meta.color}33`,
                display: "flex", alignItems: "center", justifyContent: "center",
                fontSize: 18, color: meta.color,
              }}>
                {meta.icon}
              </div>
              <div>
                <div style={{ fontWeight: 700, fontSize: 14, color: "#f9fafb", letterSpacing: "0.02em" }}>
                  {meta.label}
                </div>
                <div style={{ fontSize: 11, color: "#4b5563" }}>{meta.desc}</div>
              </div>
            </div>
            <button onClick={onClose} style={{
              background: "rgba(255,255,255,0.06)", border: "1px solid rgba(255,255,255,0.08)",
              color: "#6b7280", cursor: "pointer", fontSize: 14, width: 32, height: 32,
              borderRadius: 8, display: "flex", alignItems: "center", justifyContent: "center",
            }}>✕</button>
          </div>

          {dbStage.startedAt && (
            <div style={{
              display: "flex", gap: 16, padding: "8px 12px", borderRadius: 8,
              background: "rgba(0,0,0,0.25)", border: "1px solid rgba(255,255,255,0.05)",
            }}>
              <TimeCell label="Started"  value={fmt(dbStage.startedAt)} />
              {dbStage.finishedAt && <TimeCell label="Finished" value={fmt(dbStage.finishedAt)} />}
              {dbStage.finishedAt && <TimeCell label="Duration" value={dur(dbStage.startedAt, dbStage.finishedAt) || "—"} accent={meta.color} />}
            </div>
          )}
        </div>

        {/* ── Tabs ── */}
        <div style={{
          display: "flex", gap: 0, flexShrink: 0,
          borderBottom: "1px solid rgba(255,255,255,0.06)",
          padding: "0 24px",
        }}>
          {tabs.map(t => (
            <button key={t.key} onClick={() => setTab(t.key)} style={{
              padding: "12px 16px", border: "none", cursor: "pointer",
              background: "transparent", fontSize: 12, fontWeight: 700, letterSpacing: "0.06em",
              color: tab === t.key ? t.color : "#374151",
              borderBottom: tab === t.key ? `2px solid ${t.color}` : "2px solid transparent",
              transition: "all 0.15s", marginBottom: -1,
            }}>
              {t.label.toUpperCase()}
            </button>
          ))}
        </div>

        {/* ── Body ── */}
        <div style={{ flex: 1, overflowY: "auto", padding: "20px 24px" }}>

          {/* Error banner (stage level) */}
          {dbStage.error && (
            <div style={{
              marginBottom: 16, padding: "10px 14px", borderRadius: 8,
              background: "rgba(239,68,68,0.08)", border: "1px solid rgba(239,68,68,0.2)",
            }}>
              <div style={{ fontSize: 9, color: "#ef4444", letterSpacing: "0.1em", marginBottom: 4 }}>STAGE ERROR</div>
              <p style={{ fontSize: 12, color: "#f87171", margin: 0, fontFamily: "monospace", lineHeight: 1.5 }}>
                {dbStage.error}
              </p>
            </div>
          )}

          {/* ════ TAB: OUTPUT ════ */}
          {tab === "output" && (
            output ? (
              <div>
                <SmartOutput data={output} />
                <div style={{ marginTop: 16 }}>
                  <div style={{
                    display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: 6,
                  }}>
                    <span style={{ fontSize: 9, color: "#374151", letterSpacing: "0.1em" }}>RAW JSON</span>
                    <button
                      onClick={() => navigator.clipboard?.writeText(JSON.stringify(output, null, 2))}
                      style={{
                        fontSize: 9, padding: "2px 8px", borderRadius: 4, cursor: "pointer",
                        background: "rgba(255,255,255,0.05)", border: "1px solid rgba(255,255,255,0.08)",
                        color: "#4b5563",
                      }}
                    >Copy</button>
                  </div>
                  <pre style={{
                    margin: 0, padding: "12px 14px", borderRadius: 8, overflowY: "auto",
                    maxHeight: "calc(100vh - 420px)", minHeight: 120,
                    background: "rgba(0,0,0,0.35)", border: "1px solid rgba(255,255,255,0.06)",
                    fontFamily: "monospace", fontSize: 10.5, lineHeight: 1.65,
                    color: "#86efac", whiteSpace: "pre-wrap", wordBreak: "break-all",
                    boxSizing: "border-box",
                  }}>
                    {JSON.stringify(output, null, 2)}
                  </pre>
                </div>
              </div>
            ) : (
              <EmptyState label="No output yet"
                sub={dbStage.error ? "Stage failed — xem tab Retry để chạy lại" : "Stage chưa hoàn thành"} />
            )
          )}

          {/* ════ TAB: INPUT & AGENT ════ */}
          {tab === "input" && (
            <div>

              {/* Production context card */}
              {context && (
                <div style={{
                  marginBottom: 20, borderRadius: 10,
                  border: `1px solid ${meta.color}20`,
                  background: `${meta.color}06`, overflow: "hidden",
                }}>
                  {/* Agent row */}
                  <div style={{
                    padding: "12px 16px",
                    borderBottom: `1px solid ${meta.color}14`,
                    display: "flex", alignItems: "flex-start", gap: 12,
                  }}>
                    <div style={{
                      width: 28, height: 28, borderRadius: 8, flexShrink: 0,
                      background: `${meta.color}15`, border: `1px solid ${meta.color}30`,
                      display: "flex", alignItems: "center", justifyContent: "center",
                      fontSize: 13, color: meta.color,
                    }}>◈</div>
                    <div style={{ flex: 1, minWidth: 0 }}>
                      <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 2 }}>
                        <span style={{ fontSize: 10, fontWeight: 700, color: meta.color, letterSpacing: "0.06em" }}>
                          AGENT
                        </span>
                        <code style={{
                          fontSize: 11, padding: "1px 7px", borderRadius: 4,
                          background: `${meta.color}18`, color: "#f9fafb", fontFamily: "monospace",
                        }}>
                          {context.agent}
                        </code>
                      </div>
                      <div style={{ fontSize: 10, color: "#6b7280", lineHeight: 1.5 }}>{context.agentDesc}</div>
                    </div>
                  </div>

                  {/* Skill row */}
                  <div style={{
                    padding: "12px 16px",
                    borderBottom: context.tools.length > 0 ? `1px solid ${meta.color}14` : "none",
                    display: "flex", alignItems: "flex-start", gap: 12,
                  }}>
                    <div style={{
                      width: 28, height: 28, borderRadius: 8, flexShrink: 0,
                      background: "rgba(167,139,250,0.1)", border: "1px solid rgba(167,139,250,0.25)",
                      display: "flex", alignItems: "center", justifyContent: "center",
                      fontSize: 12, color: "#a78bfa",
                    }}>⚡</div>
                    <div style={{ flex: 1, minWidth: 0 }}>
                      <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 2 }}>
                        <span style={{ fontSize: 10, fontWeight: 700, color: "#a78bfa", letterSpacing: "0.06em" }}>
                          SKILL
                        </span>
                        <code style={{
                          fontSize: 11, padding: "1px 7px", borderRadius: 4,
                          background: "rgba(167,139,250,0.12)", color: "#f9fafb", fontFamily: "monospace",
                        }}>
                          {context.skill}
                        </code>
                      </div>
                      <div style={{ fontSize: 10, color: "#6b7280", lineHeight: 1.5 }}>{context.skillDesc}</div>
                    </div>
                  </div>

                  {/* Prompts expandable */}
                  {STAGE_PROMPTS[stageKey] && <StagePromptViewer stageKey={stageKey} color={meta.color} />}

                  {/* Tools rows */}
                  {context.tools.length > 0 && (
                    <div style={{ padding: "12px 16px" }}>
                      <div style={{ fontSize: 9, color: "#38bdf8", letterSpacing: "0.1em", fontWeight: 700, marginBottom: 8 }}>
                        TOOLS ({context.tools.length})
                      </div>
                      <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
                        {context.tools.map(tool => (
                          <div key={tool.name} style={{
                            display: "flex", alignItems: "flex-start", gap: 8,
                            padding: "6px 10px", borderRadius: 6,
                            background: "rgba(56,189,248,0.05)", border: "1px solid rgba(56,189,248,0.12)",
                          }}>
                            <span style={{ fontSize: 10, color: "#38bdf8", marginTop: 1 }}>⚙</span>
                            <div>
                              <code style={{ fontSize: 10, color: "#e5e7eb", fontFamily: "monospace" }}>{tool.name}</code>
                              <div style={{ fontSize: 9, color: "#4b5563", marginTop: 1, lineHeight: 1.4 }}>{tool.desc}</div>
                            </div>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}
                </div>
              )}

              {/* Divider */}
              <div style={{
                display: "flex", alignItems: "center", gap: 10, marginBottom: 16,
              }}>
                <div style={{ flex: 1, height: 1, background: "rgba(255,255,255,0.05)" }} />
                <span style={{ fontSize: 9, color: "#374151", letterSpacing: "0.1em" }}>INPUT FIELDS</span>
                <div style={{ flex: 1, height: 1, background: "rgba(255,255,255,0.05)" }} />
              </div>

              {/* No input saved */}
              {!input && (
                <div style={{
                  padding: "18px 16px", borderRadius: 8, marginBottom: 16,
                  background: "rgba(0,0,0,0.2)", border: "1px solid rgba(255,255,255,0.06)",
                  textAlign: "center",
                }}>
                  <div style={{ fontSize: 11, color: "#4b5563", marginBottom: 4 }}>Input chưa được lưu</div>
                  <div style={{ fontSize: 10, color: "#1f2937" }}>Chuyển sang tab Retry để chạy lại stage này</div>
                </div>
              )}

              {/* Fields */}
              {input && (
                <div>
                  {dirty && (
                    <div style={{ display: "flex", justifyContent: "flex-end", marginBottom: 10 }}>
                      <span style={{ fontSize: 9, color: "#f59e0b", letterSpacing: "0.06em" }}>● CHƯA LƯU</span>
                    </div>
                  )}

                  {schema.map((field, idx) => (
                    <div key={field.key} style={{ marginBottom: 16 }}>
                      <div style={{
                        display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: 6,
                      }}>
                        <div style={{ fontSize: 9, letterSpacing: "0.1em", textTransform: "uppercase", color: `${meta.color}aa` }}>
                          {field.label}
                        </div>
                        <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
                          {field.type === "object" && (
                            <button onClick={() => handleFormatJson(field.key)} style={{
                              fontSize: 9, padding: "2px 7px", borderRadius: 4, cursor: "pointer",
                              background: "rgba(75,85,99,0.2)", border: "1px solid rgba(75,85,99,0.3)", color: "#4b5563",
                            }}>Format JSON</button>
                          )}
                          <span style={{
                            fontSize: 8, padding: "1px 6px", borderRadius: 3, letterSpacing: "0.06em", fontWeight: 700,
                            background: field.type === "object" ? "rgba(167,139,250,0.12)" : "rgba(56,189,248,0.12)",
                            color: field.type === "object" ? "#a78bfa" : "#38bdf8",
                          }}>
                            {field.type.toUpperCase()}
                          </span>
                        </div>
                      </div>
                      <textarea
                        value={fieldValues[field.key] ?? ""}
                        onChange={e => handleFieldChange(field.key, e.target.value)}
                        style={{
                          width: "100%",
                          minHeight: field.type === "object" ? 140 : 60,
                          fontFamily: field.type === "object" ? "monospace" : "inherit",
                          fontSize: 11,
                          background: "rgba(0,0,0,0.4)",
                          border: `1px solid ${fieldErrors[field.key]
                            ? "rgba(239,68,68,0.6)"
                            : dirty ? "rgba(56,189,248,0.25)" : "rgba(255,255,255,0.08)"}`,
                          borderRadius: 8, padding: 10,
                          color: field.type === "object" ? "#86efac" : "#e5e7eb",
                          resize: "vertical", boxSizing: "border-box",
                          lineHeight: 1.6, outline: "none", transition: "border-color 0.2s",
                        }}
                      />
                      {fieldErrors[field.key] && (
                        <p style={{ color: "#f87171", fontSize: 10, margin: "4px 0 0", fontFamily: "monospace" }}>
                          {fieldErrors[field.key]}
                        </p>
                      )}
                      {idx < schema.length - 1 && (
                        <div style={{ marginTop: 16, borderBottom: "1px solid rgba(255,255,255,0.04)" }} />
                      )}
                    </div>
                  ))}

                  {actionError && (
                    <p style={{ color: "#f87171", fontSize: 11, margin: "4px 0 12px", fontFamily: "monospace" }}>
                      {actionError}
                    </p>
                  )}

                  {/* Save Input button — biên tập, không re-run */}
                  <button
                    onClick={handleSaveInput}
                    disabled={saving || !dirty}
                    style={{
                      width: "100%", padding: "10px 16px", borderRadius: 8, cursor: saving || !dirty ? "not-allowed" : "pointer",
                      background: saveOk ? "rgba(52,211,153,0.15)" : (saving || !dirty) ? "rgba(55,65,81,0.4)" : "rgba(56,189,248,0.12)",
                      border: `1px solid ${saveOk ? "rgba(52,211,153,0.4)" : (saving || !dirty) ? "transparent" : "rgba(56,189,248,0.3)"}`,
                      color: saveOk ? "#34d399" : (saving || !dirty) ? "#374151" : "#38bdf8",
                      fontWeight: 700, fontSize: 12, letterSpacing: "0.05em", transition: "all 0.2s",
                    }}
                  >
                    {saving ? "Đang lưu…" : saveOk ? "✓ Đã lưu" : "↓ Update Input"}
                  </button>
                  <div style={{ fontSize: 9, color: "#374151", marginTop: 6, textAlign: "center" }}>
                    Lưu thay đổi input để biên tập — không re-run stage
                  </div>
                </div>
              )}
            </div>
          )}

          {/* ════ TAB: RETRY ════ */}
          {tab === "retry" && (
            <div>
              {/* Explanation */}
              <div style={{
                marginBottom: 20, padding: "14px 16px", borderRadius: 10,
                background: "rgba(248,113,113,0.06)", border: "1px solid rgba(248,113,113,0.15)",
              }}>
                <div style={{ fontSize: 9, color: "#f87171", letterSpacing: "0.1em", marginBottom: 6 }}>RETRY STAGE</div>
                <p style={{ fontSize: 11, color: "#94a3b8", margin: 0, lineHeight: 1.7 }}>
                  Chạy lại stage <strong style={{ color: "#f9fafb" }}>{meta.label}</strong> với agent{" "}
                  <code style={{ color: meta.color, fontSize: 11 }}>{context?.agent ?? stageKey}</code>.
                  {dirty && (
                    <span style={{ color: "#f59e0b" }}> Có thay đổi input chưa lưu — lưu trước ở tab Input & Agent nếu muốn retry với input mới.</span>
                  )}
                </p>
              </div>

              {actionError && (
                <p style={{ color: "#f87171", fontSize: 11, margin: "0 0 12px", fontFamily: "monospace" }}>
                  {actionError}
                </p>
              )}

              {/* Retry with current saved input */}
              <button
                onClick={handleRetryWithInput}
                disabled={retrying}
                style={{
                  width: "100%", padding: "12px 16px", borderRadius: 8, marginBottom: 10,
                  cursor: retrying ? "not-allowed" : "pointer",
                  background: retrying ? "rgba(55,65,81,0.4)" : "rgba(248,113,113,0.12)",
                  border: `1px solid ${retrying ? "transparent" : "rgba(248,113,113,0.3)"}`,
                  color: retrying ? "#374151" : "#f87171",
                  fontWeight: 700, fontSize: 12, letterSpacing: "0.05em",
                }}
              >
                {retrying ? "Đang khởi động…" : "↺ Retry với Input hiện tại"}
              </button>
              <div style={{ fontSize: 9, color: "#374151", marginBottom: 16, textAlign: "center" }}>
                Dùng input đã lưu trong DB (bao gồm thay đổi nếu đã Update Input)
              </div>

              {/* Retry with original pipeline-built input */}
              <button
                onClick={handleRetryOriginal}
                disabled={retrying}
                style={{
                  width: "100%", padding: "12px 16px", borderRadius: 8,
                  cursor: retrying ? "not-allowed" : "pointer",
                  background: retrying ? "rgba(55,65,81,0.4)" : "rgba(107,114,128,0.08)",
                  border: `1px solid ${retrying ? "transparent" : "rgba(107,114,128,0.2)"}`,
                  color: retrying ? "#374151" : "#6b7280",
                  fontWeight: 600, fontSize: 12, letterSpacing: "0.04em",
                }}
              >
                {retrying ? "Đang khởi động…" : "↺ Retry với Input gốc từ Pipeline"}
              </button>
              <div style={{ fontSize: 9, color: "#374151", marginTop: 6, textAlign: "center" }}>
                Pipeline tự build input từ output của các stage trước (bỏ qua thay đổi thủ công)
              </div>
            </div>
          )}

        </div>
      </div>
    </>
  );
}

function TimeCell({ label, value, accent }: { label: string; value: string; accent?: string }) {
  return (
    <div>
      <div style={{ fontSize: 9, color: "#374151", letterSpacing: "0.1em", marginBottom: 2 }}>{label.toUpperCase()}</div>
      <div style={{ fontSize: 11, fontFamily: "monospace", color: accent || "#6b7280" }}>{value}</div>
    </div>
  );
}

function EmptyState({ label, sub }: { label: string; sub?: string }) {
  return (
    <div style={{ textAlign: "center", padding: "48px 24px", color: "#374151" }}>
      <div style={{ fontSize: 32, marginBottom: 12, opacity: 0.4 }}>—</div>
      <p style={{ fontSize: 13, marginBottom: 4 }}>{label}</p>
      {sub && <p style={{ fontSize: 11, color: "#1f2937" }}>{sub}</p>}
    </div>
  );
}

// ─── Stage Row (vertical timeline item) ──────────────────────────────────────
function StageRow({
  projectId, stageKey, liveStatus, dbStage, index, isLast, onOutputUpdate, onStageRetried,
}: {
  projectId: string; stageKey: string; liveStatus: StageStatus;
  dbStage?: StageInfo; index: number; isLast: boolean;
  onOutputUpdate: (stage: string, data: Record<string, unknown>) => void;
  onStageRetried: () => void;
}) {
  const meta = STAGE_META[stageKey] || { label: stageKey, icon: "⚙", color: "#6b7280", desc: "" };
  const status = liveStatus !== "pending" ? liveStatus : (dbStage?.status as StageStatus) || "pending";
  const [showDetail, setShowDetail] = useState(false);
  const [outputOverride, setOutputOverride] = useState<Record<string, unknown> | null>(null);

  const output = outputOverride ?? (dbStage?.output as Record<string, unknown> | null ?? null);

  const statusColors: Record<StageStatus, { line: string; dot: string; bg: string; text: string }> = {
    running:   { line: meta.color, dot: meta.color, bg: `${meta.color}10`, text: meta.color },
    completed: { line: meta.color, dot: meta.color, bg: `${meta.color}08`, text: meta.color },
    failed:    { line: "#f87171", dot: "#f87171", bg: "rgba(239,68,68,0.06)", text: "#f87171" },
    pending:   { line: "rgba(255,255,255,0.06)", dot: "#1f2937", bg: "transparent", text: "#374151" },
  };

  const sc = statusColors[status];

  return (
    <div style={{ display: "flex", gap: 0, position: "relative" }}>
      {/* Timeline column */}
      <div style={{ display: "flex", flexDirection: "column", alignItems: "center", width: 48, flexShrink: 0 }}>
        {/* Dot */}
        <div style={{
          width: 28, height: 28, borderRadius: "50%", flexShrink: 0, zIndex: 1,
          background: status === "pending" ? "#12151e" : sc.bg,
          border: `2px solid ${sc.dot}`,
          display: "flex", alignItems: "center", justifyContent: "center",
          fontSize: 13, color: sc.dot,
          boxShadow: status === "running" ? `0 0 12px ${meta.color}66` : "none",
          transition: "all 0.3s",
          position: "relative",
        }}>
          {status === "running"
            ? <div style={{
                width: 8, height: 8, borderRadius: "50%", background: meta.color,
                animation: "pulse 1.4s infinite",
              }} />
            : status === "completed"
            ? <span style={{ fontSize: 11 }}>✓</span>
            : status === "failed"
            ? <span style={{ fontSize: 11 }}>✗</span>
            : <span style={{ fontSize: 11, opacity: 0.4 }}>{index + 1}</span>
          }
        </div>
        {/* Connector line */}
        {!isLast && (
          <div style={{
            width: 2, flex: 1, minHeight: 24, marginTop: 2, marginBottom: 2,
            background: status === "completed"
              ? `linear-gradient(${meta.color}, ${STAGE_META[STAGE_ORDER[index + 1]]?.color || "#6b7280"}88)`
              : "rgba(255,255,255,0.06)",
            borderRadius: 1, transition: "background 0.5s",
          }} />
        )}
      </div>

      {/* Card */}
      <div style={{ flex: 1, marginBottom: isLast ? 0 : 12, paddingBottom: isLast ? 0 : 4 }}>
        <div style={{
          borderRadius: 12, border: `1px solid ${status === "pending" ? "rgba(255,255,255,0.06)" : sc.dot + "30"}`,
          background: sc.bg, padding: "14px 16px",
          transition: "all 0.3s",
          marginLeft: 8,
        }}>
          {/* Header */}
          <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
            <span style={{ fontSize: 15, color: sc.dot, fontFamily: "monospace" }}>{meta.icon}</span>
            <div style={{ flex: 1 }}>
              <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                <span style={{ fontWeight: 600, fontSize: 13, color: status === "pending" ? "#374151" : "#e5e7eb" }}>
                  {meta.label}
                </span>
                <StatusBadge status={status} color={sc.text} />
              </div>
              {meta.desc && status === "pending" && (
                <div style={{ fontSize: 10, color: "#374151", marginTop: 2 }}>{meta.desc}</div>
              )}
            </div>
            {/* Timing */}
            {dbStage?.startedAt && (
              <div style={{ fontSize: 9, color: "#374151", fontFamily: "monospace", textAlign: "right" }}>
                <div>{fmt(dbStage.startedAt)}</div>
                {dbStage.finishedAt && (
                  <div style={{ color: sc.dot, marginTop: 1 }}>
                    {dur(dbStage.startedAt, dbStage.finishedAt)}
                  </div>
                )}
              </div>
            )}
          </div>

          {/* Error */}
          {dbStage?.error && (
            <div style={{
              marginTop: 10, padding: "8px 10px", borderRadius: 7,
              background: "rgba(239,68,68,0.08)", border: "1px solid rgba(239,68,68,0.2)",
              fontSize: 11, color: "#f87171", fontFamily: "monospace",
            }}>
              {dbStage.error}
            </div>
          )}

          {/* Output preview (compact) */}
          {status === "completed" && output && (
            <div style={{ marginTop: 10 }}>
              <OutputPreview data={output} color={meta.color} />
            </div>
          )}

          {/* Actions */}
          {dbStage && (status === "completed" || status === "failed" || status === "running") && (
            <div style={{ display: "flex", gap: 6, marginTop: 12 }}>
              <ActionBtn
                onClick={() => setShowDetail(true)}
                color={meta.color} icon="⊙"
                label={status === "failed" ? "Details / Retry" : "Details"}
              />
            </div>
          )}
        </div>
      </div>

      {/* Detail panel with retry */}
      {showDetail && dbStage && (
        <StageDetailPanel
          stageKey={stageKey} dbStage={dbStage} meta={meta}
          projectId={projectId}
          onClose={() => setShowDetail(false)}
          onRetried={() => { onStageRetried(); onOutputUpdate(stageKey, {}); }}
        />
      )}
    </div>
  );
}

function StatusBadge({ status, color }: { status: StageStatus; color: string }) {
  const labels: Record<StageStatus, string> = {
    running: "RUNNING", completed: "DONE", failed: "FAILED", pending: "PENDING",
  };
  return (
    <span style={{
      fontSize: 9, padding: "2px 8px", borderRadius: 4,
      background: `${color}15`, color, letterSpacing: "0.08em", fontWeight: 700,
    }}>
      {labels[status]}
    </span>
  );
}

function ActionBtn({ onClick, color, icon, label }: {
  onClick: () => void; color: string; icon: string; label: string;
}) {
  return (
    <button onClick={onClick} style={{
      fontSize: 10, padding: "4px 10px", borderRadius: 6, cursor: "pointer",
      background: `${color}10`, border: `1px solid ${color}25`,
      color, letterSpacing: "0.05em", fontWeight: 600,
      display: "flex", alignItems: "center", gap: 4,
    }}>
      <span>{icon}</span>{label}
    </button>
  );
}

// Compact output preview — shows counts/thumbnails without full render
function OutputPreview({ data, color }: { data: Record<string, unknown>; color: string }) {
  const results = data.results as Record<string, unknown>[] | undefined;
  const panels = data.panels as Record<string, unknown>[] | undefined;
  const keys = Object.keys(data);

  if (Array.isArray(results) && results.some(r => r.imageUrl)) {
    const imgs = results.filter(r => r.imageUrl);
    return (
      <div style={{ display: "flex", gap: 6, alignItems: "center" }}>
        {imgs.slice(0, 5).map((r, i) => (
          <img key={i} src={r.imageUrl as string} alt=""
            style={{ width: 36, height: 36, objectFit: "cover", borderRadius: 4, border: `1px solid ${color}30` }}
            onError={e => { (e.target as HTMLImageElement).style.display = "none"; }} />
        ))}
        {imgs.length > 5 && (
          <span style={{ fontSize: 10, color: "#6b7280" }}>+{imgs.length - 5} more</span>
        )}
      </div>
    );
  }

  if (Array.isArray(panels) && panels.length > 0) {
    return (
      <div style={{ fontSize: 10, color }}>
        ▣ {panels.length} panels generated
      </div>
    );
  }

  return (
    <div style={{ fontSize: 10, color: "#4b5563" }}>
      {keys.slice(0, 3).join(", ")}{keys.length > 3 ? `… +${keys.length - 3}` : ""}
    </div>
  );
}

// ─── Summary bar ──────────────────────────────────────────────────────────────
function SummaryBar({ liveStatuses, dbStages, connected, events }: {
  liveStatuses: Record<string, StageStatus>;
  dbStages: Record<string, StageInfo>;
  connected: boolean;
  events: PipelineEvent[];
}) {
  const done = STAGE_ORDER.filter(s => liveStatuses[s] === "completed" || dbStages[s]?.status === "completed").length;
  const running = STAGE_ORDER.filter(s => liveStatuses[s] === "running").length;
  const failed = STAGE_ORDER.filter(s => liveStatuses[s] === "failed" || dbStages[s]?.status === "failed").length;
  const firstStart = STAGE_ORDER.map(s => dbStages[s]?.startedAt).filter(Boolean)[0];
  const lastEnd = [...STAGE_ORDER].reverse().map(s => dbStages[s]?.finishedAt).filter(Boolean)[0];

  return (
    <div style={{
      display: "grid", gridTemplateColumns: "repeat(5, 1fr)", gap: 10, marginBottom: 28,
    }}>
      {[
        { label: "Completed", value: `${done}/${STAGE_ORDER.length}`, color: "#34d399" },
        { label: "Running", value: running, color: running > 0 ? "#a78bfa" : "#1f2937" },
        { label: "Failed", value: failed, color: failed > 0 ? "#f87171" : "#1f2937" },
        { label: "Duration", value: dur(firstStart, lastEnd) || "—", color: "#f59e0b" },
        { label: "Events", value: events.length, color: connected ? "#34d399" : "#374151" },
      ].map(item => (
        <div key={item.label} style={{
          background: "#12151e", border: "1px solid rgba(255,255,255,0.05)",
          borderRadius: 10, padding: "10px 14px",
        }}>
          <div style={{ fontSize: 9, color: "#374151", letterSpacing: "0.1em", marginBottom: 6 }}>
            {item.label.toUpperCase()}
          </div>
          <div style={{ fontSize: 18, fontWeight: 700, color: item.color, fontFamily: "monospace" }}>
            {item.value}
          </div>
        </div>
      ))}
    </div>
  );
}

// ─── Live Log ─────────────────────────────────────────────────────────────────
function LiveLog({ events }: { events: PipelineEvent[] }) {
  const logRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    logRef.current?.scrollTo(0, logRef.current.scrollHeight);
  }, [events]);

  if (events.length === 0) return null;
  return (
    <div style={{ marginTop: 32 }}>
      <div style={{
        fontSize: 10, fontWeight: 700, letterSpacing: "0.12em", color: "#374151", marginBottom: 10,
        display: "flex", alignItems: "center", gap: 8,
      }}>
        <div style={{ width: 5, height: 5, borderRadius: "50%", background: "#34d399", animation: "pulse 2s infinite" }} />
        LIVE LOG · {events.length} events
      </div>
      <div ref={logRef} style={{
        maxHeight: 200, overflowY: "auto", borderRadius: 10,
        border: "1px solid rgba(255,255,255,0.05)",
        background: "rgba(0,0,0,0.3)", padding: "10px 14px",
        fontFamily: "monospace", fontSize: 11,
      }}>
        {events.map((event, i) => (
          <div key={i} style={{ display: "flex", gap: 10, marginBottom: 4, alignItems: "baseline" }}>
            <span style={{ color: "#374151", minWidth: 64, fontSize: 9 }}>
              {new Date(event.timestamp).toLocaleTimeString()}
            </span>
            <span style={{
              fontSize: 9, padding: "1px 6px", borderRadius: 3, letterSpacing: "0.06em",
              background: event.status === "started"   ? "rgba(245,158,11,0.12)" :
                           event.status === "completed" ? "rgba(52,211,153,0.12)" : "rgba(239,68,68,0.12)",
              color: event.status === "started"   ? "#f59e0b" :
                     event.status === "completed" ? "#34d399" : "#f87171",
            }}>
              {event.status.toUpperCase()}
            </span>
            <span style={{ color: "#6b7280", fontSize: 11 }}>{event.message}</span>
          </div>
        ))}
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
  const [dbStages, setDbStages] = useState<Record<string, StageInfo>>({});

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
        setEvents(prev => [...prev, event]);
        setLiveStatuses(prev => ({
          ...prev,
          [event.stage]: event.status === "started" ? "running" :
                         event.status === "completed" ? "completed" : "failed",
        }));
        if (event.status === "completed" || event.status === "failed") loadStages();
      },
      () => setConnected(false)
    );
    return () => source.close();
  }, [projectId, loadStages]);

  const handleOutputUpdate = useCallback((stage: string, data: Record<string, unknown>) => {
    setDbStages(prev => ({ ...prev, [stage]: { ...prev[stage], output: data as StageInfo["output"] } }));
  }, []);

  const completedCount = STAGE_ORDER.filter(
    s => liveStatuses[s] === "completed" || dbStages[s]?.status === "completed"
  ).length;
  const progress = (completedCount / STAGE_ORDER.length) * 100;
  const hasAnyData = STAGE_ORDER.some(s => dbStages[s]);
  const shortId = projectId.slice(0, 8).toUpperCase();

  return (
    <>
      <style>{`
        @keyframes pulse { 0%,100%{opacity:1} 50%{opacity:0.3} }
        @keyframes slide-in-right {
          from { transform: translateX(100%); opacity: 0; }
          to   { transform: translateX(0);    opacity: 1; }
        }
        * { box-sizing: border-box; }
      `}</style>

      <div style={{
        padding: "28px 32px", maxWidth: 860, margin: "0 auto",
        fontFamily: "'Plus Jakarta Sans', sans-serif", color: "#e5e7eb",
        minHeight: "100vh",
      }}>

        {/* Header */}
        <div style={{ marginBottom: 28 }}>
          <Link href="/projects" style={{
            color: "#4b5563", fontSize: 11, textDecoration: "none",
            letterSpacing: "0.08em", display: "inline-flex", alignItems: "center", gap: 6,
            marginBottom: 16, padding: "4px 10px", borderRadius: 6,
            border: "1px solid rgba(255,255,255,0.06)", background: "rgba(255,255,255,0.02)",
          }}>
            ← PROJECTS
          </Link>

          <div style={{ display: "flex", alignItems: "flex-start", justifyContent: "space-between" }}>
            <div>
              <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 6 }}>
                <h1 style={{ fontSize: 26, fontWeight: 800, letterSpacing: "-0.03em", color: "#f9fafb", margin: 0 }}>
                  Pipeline Monitor
                </h1>
                <div style={{
                  display: "flex", alignItems: "center", gap: 6,
                  background: "rgba(255,255,255,0.04)", border: "1px solid rgba(255,255,255,0.08)",
                  borderRadius: 6, padding: "3px 8px",
                }}>
                  <span style={{ fontSize: 9, color: "#f5b240", letterSpacing: "0.12em", fontWeight: 700 }}>FILM</span>
                  <span style={{ fontSize: 10, color: "#6b7280", fontFamily: "monospace" }}>{shortId}</span>
                </div>
              </div>
              <p style={{ fontSize: 10, color: "#374151", fontFamily: "monospace", letterSpacing: "0.04em", margin: 0 }}>
                {projectId}
              </p>
            </div>

            {/* Live indicator */}
            <div style={{
              display: "flex", alignItems: "center", gap: 8,
              padding: "8px 14px", borderRadius: 8,
              background: connected ? "rgba(52,211,153,0.08)" : "rgba(255,255,255,0.03)",
              border: `1px solid ${connected ? "rgba(52,211,153,0.2)" : "rgba(255,255,255,0.06)"}`,
            }}>
              <div style={{
                width: 7, height: 7, borderRadius: "50%",
                background: connected ? "#34d399" : "#374151",
                boxShadow: connected ? "0 0 8px #34d399" : "none",
                animation: connected ? "pulse 2s infinite" : "none",
              }} />
              <span style={{ fontSize: 11, color: connected ? "#34d399" : "#374151", fontWeight: 600, letterSpacing: "0.06em" }}>
                {connected ? "LIVE" : hasAnyData ? "HISTORICAL" : "AWAITING"}
              </span>
            </div>
          </div>
        </div>

        {/* Progress bar */}
        <div style={{ marginBottom: 28 }}>
          <div style={{ display: "flex", justifyContent: "space-between", marginBottom: 8 }}>
            <span style={{ fontSize: 10, color: "#374151", letterSpacing: "0.1em" }}>PIPELINE PROGRESS</span>
            <span style={{ fontSize: 11, fontFamily: "monospace", color: "#f59e0b" }}>
              {completedCount} / {STAGE_ORDER.length} stages
            </span>
          </div>
          <div style={{ height: 4, background: "rgba(255,255,255,0.05)", borderRadius: 4, overflow: "hidden", position: "relative" }}>
            <div style={{
              height: "100%",
              background: progress === 100
                ? "linear-gradient(90deg, #34d399, #059669)"
                : "linear-gradient(90deg, #f59e0b, #f472b6, #a78bfa)",
              borderRadius: 4, width: `${progress}%`,
              transition: "width 0.6s ease",
              boxShadow: `0 0 12px rgba(245,158,11,0.4)`,
            }} />
          </div>
        </div>

        {/* Summary stats */}
        <SummaryBar
          liveStatuses={liveStatuses}
          dbStages={dbStages}
          connected={connected}
          events={events}
        />

        {/* Stage timeline */}
        <div>
          <div style={{
            fontSize: 10, fontWeight: 700, letterSpacing: "0.12em",
            color: "#374151", marginBottom: 16,
          }}>
            STAGE TIMELINE
          </div>
          {STAGE_ORDER.map((key, i) => (
            <StageRow
              key={key}
              projectId={projectId}
              stageKey={key}
              liveStatus={liveStatuses[key]}
              dbStage={dbStages[key]}
              index={i}
              isLast={i === STAGE_ORDER.length - 1}
              onOutputUpdate={handleOutputUpdate}
              onStageRetried={loadStages}
            />
          ))}
        </div>

        {/* Live log */}
        <LiveLog events={events} />
      </div>
    </>
  );
}
