import { useCallback, useEffect, useState } from "react";

import {
  api,
  streamRun,
  type AppConfig,
  type Conversation,
  type Message,
  type Mode,
  type ProviderKey,
  type RunEvent,
} from "./api";
import { Composer } from "./components/Composer";
import { emptyRun, reduceRun, type RunState } from "./components/RunStatus";
import { Sidebar } from "./components/Sidebar";
import { Transcript } from "./components/Transcript";
import { SettingsDialog } from "./components/SettingsDialog";

const OVERRIDES_KEY = "roundtable.models";

type Overrides = Partial<Record<ProviderKey, string>>;
type Notice = { text: string; kind: "error" | "warn" } | null;

function loadOverrides(): Overrides {
  try {
    const raw = localStorage.getItem(OVERRIDES_KEY);
    return raw ? (JSON.parse(raw) as Overrides) : {};
  } catch {
    return {};
  }
}

export function App() {
  const [config, setConfig] = useState<AppConfig | null>(null);
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [activeId, setActiveId] = useState<number | null>(null);
  const [messages, setMessages] = useState<Message[]>([]);
  const [run, setRun] = useState<RunState | null>(null);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<Notice>(null);
  const [mode, setMode] = useState<Mode>("full");
  const [chair, setChair] = useState<ProviderKey | "">("");
  const [overrides, setOverrides] = useState<Overrides>(loadOverrides);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [sidebarOpen, setSidebarOpen] = useState(false);

  const refreshConversations = useCallback(async () => {
    try {
      setConversations(await api.conversations());
    } catch {
      // The sidebar is not critical; leave the previous list in place.
    }
  }, []);

  useEffect(() => {
    void (async () => {
      try {
        const loaded = await api.config();
        setConfig(loaded);
        setChair(loaded.default_chair);
        if (!loaded.ready) {
          setNotice({
            kind: "error",
            text: "No provider API keys are configured. Add them to .env and restart the app.",
          });
        }
      } catch (error) {
        setNotice({ text: `Could not load configuration: ${(error as Error).message}`, kind: "error" });
      }
      await refreshConversations();
    })();
  }, [refreshConversations]);

  const openConversation = useCallback(async (id: number) => {
    try {
      const detail = await api.conversation(id);
      setActiveId(id);
      setMessages(detail.messages);
      setRun(null);
      setNotice(null);
      setSidebarOpen(false);
    } catch (error) {
      setNotice({ text: (error as Error).message, kind: "error" });
    }
  }, []);

  const startNewConversation = useCallback(() => {
    setActiveId(null);
    setMessages([]);
    setRun(null);
    setNotice(null);
    setSidebarOpen(false);
  }, []);

  const deleteConversation = useCallback(
    async (id: number) => {
      if (!window.confirm("Delete this conversation and its deliberation traces?")) return;
      try {
        await api.deleteConversation(id);
        if (activeId === id) startNewConversation();
        await refreshConversations();
      } catch (error) {
        setNotice({ text: (error as Error).message, kind: "error" });
      }
    },
    [activeId, refreshConversations, startNewConversation],
  );

  const send = useCallback(
    async (text: string) => {
      setNotice(null);
      setBusy(true);
      setRun(emptyRun);

      // Show the user's turn immediately; the server assigns the real id.
      const optimistic: Message = {
        id: -Date.now(),
        role: "user",
        content: text,
        created_at: new Date().toISOString(),
      };
      setMessages((current) => [...current, optimistic]);

      let accepted;
      try {
        accepted = await api.chat({
          message: text,
          conversation_id: activeId,
          mode,
          chair: chair || null,
          models: overrides,
        });
      } catch (error) {
        setNotice({ text: (error as Error).message, kind: "error" });
        setMessages((current) => current.filter((message) => message.id !== optimistic.id));
        setRun(null);
        setBusy(false);
        return;
      }

      setActiveId(accepted.conversation_id);

      const handle = (event: RunEvent) => {
        if (event.type === "complete" && event.result) {
          const outcome = event.result;
          setRun(null);
          setMessages((current) => [
            ...current,
            {
              id: -Date.now() - 1,
              role: "assistant",
              content: outcome.final_text,
              mode: outcome.mode,
              chair: outcome.chair,
              trace: outcome,
              created_at: new Date().toISOString(),
            },
          ]);
          if (outcome.failures.length > 0) {
            setNotice({
              kind: "warn",
              text: `${outcome.failures.length} provider call(s) failed; the answer used the rest.`,
            });
          }
        } else if (event.type === "error") {
          setNotice({ text: event.message ?? "The turn failed.", kind: "error" });
          setRun(null);
        } else {
          setRun((current) => reduceRun(current ?? emptyRun, event));
        }
      };

      await streamRun(accepted.run_id, handle);

      // Reload from the server so ids, timestamps, and traces are canonical.
      try {
        const detail = await api.conversation(accepted.conversation_id);
        setMessages(detail.messages);
      } catch {
        // Keep the optimistic transcript if the reload fails.
      }
      setRun(null);
      setBusy(false);
      await refreshConversations();
    },
    [activeId, chair, mode, overrides, refreshConversations],
  );

  const saveOverrides = useCallback((next: Overrides) => {
    setOverrides(next);
    try {
      localStorage.setItem(OVERRIDES_KEY, JSON.stringify(next));
    } catch {
      // Storage can be unavailable (private mode); keep them in memory.
    }
    setSettingsOpen(false);
  }, []);

  const providers = config?.providers ?? [];
  const configured = providers.filter((provider) => provider.configured);

  return (
    <div className={`layout${sidebarOpen ? " mobile-open" : ""}`}>
      <Sidebar
        conversations={conversations}
        activeId={activeId}
        busy={busy}
        onSelect={(id) => void openConversation(id)}
        onDelete={(id) => void deleteConversation(id)}
        onNew={startNewConversation}
        onOpenSettings={() => setSettingsOpen(true)}
      />

      <main className="main">
        <header className="topbar">
          <button
            type="button"
            className="btn ghost icon"
            aria-label="Toggle sidebar"
            onClick={() => setSidebarOpen((open) => !open)}
          >
            ☰
          </button>

          <div className="controls">
            <label className="control">
              <span>Mode</span>
              <select value={mode} onChange={(event) => setMode(event.target.value as Mode)}>
                <option value="full">Full roundtable</option>
                <option value="panel">Panel (no peer review)</option>
                <option value="direct">Direct (single model)</option>
              </select>
            </label>

            <label className="control">
              <span>Chair</span>
              <select
                value={chair}
                onChange={(event) => setChair(event.target.value as ProviderKey)}
              >
                {configured.map((provider) => (
                  <option value={provider.key} key={provider.key}>
                    {provider.label}
                  </option>
                ))}
              </select>
            </label>
          </div>

          <div className="status-dots">
            {providers.map((provider) => (
              <span
                className={`status-dot dot-${provider.key} ${provider.configured ? "on" : "off"}`}
                key={provider.key}
                title={
                  provider.configured
                    ? `${provider.model} — API key configured`
                    : "No API key configured"
                }
              >
                {provider.label}
              </span>
            ))}
          </div>
        </header>

        <Transcript messages={messages} run={run} providers={providers} />

        <Composer
          busy={busy}
          disabled={config !== null && !config.ready}
          notice={notice}
          onSend={(text) => void send(text)}
        />
      </main>

      <SettingsDialog
        open={settingsOpen}
        providers={providers}
        overrides={overrides}
        onSave={saveOverrides}
        onClose={() => setSettingsOpen(false)}
      />
    </div>
  );
}
