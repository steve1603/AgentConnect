"use strict";

/**
 * AI Roundtable front end.
 *
 * A turn is started with POST /api/chat, which returns a run id; progress is
 * then streamed from GET /api/runs/{id}/events as server-sent events.
 */

const PHASE_LABELS = {
  independent: "Independent answers",
  review: "Peer review",
  chair: "Chair synthesis",
  direct: "Direct answer",
};

const state = {
  config: null,
  conversationId: null,
  busy: false,
  overrides: loadOverrides(),
  source: null,
};

const el = {
  transcript: document.getElementById("transcript"),
  emptyState: document.getElementById("empty-state"),
  form: document.getElementById("composer-form"),
  input: document.getElementById("composer-input"),
  send: document.getElementById("send"),
  notice: document.getElementById("notice"),
  mode: document.getElementById("mode"),
  chair: document.getElementById("chair"),
  status: document.getElementById("provider-status"),
  list: document.getElementById("conversation-list"),
  newChat: document.getElementById("new-chat"),
  settingsBtn: document.getElementById("open-settings"),
  dialog: document.getElementById("settings-dialog"),
  overrideFields: document.getElementById("model-overrides"),
  saveSettings: document.getElementById("save-settings"),
  toggleSidebar: document.getElementById("toggle-sidebar"),
  layout: document.querySelector(".layout"),
};

// --------------------------------------------------------------- utilities

function loadOverrides() {
  try {
    const raw = localStorage.getItem("roundtable.models");
    return raw ? JSON.parse(raw) : {};
  } catch {
    return {};
  }
}

function saveOverrides(values) {
  state.overrides = values;
  try {
    localStorage.setItem("roundtable.models", JSON.stringify(values));
  } catch {
    /* storage unavailable (private mode); overrides stay in memory */
  }
}

function showNotice(message, kind = "error") {
  el.notice.textContent = message;
  el.notice.className = kind === "warn" ? "notice warn" : "notice";
  el.notice.hidden = !message;
}

function clearNotice() {
  showNotice("");
}

async function api(path, options = {}) {
  const response = await fetch(path, {
    headers: { "Content-Type": "application/json" },
    ...options,
  });
  if (!response.ok) {
    let detail = `Request failed (${response.status})`;
    try {
      const body = await response.json();
      if (body && body.detail) detail = body.detail;
    } catch {
      /* non-JSON error body */
    }
    throw new Error(detail);
  }
  return response.status === 204 ? null : response.json();
}

function scrollToBottom() {
  el.transcript.scrollTop = el.transcript.scrollHeight;
}

function providerLabel(key) {
  const found = (state.config?.providers || []).find((p) => p.key === key);
  return found ? found.label : key;
}

function formatLatency(ms) {
  if (typeof ms !== "number") return "";
  return ms >= 1000 ? `${(ms / 1000).toFixed(1)}s` : `${ms}ms`;
}

// ------------------------------------------------------------------ render

function renderTurn(role, text, meta) {
  el.emptyState.hidden = true;
  const turn = document.createElement("article");
  turn.className = `turn ${role}`;

  const bubble = document.createElement("div");
  bubble.className = "bubble";
  bubble.textContent = text;
  turn.append(bubble);

  if (meta) {
    const line = document.createElement("p");
    line.className = "turn-meta";
    line.textContent = meta;
    turn.append(line);
  }

  el.transcript.append(turn);
  return turn;
}

function renderTrace(turn, trace) {
  if (!trace) return;

  const entries = [];
  for (const answer of trace.answers || []) {
    entries.push({ heading: `${answer.label} — independent answer`, item: answer });
  }
  for (const review of trace.reviews || []) {
    entries.push({ heading: `${review.label} — peer review`, item: review });
  }
  if (trace.chair_result) {
    entries.push({
      heading: `${trace.chair_result.label} — chair synthesis`,
      item: trace.chair_result,
    });
  }
  if (!entries.length) return;

  const details = document.createElement("details");
  details.className = "trace";
  const summary = document.createElement("summary");
  summary.textContent = "Roundtable deliberation";
  details.append(summary);

  const body = document.createElement("div");
  body.className = "trace-body";

  for (const { heading, item } of entries) {
    const entry = document.createElement("div");
    entry.className = item.ok ? "trace-entry" : "trace-entry failed";

    const title = document.createElement("h4");
    title.textContent = heading;
    title.classList.add(`badge-${item.provider}`);

    const tag = document.createElement("span");
    tag.className = "tag";
    tag.textContent = [item.model, formatLatency(item.latency_ms)].filter(Boolean).join(" · ");
    title.append(tag);

    const text = document.createElement("div");
    text.className = "text";
    text.textContent = item.ok ? item.text : `Failed: ${item.error}`;

    entry.append(title, text);
    body.append(entry);
  }

  details.append(body);
  turn.append(details);
}

class RunStatus {
  constructor() {
    this.node = document.createElement("section");
    this.node.className = "run-status";
    const heading = document.createElement("h3");
    heading.textContent = "Roundtable working";
    this.node.append(heading);
    this.phases = new Map();
    this.chips = new Map();
    el.transcript.append(this.node);
  }

  phase(name) {
    if (this.phases.has(name)) return this.phases.get(name);
    const wrapper = document.createElement("div");
    wrapper.className = "phase";
    const label = document.createElement("div");
    label.className = "phase-name";
    label.textContent = PHASE_LABELS[name] || name;
    const row = document.createElement("div");
    row.className = "provider-row";
    wrapper.append(label, row);
    this.node.append(wrapper);
    this.phases.set(name, row);
    return row;
  }

  update(event) {
    const key = `${event.phase}:${event.provider}`;
    let chip = this.chips.get(key);
    if (!chip) {
      chip = document.createElement("div");
      chip.className = "provider-chip";
      const marker = document.createElement("span");
      marker.className = "marker";
      const name = document.createElement("span");
      name.className = "name";
      const detail = document.createElement("span");
      detail.className = "detail";
      chip.append(marker, name, detail);
      this.phase(event.phase).append(chip);
      this.chips.set(key, chip);
    }

    chip.className = `provider-chip ${event.status}`;
    chip.querySelector(".name").textContent = event.label || providerLabel(event.provider);

    let detail = "";
    if (event.status === "running") detail = "answering…";
    else if (event.status === "ok") detail = formatLatency(event.latency_ms);
    else if (event.status === "error") detail = event.error || "failed";
    chip.querySelector(".detail").textContent = detail;
  }

  note(text) {
    const line = document.createElement("div");
    line.className = "phase-name";
    line.textContent = text;
    this.node.append(line);
  }

  remove() {
    this.node.remove();
  }
}

// ----------------------------------------------------------- conversations

async function refreshConversations() {
  let conversations = [];
  try {
    conversations = await api("/api/conversations");
  } catch {
    return;
  }

  el.list.replaceChildren();
  for (const conversation of conversations) {
    const row = document.createElement("div");
    row.className =
      "conversation" + (conversation.id === state.conversationId ? " active" : "");

    const open = document.createElement("button");
    open.type = "button";
    open.className = "title";
    open.textContent = conversation.title;
    open.addEventListener("click", () => openConversation(conversation.id));

    const remove = document.createElement("button");
    remove.type = "button";
    remove.className = "delete";
    remove.title = "Delete conversation";
    remove.setAttribute("aria-label", `Delete ${conversation.title}`);
    remove.textContent = "×";
    remove.addEventListener("click", async (event) => {
      event.stopPropagation();
      if (!confirm("Delete this conversation and its deliberation traces?")) return;
      await api(`/api/conversations/${conversation.id}`, { method: "DELETE" });
      if (state.conversationId === conversation.id) startNewConversation();
      refreshConversations();
    });

    row.append(open, remove);
    el.list.append(row);
  }
}

async function openConversation(id) {
  if (state.busy) return;
  const detail = await api(`/api/conversations/${id}`);
  state.conversationId = id;
  el.transcript.replaceChildren(el.emptyState);
  el.emptyState.hidden = true;

  for (const message of detail.messages) {
    const meta =
      message.role === "assistant" && message.chair
        ? `Chaired by ${providerLabel(message.chair)}`
        : null;
    const turn = renderTurn(message.role, message.content, meta);
    if (message.role === "assistant") renderTrace(turn, message.trace);
  }

  clearNotice();
  refreshConversations();
  scrollToBottom();
}

function startNewConversation() {
  state.conversationId = null;
  el.transcript.replaceChildren(el.emptyState);
  el.emptyState.hidden = false;
  clearNotice();
  refreshConversations();
}

// -------------------------------------------------------------------- chat

function setBusy(busy) {
  state.busy = busy;
  el.send.disabled = busy;
  el.input.disabled = busy;
  el.send.textContent = busy ? "Working…" : "Send";
}

async function submitMessage(text) {
  clearNotice();
  setBusy(true);
  renderTurn("user", text, null);
  scrollToBottom();

  const status = new RunStatus();
  scrollToBottom();

  let accepted;
  try {
    accepted = await api("/api/chat", {
      method: "POST",
      body: JSON.stringify({
        message: text,
        conversation_id: state.conversationId,
        mode: el.mode.value,
        chair: el.chair.value || null,
        models: state.overrides,
      }),
    });
  } catch (error) {
    status.remove();
    showNotice(error.message);
    setBusy(false);
    return;
  }

  state.conversationId = accepted.conversation_id;
  await streamRun(accepted.run_id, status);
  setBusy(false);
  refreshConversations();
}

function streamRun(runId, status) {
  return new Promise((resolve) => {
    const source = new EventSource(`/api/runs/${runId}/events`);
    state.source = source;
    let settled = false;

    const finish = () => {
      if (settled) return;
      settled = true;
      source.close();
      state.source = null;
      status.remove();
      resolve();
    };

    source.onmessage = (message) => {
      let event;
      try {
        event = JSON.parse(message.data);
      } catch {
        return;
      }

      if (event.type === "provider") {
        status.update(event);
        scrollToBottom();
      } else if (event.type === "phase" && event.status === "skipped") {
        status.note(`Peer review skipped: ${event.reason}`);
      } else if (event.type === "phase" && event.status === "failed") {
        status.note(`Chair synthesis failed: ${event.reason}`);
      } else if (event.type === "complete") {
        const result = event.result;
        status.remove();
        const meta = buildMeta(result);
        const turn = renderTurn("assistant", result.final_text, meta);
        renderTrace(turn, result);
        if (result.failures && result.failures.length) {
          showNotice(
            `${result.failures.length} provider call(s) failed; the answer used the rest.`,
            "warn",
          );
        }
        scrollToBottom();
      } else if (event.type === "saved") {
        finish();
      } else if (event.type === "error") {
        showNotice(event.message);
        finish();
      }
    };

    source.onerror = () => {
      // The stream also closes normally when the server finishes the run.
      finish();
    };
  });
}

function buildMeta(result) {
  const parts = [];
  if (result.chair) parts.push(`Chaired by ${providerLabel(result.chair)}`);
  if (result.chair_fallback && result.chair_requested) {
    parts.push(`fallback from ${providerLabel(result.chair_requested)}`);
  }
  parts.push(`mode: ${result.mode}`);
  return parts.join(" · ");
}

// ------------------------------------------------------------------ config

async function loadConfig() {
  try {
    state.config = await api("/api/config");
  } catch (error) {
    showNotice(`Could not load configuration: ${error.message}`);
    return;
  }

  el.status.replaceChildren();
  el.chair.replaceChildren();
  el.overrideFields.replaceChildren();

  for (const provider of state.config.providers) {
    const dot = document.createElement("span");
    dot.className = `status-dot ${provider.configured ? "on" : "off"}`;
    dot.textContent = provider.label;
    dot.title = provider.configured
      ? `${provider.model} — API key configured`
      : "No API key configured";
    el.status.append(dot);

    if (provider.configured) {
      const option = document.createElement("option");
      option.value = provider.key;
      option.textContent = provider.label;
      el.chair.append(option);
    }

    const field = document.createElement("div");
    field.className = "override";
    const label = document.createElement("label");
    label.textContent = provider.label;
    label.htmlFor = `override-${provider.key}`;
    const input = document.createElement("input");
    input.type = "text";
    input.id = `override-${provider.key}`;
    input.dataset.provider = provider.key;
    input.placeholder = provider.model;
    input.value = state.overrides[provider.key] || "";
    field.append(label, input);
    el.overrideFields.append(field);
  }

  if (state.config.default_chair) el.chair.value = state.config.default_chair;

  if (!state.config.ready) {
    showNotice(
      "No provider API keys are configured. Add them to .env and restart the app.",
    );
    el.send.disabled = true;
  }
}

// ------------------------------------------------------------------ events

el.form.addEventListener("submit", (event) => {
  event.preventDefault();
  const text = el.input.value.trim();
  if (!text || state.busy) return;
  el.input.value = "";
  el.input.style.height = "auto";
  submitMessage(text);
});

el.input.addEventListener("keydown", (event) => {
  if (event.key === "Enter" && !event.shiftKey) {
    event.preventDefault();
    el.form.requestSubmit();
  }
});

el.input.addEventListener("input", () => {
  el.input.style.height = "auto";
  el.input.style.height = `${Math.min(el.input.scrollHeight, 220)}px`;
});

el.newChat.addEventListener("click", startNewConversation);

el.settingsBtn.addEventListener("click", () => el.dialog.showModal());

el.dialog.addEventListener("close", () => {
  if (el.dialog.returnValue !== "save") return;
  const values = {};
  for (const input of el.overrideFields.querySelectorAll("input")) {
    const value = input.value.trim();
    if (value) values[input.dataset.provider] = value;
  }
  saveOverrides(values);
});

el.toggleSidebar.addEventListener("click", () => {
  const mobile = window.matchMedia("(max-width: 860px)").matches;
  el.layout.classList.toggle(mobile ? "mobile-open" : "collapsed");
});

window.addEventListener("beforeunload", () => {
  if (state.source) state.source.close();
});

(async function init() {
  await loadConfig();
  await refreshConversations();
})();
