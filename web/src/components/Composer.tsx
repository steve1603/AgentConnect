import { useRef, useState, type FormEvent, type KeyboardEvent } from "react";

interface ComposerProps {
  busy: boolean;
  disabled: boolean;
  notice: { text: string; kind: "error" | "warn" } | null;
  onSend: (text: string) => void;
}

const MAX_HEIGHT = 220;

export function Composer({ busy, disabled, notice, onSend }: ComposerProps) {
  const [value, setValue] = useState("");
  const textarea = useRef<HTMLTextAreaElement>(null);

  const submit = (event: FormEvent) => {
    event.preventDefault();
    const text = value.trim();
    if (!text || busy || disabled) return;
    setValue("");
    if (textarea.current) textarea.current.style.height = "auto";
    onSend(text);
  };

  const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      event.currentTarget.form?.requestSubmit();
    }
  };

  return (
    <section className="composer">
      {notice && <div className={notice.kind === "warn" ? "notice warn" : "notice"}>{notice.text}</div>}
      <form onSubmit={submit}>
        <textarea
          ref={textarea}
          rows={1}
          value={value}
          placeholder="Ask the roundtable… (Enter to send, Shift+Enter for a new line)"
          autoComplete="off"
          disabled={busy || disabled}
          onKeyDown={onKeyDown}
          onChange={(event) => {
            setValue(event.target.value);
            const element = event.target;
            element.style.height = "auto";
            element.style.height = `${Math.min(element.scrollHeight, MAX_HEIGHT)}px`;
          }}
        />
        <button type="submit" className="btn primary" disabled={busy || disabled}>
          {busy ? "Working…" : "Send"}
        </button>
      </form>
    </section>
  );
}
