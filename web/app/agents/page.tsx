"use client";
import { useEffect, useState } from "react";
import { api, type AgentCard } from "@/lib/api";

export default function AgentsPage() {
  const [agents, setAgents] = useState<AgentCard[]>([]);

  useEffect(() => {
    api.agents.list().then((data) => setAgents(data.agents || []));
  }, []);

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
          </div>
        ))}
      </div>
    </div>
  );
}

function agentIcon(name: string): string {
  const icons: Record<string, string> = {
    director: "🎬", character: "👤", location: "🏔",
    storyboard: "🎞", media: "🖼", voice: "🎤",
  };
  return icons[name] || "🤖";
}
