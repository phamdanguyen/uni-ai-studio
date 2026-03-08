"use client";
import { useEffect, useState } from "react";
import { api, type LLMSettings } from "@/lib/api";

export default function SettingsPage() {
  const [settings, setSettings] = useState<LLMSettings | null>(null);
  const [form, setForm] = useState<LLMSettings | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    api.settings.getLLM()
      .then((data) => { setSettings(data); setForm(data); })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  }, []);

  const handleSave = async () => {
    if (!form) return;
    setSaving(true); setError(""); setSaved(false);
    try {
      await api.settings.updateLLM(form);
      setSettings(form);
      setSaved(true);
      setTimeout(() => setSaved(false), 3000);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to save");
    } finally { setSaving(false); }
  };

  const hasChanges = JSON.stringify(settings) !== JSON.stringify(form);

  if (loading) {
    return (
      <div style={{ padding: 32, display: "flex", alignItems: "center", gap: 12, color: "#8b8fa3" }}>
        <div style={{
          width: 16, height: 16, borderRadius: "50%",
          border: "2px solid #f5b240", borderTopColor: "transparent",
          animation: "spin-slow 1s linear infinite",
        }} />
        Loading settings...
      </div>
    );
  }

  return (
    <div style={{ padding: 32, maxWidth: 800 }}>
      <div className="anim-in" style={{ marginBottom: 32 }}>
        <h1 style={{ fontSize: 24, fontWeight: 700, marginBottom: 4, letterSpacing: "-0.02em" }}>
          <span style={{ color: "#f5b240" }}>⚙</span> Settings
        </h1>
        <p style={{ fontSize: 14, color: "#8b8fa3" }}>
          Cấu hình LLM providers, model routing, và budget
        </p>
      </div>

      {error && (
        <div className="anim-in" style={{
          marginBottom: 24, padding: 12, borderRadius: 12, fontSize: 13,
          background: "rgba(239,68,68,0.08)", border: "1px solid rgba(239,68,68,0.15)", color: "#ef4444",
        }}>
          {error}
        </div>
      )}

      {saved && (
        <div className="anim-in" style={{
          marginBottom: 24, padding: 12, borderRadius: 12, fontSize: 13,
          background: "rgba(61,214,140,0.08)", border: "1px solid rgba(61,214,140,0.15)", color: "#3dd68c",
        }}>
          ✓ Settings saved successfully
        </div>
      )}

      {form && (
        <div style={{ display: "flex", flexDirection: "column", gap: 32 }}>
          {/* LLM Provider Keys */}
          <Section title="🔑 LLM Provider">
            <FieldGroup label="OpenRouter API Key">
              <input type="password" value={form.openRouterApiKey}
                onChange={(e) => setForm({ ...form, openRouterApiKey: e.target.value })}
                className="input-field" style={{ fontFamily: "'JetBrains Mono', monospace", fontSize: 13 }}
                placeholder="sk-or-v1-..." />
            </FieldGroup>
            <FieldGroup label="OpenRouter Base URL">
              <input type="text" value={form.openRouterBaseUrl}
                onChange={(e) => setForm({ ...form, openRouterBaseUrl: e.target.value })}
                className="input-field" style={{ fontFamily: "'JetBrains Mono', monospace", fontSize: 13 }} />
            </FieldGroup>
            <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 12 }}>
              <FieldGroup label="Google AI Key">
                <input type="password" value={form.googleAiKey}
                  onChange={(e) => setForm({ ...form, googleAiKey: e.target.value })}
                  className="input-field" style={{ fontFamily: "'JetBrains Mono', monospace", fontSize: 13 }}
                  placeholder="AIza..." />
              </FieldGroup>
              <FieldGroup label="Anthropic Key">
                <input type="password" value={form.anthropicKey}
                  onChange={(e) => setForm({ ...form, anthropicKey: e.target.value })}
                  className="input-field" style={{ fontFamily: "'JetBrains Mono', monospace", fontSize: 13 }}
                  placeholder="sk-ant-..." />
              </FieldGroup>
            </div>
          </Section>

          {/* Model Routing */}
          <Section title="⬡ Model Routing">
            <p style={{ fontSize: 12, color: "#5c6079", marginBottom: 16 }}>
              Chọn model cho mỗi tier. Format: <code style={{ color: "#f5b240", fontFamily: "'JetBrains Mono', monospace" }}>provider/model-name</code>
            </p>
            <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
              <ModelRow tier="Flash" emoji="⚡" desc="Nhanh, routing & parsing" value={form.flashModel}
                onChange={(v) => setForm({ ...form, flashModel: v })} />
              <ModelRow tier="Standard" emoji="✨" desc="Cân bằng, creative tasks" value={form.standardModel}
                onChange={(v) => setForm({ ...form, standardModel: v })} />
              <ModelRow tier="Premium" emoji="💎" desc="Cao cấp, complex reasoning" value={form.premiumModel}
                onChange={(v) => setForm({ ...form, premiumModel: v })} />
            </div>
          </Section>

          {/* Budget & Performance */}
          <Section title="📊 Budget & Performance">
            <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 12 }}>
              <FieldGroup label="Default Budget (USD)">
                <input type="number" step="0.5" min="0" value={form.defaultBudgetUsd}
                  onChange={(e) => setForm({ ...form, defaultBudgetUsd: parseFloat(e.target.value) || 0 })}
                  className="input-field" style={{ fontFamily: "'JetBrains Mono', monospace" }} />
              </FieldGroup>
              <FieldGroup label="Request Timeout (seconds)">
                <input type="number" min="10" max="600" value={form.requestTimeoutS}
                  onChange={(e) => setForm({ ...form, requestTimeoutS: parseInt(e.target.value) || 120 })}
                  className="input-field" style={{ fontFamily: "'JetBrains Mono', monospace" }} />
              </FieldGroup>
            </div>
          </Section>

          {/* Save */}
          <div style={{ display: "flex", alignItems: "center", gap: 12, paddingTop: 8 }}>
            <button onClick={handleSave} disabled={saving || !hasChanges} className="btn-primary">
              {saving ? "Saving..." : "💾 Save Changes"}
            </button>
            {hasChanges && <span style={{ fontSize: 12, color: "#f59e0b" }}>Unsaved changes</span>}
            <div style={{ flex: 1 }} />
            <span style={{ fontSize: 11, color: "#5c6079" }}>
              ⓘ Thay đổi chỉ áp dụng runtime, restart server để persist
            </span>
          </div>
        </div>
      )}
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="glass-card" style={{ padding: 24, cursor: "default" }}>
      <h2 className="section-header">{title}</h2>
      <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>{children}</div>
    </div>
  );
}

function FieldGroup({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <label style={{ display: "block", fontSize: 12, fontWeight: 500, marginBottom: 6, color: "#5c6079" }}>{label}</label>
      {children}
    </div>
  );
}

function ModelRow({ tier, emoji, desc, value, onChange }: {
  tier: string; emoji: string; desc: string; value: string; onChange: (v: string) => void;
}) {
  return (
    <div style={{
      display: "flex", alignItems: "center", gap: 16, padding: 12, borderRadius: 12, background: "#181c28",
    }}>
      <div style={{ flexShrink: 0, width: 80 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
          <span>{emoji}</span>
          <span style={{ fontSize: 14, fontWeight: 600 }}>{tier}</span>
        </div>
        <div style={{ fontSize: 10, color: "#5c6079", marginTop: 2 }}>{desc}</div>
      </div>
      <input type="text" value={value} onChange={(e) => onChange(e.target.value)}
        className="input-field" style={{ flex: 1, fontFamily: "'JetBrains Mono', monospace", fontSize: 13 }} />
    </div>
  );
}
