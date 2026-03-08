const API_BASE = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';

export interface AgentCard {
  name: string;
  version: string;
  description: string;
  skills: { id: string; name: string; description: string }[];
  capabilities: { streaming: boolean; stateTransitionHistory: boolean };
}

export interface AgentHealth {
  name: string;
  status: 'healthy' | 'degraded' | 'failed';
  lastHeartbeat: string;
  tasksHandled: number;
  tasksFailed: number;
  avgLatencyMs: number;
  errorRate: number;
}

export interface ToolInfo {
  name: string;
  description: string;
}

export interface HealthStatus {
  status: string;
  service: string;
  version: string;
  database: string;
  pending: number;
}

export interface PipelineRequest {
  projectId: string;
  story: string;
  inputType: string;
  budget: string;
  qualityLevel: string;
}

export interface LLMSettings {
  openRouterApiKey: string;
  openRouterBaseUrl: string;
  googleAiKey: string;
  anthropicKey: string;
  flashModel: string;
  standardModel: string;
  premiumModel: string;
  defaultBudgetUsd: number;
  requestTimeoutS: number;
}

async function fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', ...init?.headers },
  });
  if (!res.ok) throw new Error(`API error: ${res.status}`);
  return res.json();
}

export const api = {
  health: () => fetchJSON<HealthStatus>('/health'),
  
  agents: {
    list: () => fetchJSON<{ agents: AgentCard[]; count: number }>('/agents'),
    get: (name: string) => fetchJSON<AgentCard>(`/agents/${name}`),
    health: () => fetchJSON<{ health: AgentHealth[] }>('/agents/health'),
    send: (name: string, payload: Record<string, unknown>) =>
      fetchJSON(`/agents/${name}/send`, {
        method: 'POST',
        body: JSON.stringify(payload),
      }),
  },

  tools: {
    list: () => fetchJSON<{ tools: ToolInfo[]; count: number }>('/tools'),
  },

  pipeline: {
    start: (req: PipelineRequest) =>
      fetchJSON<{ status: string; projectId: string }>('/pipeline/start', {
        method: 'POST',
        body: JSON.stringify(req),
      }),
  },

  settings: {
    getLLM: () => fetchJSON<LLMSettings>('/settings/llm'),
    updateLLM: (data: LLMSettings) =>
      fetchJSON<{ status: string }>('/settings/llm', {
        method: 'PUT',
        body: JSON.stringify(data),
      }),
  },
};
