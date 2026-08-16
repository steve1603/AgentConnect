import { useEffect, useRef } from "react";

import type { Message, ProviderConfig } from "../api";
import { RunStatus, type RunState } from "./RunStatus";
import { Trace } from "./Trace";

interface TranscriptProps {
  messages: Message[];
  run: RunState | null;
  providers: ProviderConfig[];
}

function labelFor(providers: ProviderConfig[], key?: string): string {
  return providers.find((provider) => provider.key === key)?.label ?? key ?? "";
}

function metaFor(providers: ProviderConfig[], message: Message): string | null {
  if (message.role !== "assistant") return null;

  const parts: string[] = [];
  if (message.chair) parts.push(`Chaired by ${labelFor(providers, message.chair)}`);
  if (message.trace?.chair_fallback && message.trace.chair_requested) {
    parts.push(`fallback from ${labelFor(providers, message.trace.chair_requested)}`);
  }
  if (message.mode) parts.push(`mode: ${message.mode}`);
  return parts.length > 0 ? parts.join(" · ") : null;
}

export function Transcript({ messages, run, providers }: TranscriptProps) {
  const bottom = useRef<HTMLDivElement>(null);

  // Follow the conversation as answers and progress events arrive.
  useEffect(() => {
    bottom.current?.scrollIntoView({ block: "end" });
  }, [messages, run]);

  if (messages.length === 0 && run === null) {
    return (
      <section className="transcript">
        <div className="empty-state">
          <h2>Three models. One answer.</h2>
          <p>
            ChatGPT, Claude, and Gemini answer independently, review each other&rsquo;s answers, and
            your chosen chair writes the final response.
          </p>
        </div>
      </section>
    );
  }

  return (
    <section className="transcript" aria-live="polite">
      {messages.map((message) => {
        const meta = metaFor(providers, message);
        return (
          <article className={`turn ${message.role}`} key={message.id}>
            <div className="bubble">{message.content}</div>
            {meta && <p className="turn-meta">{meta}</p>}
            {message.trace && <Trace trace={message.trace} />}
          </article>
        );
      })}
      {run && <RunStatus state={run} />}
      <div ref={bottom} />
    </section>
  );
}
