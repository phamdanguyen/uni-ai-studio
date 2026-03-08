"use client";
import { useEffect, useState, useRef } from "react";
import { useParams } from "next/navigation";
import { connectPipelineSSE, type PipelineEvent } from "@/lib/sse";

const STAGES = [
  { key: "analysis", label: "Story Analysis", icon: "📖" },
  { key: "planning", label: "Pipeline Planning", icon: "📋" },
  { key: "characters", label: "Character Design", icon: "👤" },
  { key: "locations", label: "Location Design", icon: "🏔" },
  { key: "storyboard", label: "Storyboard", icon: "🎞" },
  { key: "media_gen", label: "Media Generation", icon: "🖼" },
  { key: "quality_check", label: "Quality Check", icon: "✅" },
  { key: "voice", label: "Voice & TTS", icon: "🎤" },
  { key: "assembly", label: "Final Assembly", icon: "🎬" },
];

type StageStatus = "pending" | "running" | "completed" | "failed";

export default function ProjectPage() {
  const params = useParams();
  const projectId = params.id as string;

  const [stageStatuses, setStageStatuses] = useState<Record<string, StageStatus>>(
    Object.fromEntries(STAGES.map((s) => [s.key, "pending" as StageStatus]))
  );
  const [events, setEvents] = useState<PipelineEvent[]>([]);
  const [connected, setConnected] = useState(false);
  const logRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const source = connectPipelineSSE(
      projectId,
      (event) => {
        setConnected(true);
        setEvents((prev) => [...prev, event]);

        setStageStatuses((prev) => ({
          ...prev,
          [event.stage]: event.status === "started" ? "running" :
                         event.status === "completed" ? "completed" : "failed",
        }));
      },
      () => setConnected(false)
    );

    return () => source.close();
  }, [projectId]);

  useEffect(() => {
    logRef.current?.scrollTo(0, logRef.current.scrollHeight);
  }, [events]);

  const completedCount = Object.values(stageStatuses).filter((s) => s === "completed").length;
  const progress = (completedCount / STAGES.length) * 100;

  return (
    <div className="max-w-7xl mx-auto px-6 py-12">
      {/* Header */}
      <div className="flex items-center justify-between mb-8">
        <div>
          <h1 className="text-3xl font-bold">🎬 Pipeline Monitor</h1>
          <p className="text-zinc-400 font-mono">{projectId}</p>
        </div>
        <div className="flex items-center gap-2">
          <span className={`w-2 h-2 rounded-full ${connected ? "bg-emerald-400 animate-pulse" : "bg-zinc-600"}`} />
          <span className="text-sm text-zinc-400">{connected ? "Connected" : "Disconnected"}</span>
        </div>
      </div>

      {/* Progress Bar */}
      <div className="mb-10">
        <div className="flex items-center justify-between mb-2">
          <span className="text-sm text-zinc-400">Overall Progress</span>
          <span className="text-sm font-mono text-violet-400">{completedCount}/{STAGES.length}</span>
        </div>
        <div className="h-2 bg-zinc-800 rounded-full overflow-hidden">
          <div className="h-full bg-gradient-to-r from-violet-500 to-fuchsia-500 rounded-full transition-all duration-500" style={{ width: `${progress}%` }} />
        </div>
      </div>

      {/* Stages Grid */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4 mb-10">
        {STAGES.map((stage, i) => {
          const status = stageStatuses[stage.key];
          return (
            <div key={stage.key} className={`rounded-xl border p-5 transition ${
              status === "running" ? "border-violet-500/50 bg-violet-500/5 ring-1 ring-violet-500/20" :
              status === "completed" ? "border-emerald-500/30 bg-emerald-500/5" :
              status === "failed" ? "border-red-500/30 bg-red-500/5" :
              "border-zinc-800 bg-zinc-900/50"
            }`}>
              <div className="flex items-center gap-3 mb-2">
                <span className="text-lg">{stage.icon}</span>
                <span className="text-sm font-medium">{stage.label}</span>
                <span className="ml-auto text-xs text-zinc-500">#{i + 1}</span>
              </div>
              <div className="flex items-center gap-2">
                {status === "running" && <span className="w-1.5 h-1.5 rounded-full bg-violet-400 animate-pulse" />}
                {status === "completed" && <span className="text-emerald-400 text-xs">✓</span>}
                {status === "failed" && <span className="text-red-400 text-xs">✗</span>}
                <span className={`text-xs capitalize ${
                  status === "running" ? "text-violet-400" :
                  status === "completed" ? "text-emerald-400" :
                  status === "failed" ? "text-red-400" : "text-zinc-500"
                }`}>{status}</span>
              </div>
            </div>
          );
        })}
      </div>

      {/* Event Log */}
      <section>
        <h2 className="text-lg font-semibold mb-4 flex items-center gap-2">
          <span className="w-2 h-2 rounded-full bg-violet-500" /> Event Log
        </h2>
        <div ref={logRef} className="h-80 overflow-y-auto rounded-xl border border-zinc-800 bg-zinc-900/50 p-4 font-mono text-sm space-y-2">
          {events.length === 0 && (
            <div className="text-zinc-500 text-center py-10">Waiting for pipeline events...</div>
          )}
          {events.map((event, i) => (
            <div key={i} className="flex items-start gap-3">
              <span className="text-xs text-zinc-500 shrink-0 w-20">
                {new Date(event.timestamp).toLocaleTimeString()}
              </span>
              <span className={`text-xs px-1.5 py-0.5 rounded shrink-0 ${
                event.status === "started" ? "bg-violet-500/20 text-violet-400" :
                event.status === "completed" ? "bg-emerald-500/20 text-emerald-400" :
                "bg-red-500/20 text-red-400"
              }`}>{event.status}</span>
              <span className="text-zinc-300">{event.message}</span>
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}
