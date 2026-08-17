import type { Conversation } from "../api";

/** Three seats at the table, one per provider accent. */
function BrandMark() {
  return (
    <svg className="brand-mark" viewBox="0 0 32 32" aria-hidden="true" focusable="false">
      <circle cx="16" cy="16" r="15" className="brand-ring" />
      <circle cx="16" cy="9" r="3.4" className="seat-openai" />
      <circle cx="9.5" cy="21" r="3.4" className="seat-anthropic" />
      <circle cx="22.5" cy="21" r="3.4" className="seat-gemini" />
    </svg>
  );
}

interface SidebarProps {
  conversations: Conversation[];
  activeId: number | null;
  busy: boolean;
  onSelect: (id: number) => void;
  onDelete: (id: number) => void;
  onNew: () => void;
  onOpenSettings: () => void;
}

export function Sidebar({
  conversations,
  activeId,
  busy,
  onSelect,
  onDelete,
  onNew,
  onOpenSettings,
}: SidebarProps) {
  return (
    <aside className="sidebar">
      <div className="sidebar-head">
        <h1>
          <BrandMark />
          AI Roundtable
        </h1>
        <button type="button" className="btn primary block" onClick={onNew} disabled={busy}>
          New conversation
        </button>
      </div>

      <nav className="conversations" aria-label="Conversations">
        {conversations.map((conversation) => (
          <div
            className={`conversation${conversation.id === activeId ? " active" : ""}`}
            key={conversation.id}
          >
            <button
              type="button"
              className="title"
              onClick={() => onSelect(conversation.id)}
              disabled={busy}
            >
              {conversation.title}
            </button>
            <button
              type="button"
              className="delete"
              aria-label={`Delete ${conversation.title}`}
              title="Delete conversation"
              onClick={() => onDelete(conversation.id)}
              disabled={busy}
            >
              ×
            </button>
          </div>
        ))}
      </nav>

      <div className="sidebar-foot">
        <button type="button" className="btn ghost block" onClick={onOpenSettings}>
          Settings
        </button>
        <p className="version">v1.0.0</p>
      </div>
    </aside>
  );
}
