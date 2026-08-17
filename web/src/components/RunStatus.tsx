import type { Phase, ProviderKey, RunEvent } from "../api";

const PHASE_LABELS: Record<Phase, string> = {
  independent: "Independent answers",
  review: "Peer review",
  chair: "Chair synthesis",
  direct: "Direct answer",
};

const PHASE_ORDER: Phase[] = ["independent", "review", "chair", "direct"];

/** One provider's state within one phase. */
export interface ChipState {
  phase: Phase;
  provider: ProviderKey;
  label: string;
  status: "running" | "ok" | "error";
  latencyMs?: number;
  error?: string;
}

export interface RunState {
  chips: ChipState[];
  notes: string[];
}

export const emptyRun: RunState = { chips: [], notes: [] };

/** Fold one progress event into the live status board's state. */
export function reduceRun(state: RunState, event: RunEvent): RunState {
  if (event.type === "provider" && event.phase && event.provider) {
    const chip: ChipState = {
      phase: event.phase,
      provider: event.provider,
      label: event.label ?? event.provider,
      status: (event.status as ChipState["status"]) ?? "running",
      latencyMs: event.latency_ms,
      error: event.error,
    };
    const index = state.chips.findIndex(
      (existing) => existing.phase === chip.phase && existing.provider === chip.provider,
    );
    const chips = [...state.chips];
    if (index === -1) chips.push(chip);
    else chips[index] = chip;
    return { ...state, chips };
  }

  if (event.type === "phase" && event.reason) {
    const note =
      event.status === "skipped"
        ? `Peer review skipped: ${event.reason}`
        : `Chair synthesis: ${event.reason}`;
    return { ...state, notes: [...state.notes, note] };
  }

  return state;
}

function formatLatency(ms?: number): string {
  if (ms === undefined) return "";
  return ms >= 1000 ? `${(ms / 1000).toFixed(1)}s` : `${ms}ms`;
}

function detailFor(chip: ChipState): string {
  if (chip.status === "running") return "answering…";
  if (chip.status === "ok") return formatLatency(chip.latencyMs);
  return chip.error ?? "failed";
}

export function RunStatus({ state }: { state: RunState }) {
  const phases = PHASE_ORDER.filter((phase) => state.chips.some((chip) => chip.phase === phase));

  return (
    <section className="run-status" aria-label="Roundtable progress">
      <h3>Roundtable working</h3>
      {phases.map((phase) => (
        <div className="phase" key={phase}>
          <div className="phase-name">{PHASE_LABELS[phase]}</div>
          <div className="provider-row">
            {state.chips
              .filter((chip) => chip.phase === phase)
              .map((chip) => (
                <div
                  className={`provider-chip chip-${chip.provider} ${chip.status}`}
                  key={`${phase}:${chip.provider}`}
                >
                  <span className="marker" />
                  <span className="name">{chip.label}</span>
                  <span className="detail">{detailFor(chip)}</span>
                </div>
              ))}
          </div>
        </div>
      ))}
      {state.notes.map((note) => (
        <div className="phase-name" key={note}>
          {note}
        </div>
      ))}
    </section>
  );
}
