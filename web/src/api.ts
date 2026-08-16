// HTTP and SSE client for the roundtable API.

export type ProviderKey = "openai" | "anthropic" | "gemini";
export type Mode = "full" | "panel" | "direct";

export interface ProviderConfig {
  key: ProviderKey;
  label: string;
  model: string;
  configured: boolean;
}

export interface AppConfig {
  providers: ProviderConfig[];
  modes: Mode[];
  default_chair: ProviderKey;
  ready: boolean;
}

/** One provider call in one phase of a turn. */
export interface ProviderResult {
  provider: ProviderKey;
  label: string;
  model: string;
  ok: boolean;
  text?: string;
  error?: string;
  latency_ms: number;
  attempts: number;
}

/** Everything a completed turn produced, including the full trace. */
export interface Outcome {
  mode: Mode;
  final_text: string;
  chair?: ProviderKey;
  chair_requested?: ProviderKey;
  chair_fallback: boolean;
  answers: ProviderResult[];
  reviews: ProviderResult[];
  chair_result?: ProviderResult;
  failures: ProviderResult[];
}

export interface Conversation {
  id: number;
  title: string;
  created_at: string;
  updated_at: string;
}

export interface Message {
  id: number;
  role: "user" | "assistant";
  content: string;
  mode?: Mode;
  chair?: ProviderKey;
  trace?: Outcome;
  created_at: string;
}

export interface ConversationDetail extends Conversation {
  messages: Message[];
}

export type Phase = "independent" | "review" | "chair" | "direct";

/** A progress update from a running turn. */
export interface RunEvent {
  type: "phase" | "provider" | "complete" | "saved" | "error";
  phase?: Phase;
  status?: "start" | "done" | "skipped" | "failed" | "running" | "ok" | "error";
  provider?: ProviderKey;
  label?: string;
  model?: string;
  latency_ms?: number;
  attempts?: number;
  error?: string;
  reason?: string;
  providers?: ProviderKey[];
  succeeded?: ProviderKey[];
  result?: Outcome;
  message?: string;
  message_id?: number;
}

export interface ChatRequest {
  message: string;
  conversation_id: number | null;
  mode: Mode;
  chair: ProviderKey | null;
  models: Partial<Record<ProviderKey, string>>;
}

export interface ChatAccepted {
  run_id: string;
  conversation_id: number;
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    headers: { "Content-Type": "application/json" },
    ...init,
  });

  if (!response.ok) {
    let detail = `Request failed (${response.status})`;
    try {
      const body = (await response.json()) as { detail?: string };
      if (body?.detail) detail = body.detail;
    } catch {
      // non-JSON error body; keep the status-based message
    }
    throw new Error(detail);
  }

  return response.status === 204 ? (undefined as T) : ((await response.json()) as T);
}

export const api = {
  config: () => request<AppConfig>("/api/config"),
  conversations: () => request<Conversation[]>("/api/conversations"),
  conversation: (id: number) => request<ConversationDetail>(`/api/conversations/${id}`),
  deleteConversation: (id: number) =>
    request<void>(`/api/conversations/${id}`, { method: "DELETE" }),
  chat: (body: ChatRequest) =>
    request<ChatAccepted>("/api/chat", { method: "POST", body: JSON.stringify(body) }),
};

/**
 * Stream a run's progress events, resolving once the turn is finished.
 *
 * The server buffers each run's events, so attaching late still replays the
 * whole turn from the beginning.
 */
export function streamRun(runId: string, onEvent: (event: RunEvent) => void): Promise<void> {
  return new Promise((resolve) => {
    const source = new EventSource(`/api/runs/${runId}/events`);
    let settled = false;

    const finish = () => {
      if (settled) return;
      settled = true;
      source.close();
      resolve();
    };

    source.onmessage = (message) => {
      let event: RunEvent;
      try {
        event = JSON.parse(message.data) as RunEvent;
      } catch {
        return;
      }
      onEvent(event);
      if (event.type === "saved" || event.type === "error") finish();
    };

    // The stream also closes normally once the server finishes the run.
    source.onerror = finish;
  });
}
