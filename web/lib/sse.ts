import { getToken } from './keycloak';

const API_BASE = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8082';

export interface PipelineEvent {
  projectId: string;
  stage: string;
  stageIndex: number;
  totalStages: number;
  status: 'started' | 'completed' | 'failed' | 'awaiting_approval';
  message: string;
  data?: Record<string, unknown>;
  timestamp: string;
}

export function connectPipelineSSE(
  projectId: string,
  onEvent: (event: PipelineEvent) => void,
  onError?: (error: Event) => void
): EventSource {
  // EventSource does not support custom headers,
  // so we pass the JWT token via query param.
  const token = getToken();
  const tokenParam = token ? `?token=${encodeURIComponent(token)}` : '';
  const url = `${API_BASE}/pipeline/progress/${projectId}${tokenParam}`;
  const source = new EventSource(url);

  source.onmessage = (e) => {
    try {
      const event: PipelineEvent = JSON.parse(e.data);
      onEvent(event);
    } catch {
      console.error('Failed to parse SSE event:', e.data);
    }
  };

  source.onerror = (e) => {
    onError?.(e);
  };

  return source;
}
