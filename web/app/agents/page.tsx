"use client";
import { useEffect, useState } from "react";
import { api, type AgentCard, type AgentModelsConfig, type AgentModelConfig } from "@/lib/api";
import { agentIcon } from "@/lib/utils";

export default function AgentsPage() {
  const [agents, setAgents] = useState<AgentCard[]>([]);
  const [agentModels, setAgentModels] = useState<AgentModelsConfig>({ agents: {} });
  const [expandedAgent, setExpandedAgent] = useState<string | null>(null);
  const [savingAgent, setSavingAgent] = useState<string | null>(null);
  const [savedAgent, setSavedAgent] = useState<string | null>(null);

  useEffect(() => {
    api.agents.list().then((data) => setAgents(data.agents || []));
    api.agentModels.get().then((data) => setAgentModels(data)).catch(() => {
      // Backend chua co endpoint nay, dung state rong
    });
  }, []);

  const updateAgentTier = (agentName: string, tier: "flash" | "standard" | "premium", value: string) => {
    setAgentModels(prev => ({
      agents: {
        ...prev.agents,
        [agentName]: { ...(prev.agents[agentName] || { flash: "", standard: "", premium: "" }), [tier]: value }
      }
    }));
  };

  const saveAgentModel = async (agentName: string) => {
    setSavingAgent(agentName);
    try {
      const config = agentModels.agents[agentName] || { flash: "", standard: "", premium: "" };
      const filtered = Object.fromEntries(
        Object.entries(config).filter(([, v]) => v.trim() !== "")
      ) as AgentModelConfig;
      const update: AgentModelsConfig = {
        agents: { ...agentModels.agents, [agentName]: filtered }
      };
      await api.agentModels.update(update);
      setSavedAgent(agentName);
      setTimeout(() => setSavedAgent(null), 2000);
    } catch (err) {
      console.error("Failed to save agent model config:", err);
    } finally {
      setSavingAgent(null);
    }
  };

  return (
    <div style={{ padding: 32, maxWidth: 1200 }}>
      <div className="anim-in" style={{ marginBottom: 32 }}>
        <h1 style={{ fontSize: 24, fontWeight: 700, marginBottom: 4, letterSpacing: "-0.02em" }}>
          <span style={{ color: "#f5b240" }}>⬡</span> Agents
        </h1>
        <p style={{ fontSize: 14, color: "#8b8fa3" }}>
          6 specialized AI agents powering the filmmaking pipeline
        </p>
      </div>
      <div style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 16 }}>
        {agents.map((agent) => (
          <div key={agent.name} className="glass-card" style={{ overflow: "hidden" }}>
            <div style={{ padding: 20 }}>
              <div style={{ display: "flex", alignItems: "center", gap: 12, marginBottom: 16 }}>
                <div style={{
                  width: 40, height: 40, borderRadius: 10, display: "flex", alignItems: "center", justifyContent: "center",
                  fontSize: 18, background: "rgba(245,178,64,0.1)", border: "1px solid rgba(245,178,64,0.2)",
                }}>
                  {agentIcon(agent.name)}
                </div>
                <div>
                  <h2 style={{ fontWeight: 600, fontSize: 14, textTransform: "capitalize" }}>{agent.name}</h2>
                  <span style={{ fontSize: 11, color: "#5c6079", fontFamily: "'JetBrains Mono', monospace" }}>
                    v{agent.version}
                  </span>
                </div>
              </div>
              <p style={{ fontSize: 13, color: "#8b8fa3", marginBottom: 16, display: "-webkit-box", WebkitLineClamp: 2, WebkitBoxOrient: "vertical", overflow: "hidden" }}>
                {agent.description}
              </p>
              <div>
                <div style={{ fontSize: 10, textTransform: "uppercase", letterSpacing: "0.08em", fontWeight: 600, color: "#5c6079", marginBottom: 8 }}>
                  Skills ({agent.skills?.length || 0})
                </div>
                {agent.skills?.map((skill) => (
                  <div key={skill.id} style={{
                    display: "flex", alignItems: "flex-start", gap: 8, padding: 8, borderRadius: 8,
                    background: "#181c28", marginBottom: 4,
                  }}>
                    <span style={{ color: "#f5b240", fontSize: 12, marginTop: 2 }}>▸</span>
                    <div>
                      <div style={{ fontSize: 12, fontFamily: "'JetBrains Mono', monospace" }}>{skill.id}</div>
                      {skill.description && (
                        <div style={{ fontSize: 11, color: "#5c6079", marginTop: 2 }}>{skill.description}</div>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            </div>
            <div style={{
              padding: "12px 20px", borderTop: "1px solid rgba(255,255,255,0.06)",
              display: "flex", gap: 16, fontSize: 11, color: "#5c6079",
            }}>
              {agent.capabilities?.streaming && (
                <span style={{ display: "flex", alignItems: "center", gap: 4 }}>
                  <span style={{ color: "#f5b240" }}>⚡</span> Streaming
                </span>
              )}
              {agent.capabilities?.stateTransitionHistory && (
                <span style={{ display: "flex", alignItems: "center", gap: 4 }}>
                  <span style={{ color: "#f5b240" }}>◆</span> History
                </span>
              )}
            </div>

            {/* Model Override Section */}
            <div style={{ padding: "0 20px 16px" }}>
              <button
                onClick={() => setExpandedAgent(expandedAgent === agent.name ? null : agent.name)}
                style={{
                  fontSize: 11, color: "#5c6079", background: "none",
                  border: "1px solid rgba(255,255,255,0.08)", borderRadius: 6,
                  padding: "4px 10px", cursor: "pointer", width: "100%",
                  transition: "color 0.2s, border-color 0.2s",
                }}
              >
                {expandedAgent === agent.name ? "▲" : "▼"} Model Override
              </button>

              {expandedAgent === agent.name && (
                <div style={{ marginTop: 12, display: "flex", flexDirection: "column", gap: 8 }}>
                  {(["flash", "standard", "premium"] as const).map(tier => (
                    <div key={tier} style={{ display: "flex", alignItems: "center", gap: 8 }}>
                      <span style={{ fontSize: 11, width: 60, color: "#8b8fa3", textTransform: "capitalize", flexShrink: 0 }}>
                        {tier}
                      </span>
                      <input
                        type="text"
                        value={agentModels.agents[agent.name]?.[tier] ?? ""}
                        onChange={e => updateAgentTier(agent.name, tier, e.target.value)}
                        placeholder="(global default)"
                        className="input-field"
                        style={{ flex: 1, fontSize: 12, fontFamily: "'JetBrains Mono', monospace", padding: "4px 8px" }}
                      />
                    </div>
                  ))}
                  <div style={{ display: "flex", alignItems: "center", justifyContent: "flex-end", gap: 8 }}>
                    {savedAgent === agent.name && (
                      <span style={{ fontSize: 11, color: "#4ade80" }}>Saved</span>
                    )}
                    <button
                      onClick={() => saveAgentModel(agent.name)}
                      className="btn-primary"
                      disabled={savingAgent === agent.name}
                      style={{ fontSize: 12, padding: "6px 14px", opacity: savingAgent === agent.name ? 0.6 : 1, cursor: savingAgent === agent.name ? "not-allowed" : "pointer" }}
                    >
                      {savingAgent === agent.name ? "Saving..." : "Save"}
                    </button>
                  </div>
                </div>
              )}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}