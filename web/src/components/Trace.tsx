import type { Outcome, ProviderResult } from "../api";

interface Entry {
  heading: string;
  result: ProviderResult;
}

/** Flatten a turn's outcome into the ordered entries shown in the trace. */
function entriesOf(trace: Outcome): Entry[] {
  const entries: Entry[] = [];
  for (const answer of trace.answers ?? []) {
    entries.push({ heading: `${answer.label} — independent answer`, result: answer });
  }
  for (const review of trace.reviews ?? []) {
    entries.push({ heading: `${review.label} — peer review`, result: review });
  }
  if (trace.chair_result) {
    entries.push({
      heading: `${trace.chair_result.label} — chair synthesis`,
      result: trace.chair_result,
    });
  }
  return entries;
}

function formatLatency(ms: number): string {
  return ms >= 1000 ? `${(ms / 1000).toFixed(1)}s` : `${ms}ms`;
}

/**
 * The expandable deliberation record. It shows only normal visible model
 * output: no provider's hidden reasoning is requested or stored.
 */
export function Trace({ trace }: { trace: Outcome }) {
  const entries = entriesOf(trace);
  if (entries.length === 0) return null;

  return (
    <details className="trace">
      <summary>Roundtable deliberation</summary>
      <div className="trace-body">
        {entries.map(({ heading, result }, index) => (
          <div
            className={result.ok ? "trace-entry" : "trace-entry failed"}
            key={`${result.provider}-${heading}-${index}`}
          >
            <h4 className={`badge-${result.provider}`}>
              {heading}
              <span className="tag">
                {result.model} · {formatLatency(result.latency_ms)}
              </span>
            </h4>
            <div className="text">{result.ok ? result.text : `Failed: ${result.error}`}</div>
          </div>
        ))}
      </div>
    </details>
  );
}
