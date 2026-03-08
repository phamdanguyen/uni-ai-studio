"use client";
import { useState } from "react";
import { useRouter } from "next/navigation";
import { api } from "@/lib/api";

export default function NewProjectPage() {
  const router = useRouter();
  const [form, setForm] = useState({
    projectId: "", story: "", inputType: "novel", budget: "medium", qualityLevel: "standard",
  });
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!form.story.trim()) return setError("Hãy nhập nội dung truyện");
    const projectId = form.projectId || `project-${Date.now()}`;
    setLoading(true); setError("");
    try {
      await api.pipeline.start({ ...form, projectId });
      router.push(`/project/${projectId}`);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to start pipeline");
      setLoading(false);
    }
  };

  return (
    <div style={{ padding: 32, maxWidth: 720 }}>
      <div className="anim-in" style={{ marginBottom: 32 }}>
        <h1 style={{ fontSize: 24, fontWeight: 700, marginBottom: 4, letterSpacing: "-0.02em" }}>
          <span style={{ color: "#f5b240" }}>▶</span> New Project
        </h1>
        <p style={{ fontSize: 14, color: "#8b8fa3" }}>
          AI sẽ phân tích truyện → thiết kế nhân vật → tạo storyboard → sinh hình/video → lồng tiếng
        </p>
      </div>

      <form onSubmit={handleSubmit} className="anim-in d2" style={{ display: "flex", flexDirection: "column", gap: 20 }}>
        <FieldGroup label="Project ID (optional)">
          <input type="text" placeholder="my-awesome-film" value={form.projectId}
            onChange={(e) => setForm({ ...form, projectId: e.target.value })} className="input-field" />
        </FieldGroup>

        <FieldGroup label="Story / Script / Outline *">
          <textarea rows={8} placeholder="Viết hoặc paste truyện ngắn, kịch bản, hoặc outline tại đây..."
            value={form.story} onChange={(e) => setForm({ ...form, story: e.target.value })}
            className="input-field" style={{ resize: "none" }} />
        </FieldGroup>

        <div style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 12 }}>
          <FieldGroup label="Input Type">
            <SelectField value={form.inputType} onChange={(v) => setForm({ ...form, inputType: v })}
              options={[{ value: "novel", label: "📖 Novel" }, { value: "script", label: "📝 Script" }, { value: "outline", label: "📋 Outline" }]} />
          </FieldGroup>
          <FieldGroup label="Budget">
            <SelectField value={form.budget} onChange={(v) => setForm({ ...form, budget: v })}
              options={[{ value: "low", label: "💰 Low" }, { value: "medium", label: "💰💰 Medium" }, { value: "high", label: "💰💰💰 High" }]} />
          </FieldGroup>
          <FieldGroup label="Quality">
            <SelectField value={form.qualityLevel} onChange={(v) => setForm({ ...form, qualityLevel: v })}
              options={[{ value: "draft", label: "⚡ Draft" }, { value: "standard", label: "✨ Standard" }, { value: "premium", label: "💎 Premium" }]} />
          </FieldGroup>
        </div>

        {error && (
          <div style={{
            padding: 12, borderRadius: 12, fontSize: 13,
            background: "rgba(239,68,68,0.08)", border: "1px solid rgba(239,68,68,0.15)", color: "#ef4444",
          }}>
            {error}
          </div>
        )}

        <button type="submit" disabled={loading} className="btn-primary" style={{ width: "100%", padding: "14px 20px", fontSize: 15 }}>
          {loading ? "Starting pipeline..." : "▶ Start Filmmaking Pipeline"}
        </button>
      </form>
    </div>
  );
}

function FieldGroup({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <label style={{ display: "block", fontSize: 11, fontWeight: 600, marginBottom: 6, color: "#5c6079", textTransform: "uppercase", letterSpacing: "0.06em" }}>
        {label}
      </label>
      {children}
    </div>
  );
}

function SelectField({ value, onChange, options }: {
  value: string; onChange: (v: string) => void; options: { value: string; label: string }[];
}) {
  return (
    <select value={value} onChange={(e) => onChange(e.target.value)} className="input-field" style={{ appearance: "none" }}>
      {options.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
    </select>
  );
}
