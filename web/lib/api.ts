const API_BASE = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8082';

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

export interface StageInfo {
  stage: string;
  stageIndex: number;
  status: string;
  input?: Record<string, unknown>;
  output?: Record<string, unknown>;
  error?: string;
  startedAt?: string;
  finishedAt?: string;
}

export interface ProjectSummary {
  projectId: string;
  totalStages: number;
  completedStages: number;
  failedStages: number;
  startedAt?: string;
  finishedAt?: string;
  lastUpdated: string;
  overallStatus: string;
}

export type AgentModelConfig = {
  flash: string;
  standard: string;
  premium: string;
};

export type AgentModelsConfig = {
  agents: Record<string, AgentModelConfig>;
};

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
    getStages: (projectId: string) =>
      fetchJSON<{ projectId: string; stages: StageInfo[] }>(`/pipeline/${projectId}`),
    updateStageOutput: (projectId: string, stage: string, output: Record<string, unknown>) =>
      fetchJSON<{ status: string }>(`/pipeline/${projectId}/stage/${stage}/output`, {
        method: 'PATCH',
        body: JSON.stringify(output),
      }),
    updateStageInput: (projectId: string, stage: string, input: Record<string, unknown>) =>
      fetchJSON<{ status: string }>(`/pipeline/${projectId}/stage/${stage}/input`, {
        method: 'PATCH',
        body: JSON.stringify(input),
      }),
    retryStage: (projectId: string, stage: string, inputOverride?: Record<string, unknown>) =>
      fetchJSON<{ status: string; projectId: string; stage: string }>(
        `/pipeline/${projectId}/retry/${stage}`,
        { method: 'POST', body: JSON.stringify(inputOverride ?? {}) }
      ),
    addStageMedia: (projectId: string, stage: string, url: string, label: string, mimeType: string) =>
      fetchJSON<{ status: string; url: string }>(`/pipeline/${projectId}/stage/${stage}/media`, {
        method: 'POST',
        body: JSON.stringify({ url, label, mimeType }),
      }),
  },

  projects: {
    list: () => fetchJSON<{ projects: ProjectSummary[]; count: number }>('/projects'),
  },

  settings: {
    getLLM: () => fetchJSON<LLMSettings>('/settings/llm'),
    updateLLM: (data: LLMSettings) =>
      fetchJSON<{ status: string }>('/settings/llm', {
        method: 'PUT',
        body: JSON.stringify(data),
      }),
  },

  agentModels: {
    get: (): Promise<AgentModelsConfig> =>
      fetchJSON<AgentModelsConfig>('/settings/agents'),
    update: (config: AgentModelsConfig): Promise<void> =>
      fetch(`${API_BASE}/settings/agents`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(config),
      }).then(r => { if (!r.ok) throw new Error('Failed to update'); }),
  },
};
