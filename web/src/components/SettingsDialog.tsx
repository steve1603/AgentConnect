import { useEffect, useRef, useState } from "react";

import type { ProviderConfig, ProviderKey } from "../api";

type Overrides = Partial<Record<ProviderKey, string>>;

interface SettingsDialogProps {
  open: boolean;
  providers: ProviderConfig[];
  overrides: Overrides;
  onSave: (overrides: Overrides) => void;
  onClose: () => void;
}

/**
 * Per-browser model overrides. API keys are never handled here: they stay on
 * the server and are never sent to the page.
 */
export function SettingsDialog({
  open,
  providers,
  overrides,
  onSave,
  onClose,
}: SettingsDialogProps) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [draft, setDraft] = useState<Overrides>(overrides);

  useEffect(() => {
    if (open) {
      setDraft(overrides);
      dialog.current?.showModal();
    } else {
      dialog.current?.close();
    }
  }, [open, overrides]);

  const save = () => {
    const cleaned: Overrides = {};
    for (const [key, value] of Object.entries(draft)) {
      const trimmed = value?.trim();
      if (trimmed) cleaned[key as ProviderKey] = trimmed;
    }
    onSave(cleaned);
  };

  return (
    <dialog ref={dialog} onClose={onClose}>
      <div className="settings">
        <h2>Settings</h2>
        <p className="hint">
          Model overrides are stored in this browser only and do not change <code>.env</code>. Leave
          a field blank to use the server default.
        </p>

        {providers.map((provider) => (
          <div className="override" key={provider.key}>
            <label htmlFor={`override-${provider.key}`}>{provider.label}</label>
            <input
              id={`override-${provider.key}`}
              type="text"
              placeholder={provider.model}
              value={draft[provider.key] ?? ""}
              onChange={(event) =>
                setDraft((current) => ({ ...current, [provider.key]: event.target.value }))
              }
            />
          </div>
        ))}

        <p className="hint">API keys are held on the server and are never sent to this page.</p>

        <menu>
          <button type="button" className="btn ghost" onClick={onClose}>
            Close
          </button>
          <button type="button" className="btn primary" onClick={save}>
            Save
          </button>
        </menu>
      </div>
    </dialog>
  );
}
