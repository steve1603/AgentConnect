# AI Roundtable

**Version 1.0.0**

A single local chat application where OpenAI/ChatGPT, Anthropic/Claude, and Google/Gemini collaborate on each user turn. Go backend, React frontend, one self-contained binary.

## How one turn works

1. **Independent answers**: every configured model receives the conversation history and current request in parallel.
2. **Peer review**: each successful model receives all successful first-pass answers and critiques them.
3. **Chair synthesis**: the selected chair receives the conversation, candidate answers, and reviews and writes one final answer.
4. **Trace storage**: the final answer and the complete visible deliberation record are saved in SQLite.

```text
                         YOUR MESSAGE
                              │
                              ▼
                    ┌───────────────────┐
                    │ AI ROUND TABLE    │
                    │   Orchestrator    │
                    └─────────┬─────────┘
                              │
              ┌───────────────┼───────────────┐
              ▼               ▼               ▼
         ┌─────────┐     ┌─────────┐     ┌─────────┐
         │ ChatGPT │     │ Claude  │     │ Gemini  │
         └────┬────┘     └────┬────┘     └────┬────┘
              │               │               │
              └─────── Independent ───────────┘
                        answers
                              │
                              ▼
              ┌───────────────────────────────┐
              │          PEER REVIEW          │
              │ each model reviews all answers│
              └───────────────┬───────────────┘
                              │
                              ▼
                    ┌───────────────────┐
                    │ SYNTHESIS CHAIR   │
                    │ OpenAI / Claude / │
                    │ Gemini selectable │
                    └─────────┬─────────┘
                              │
                              ▼
                       FINAL ANSWER
```

The app deliberately does **not** request or expose providers' hidden chain-of-thought. The trace contains only normal model outputs explicitly requested for collaboration.

## Stack

| Layer     | Technology                                                                  |
| --------- | --------------------------------------------------------------------------- |
| Backend   | Go, standard-library `net/http`, no web framework                       |
| Frontend  | React 19 + TypeScript, built with Vite                                       |
| Storage   | SQLite via `modernc.org/sqlite` (pure Go — no cgo, no C toolchain)           |
| Transport | JSON over HTTP, plus Server-Sent Events for live turn progress               |
| Packaging | The built UI is embedded with `go:embed`, so the binary is self-contained    |

## Features

- One responsive dark-mode web chat
- ChatGPT + Claude + Gemini in one orchestration pipeline
- Parallel first-pass requests for lower latency
- Parallel peer-review round
- Selectable synthesis chair
- Full Roundtable, Panel, and Direct modes
- Per-provider model overrides, stored per browser
- Persistent SQLite conversation history
- Expandable deliberation trace on every answer
- Server-Sent Event progress updates, buffered so a late subscriber still sees the whole run
- Automatic retry with exponential backoff and jitter
- Chair fallback when the selected synthesis provider fails
- API keys kept server-side in `.env`
- Browser security headers and no third-party frontend dependencies at runtime
- Launchers for Windows (`.bat`, PowerShell) and for Linux, macOS, and Android/Termux (`run.sh`)
- Go test suite covering prompts, orchestration, providers, storage, and the HTTP API

## Theme

The UI uses a dark, tri-tone brand system defined entirely as CSS custom
properties at the top of `web/src/styles.css`. Retheming means editing that one
block; nothing else hard-codes a colour.

| Token                        | Value     | Used for                              |
| ---------------------------- | --------- | ------------------------------------- |
| `--bg` / `--bg-raised`       | `#0a1526` / `#111e33` | page and panel surfaces   |
| `--bg-sunken` / `--bg-inset` | `#060e1b` / `#17263f` | sidebar, inputs, user turns |
| `--border`                   | `#1e3252` | dividers and outlines                 |
| `--text` / `--text-dim`      | `#eaf0fa` / `#94a6c4` | body and secondary text   |
| `--accent`                   | `#17b8c4` | primary actions, focus rings          |
| `--openai`                   | `#2fd3a0` | ChatGPT's seat at the table           |
| `--anthropic`                | `#f0975a` | Claude's seat                         |
| `--gemini`                   | `#8b7bf7` | Gemini's seat                         |

The three provider accents carry through the whole interface — the brand mark,
the header status dots, the live progress chips, and the deliberation trace —
so it is always obvious which model produced which piece of a turn.

> The palette is an in-house interpretation, not a reproduction of any external
> brand's official assets. Swap the token block for real brand values when they
> are available.

## Provider interfaces

| Provider  | Interface                                             | Client            | Default model      |
| --------- | ----------------------------------------------------- | ----------------- | ------------------ |
| OpenAI    | Responses API (`POST /v1/responses`)                  | `net/http`        | `gpt-5.6`          |
| Anthropic | Messages API (`POST /v1/messages`)                    | official Go SDK   | `claude-sonnet-5`  |
| Google    | Gemini Interactions API (`POST /v1beta/interactions`) | `net/http`        | `gemini-3.7-flash` |

Each provider is a small adapter behind one interface, so changing a model ID or adding a fourth vendor touches one file in `internal/providers/`.

## Windows setup

### 1. Install Go

Install **Go 1.24 or newer** from [go.dev/dl](https://go.dev/dl/). Node.js is **not** required to run the app: the React UI is committed pre-built and embedded into the binary.

### 2. Extract the project

Extract it to a permanent folder, for example:

```text
C:\Users\<your-user>\Applications\ai-roundtable
```

### 3. Run the launcher

Double-click:

```text
run.bat
```

On the first run it will:

- compile `roundtable.exe`
- copy `.env.example` to `.env`
- open `.env` in Notepad

### 4. Add API keys

Edit `.env` and populate:

```dotenv
OPENAI_API_KEY=your_openai_key
ANTHROPIC_API_KEY=your_anthropic_key
GEMINI_API_KEY=your_gemini_key
```

Do not put quotes around the keys unless the key itself requires them.

You can run the application with only one or two keys, but the full roundtable requires all three.

### 5. Start again

Double-click `run.bat` again. The application will start at:

```text
http://127.0.0.1:8000
```

Your browser opens automatically.

## Android setup (Termux)

The app runs natively on a phone under [Termux](https://termux.dev) — the
SQLite driver is pure Go, so there is no NDK or C toolchain involved, and the
UI is embedded in the binary, so there is no Node.js either.

```bash
pkg update && pkg install golang git
git clone https://github.com/steve1603/AgentConnect.git
cd AgentConnect
chmod +x run.sh
./run.sh                 # builds, then creates .env and stops
nano .env                # add your API keys (pkg install nano)
./run.sh                 # starts the server
```

Then open **http://127.0.0.1:8000** in your phone's browser. If
`termux-open-url` is available (`pkg install termux-tools`) the launcher opens
it for you; otherwise the address is printed in the terminal.

Notes:

- `go build` on a phone is slow the first time — a few minutes is normal. The
  binary is cached afterwards, so later starts are instant.
- Check `go version` against the `go` directive in `go.mod`. Termux tracks Go
  closely, but if its package is older than the directive the build fails with
  an explicit message.
- Termux kills background processes when Android reclaims memory. Run
  `termux-wake-lock` first, or keep the Termux notification visible, if the
  server stops on its own.
- `Ctrl+C` stops the server. History lives in `data/roundtable.db` next to the
  binary.

## Linux and macOS setup

```bash
chmod +x run.sh
./run.sh                 # builds, then creates .env and stops
$EDITOR .env             # add your API keys
./run.sh                 # starts the server
```

Or manually:

```bash
cp .env.example .env      # then add your API keys
go build -o roundtable ./cmd/roundtable
./roundtable
```

To rebuild the UI after changing anything under `web/src`:

```bash
cd web && npm install && npm run build   # writes web/dist
cd .. && go build -o roundtable ./cmd/roundtable
```

`make` targets are provided for the same steps: `make web`, `make build`, `make run`, `make check`.

## Development

Run the Go server and the Vite dev server side by side for hot module reloading:

```bash
go run ./cmd/roundtable      # terminal 1 — API on :8000
cd web && npm run dev        # terminal 2 — UI on :5173, proxies /api to :8000
```

## Tests

```bash
go test ./...          # unit + HTTP integration tests
go test -race ./...    # same suite under the race detector
go vet ./...
cd web && npm run typecheck
```

Every test uses fake providers, so the suite makes no network calls and spends no API credits.

## Modes

| Mode                | Rounds                                    | Use when                                        |
| ------------------- | ----------------------------------------- | ----------------------------------------------- |
| **Full Roundtable** | independent → peer review → chair         | The answer matters more than latency or cost    |
| **Panel**           | independent → chair                       | You want cross-model synthesis without review   |
| **Direct**          | one model only (whichever chair you pick) | Quick questions, or debugging a single provider |

## Configuration

`.env` supports:

```dotenv
OPENAI_MODEL=gpt-5.6
ANTHROPIC_MODEL=claude-sonnet-5
GEMINI_MODEL=gemini-3.7-flash
APP_HOST=127.0.0.1
APP_PORT=8000
DATABASE_PATH=data/roundtable.db
LOG_LEVEL=info
OPEN_BROWSER=true
REQUEST_TIMEOUT_SECONDS=120
PROVIDER_RETRIES=2
MAX_PROMPT_CHARS=30000
MAX_OUTPUT_TOKENS=16000
HISTORY_MESSAGE_LIMIT=24
HISTORY_CHAR_LIMIT=60000
DEFAULT_CHAIR=openai
```

Real environment variables override `.env`. The web settings dialog can override model IDs per browser without changing `.env`.

## Failure behaviour

- A provider that errors is **isolated**: its answer is dropped and the turn continues with the rest.
- Transient failures (timeouts, 408, 429, 5xx) are retried with exponential backoff and jitter, up to `PROVIDER_RETRIES` times. Authentication and validation errors fail immediately.
- If the selected chair fails, another provider that answered successfully takes over, and the answer is labelled as a fallback.
- If every chair candidate fails, the strongest single first-round answer is returned rather than losing the turn.
- If every provider fails, the turn reports the error and nothing is written to history.

## HTTP API

| Method   | Path                        | Purpose                                    |
| -------- | --------------------------- | ------------------------------------------ |
| `GET`    | `/api/config`               | Providers, models, modes (never API keys)  |
| `GET`    | `/api/conversations`        | Conversation list                          |
| `GET`    | `/api/conversations/{id}`   | One conversation with messages and traces  |
| `DELETE` | `/api/conversations/{id}`   | Delete a conversation and its traces       |
| `POST`   | `/api/chat`                 | Start a turn; returns a `run_id`           |
| `GET`    | `/api/runs/{id}/events`     | Server-sent progress stream for that turn  |
| `GET`    | `/healthz`                  | Liveness check                             |

## Security notes

- API keys never leave the backend and are never returned by `/api/config`.
- The default server binds to `127.0.0.1`, so other devices cannot access it. Changing `APP_HOST` to a non-loopback address logs a warning at startup.
- Responses carry `Content-Security-Policy`, `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, `Permissions-Policy`, and `Cross-Origin-Opener-Policy` headers. The CSP allows no inline scripts or styles and no third-party origins.
- `.env` and the local database are excluded by `.gitignore` and should not be committed.

## Project structure

```text
ai-roundtable/
├── cmd/roundtable/
│   └── main.go                  entry point: config, storage, server, shutdown
├── internal/
│   ├── config/                  .env + environment loading
│   ├── orchestration/
│   │   ├── engine.go            three-phase turn orchestration
│   │   └── prompts.go           prompt construction for every phase
│   ├── providers/
│   │   ├── provider.go          interface, result type, retry loop
│   │   ├── factory.go           builds providers from config + overrides
│   │   ├── openai.go
│   │   ├── anthropic.go
│   │   └── gemini.go
│   ├── server/
│   │   ├── server.go            routes, SSE, security headers, static UI
│   │   └── runs.go              buffered run registry behind the SSE stream
│   └── store/                   SQLite conversations, messages, traces
├── web/
│   ├── embed.go                 go:embed of the built UI
│   ├── src/
│   │   ├── App.tsx              application state and layout
│   │   ├── api.ts               HTTP + SSE client and shared types
│   │   ├── components/          Sidebar, Transcript, RunStatus, Trace, …
│   │   └── styles.css
│   └── dist/                    build output, committed so the binary is self-contained
├── .env.example
├── Makefile
├── run.bat                      Windows launcher
├── run.ps1                      Windows (PowerShell) launcher
├── run.sh                       Linux / macOS / Android (Termux) launcher
├── CHANGELOG.md
└── README.md
```

## Troubleshooting

**"No provider API keys are configured."** `.env` is missing or its keys are blank. Fill it in and restart; the file is read at startup only.

**One provider always shows red.** Hover its dot in the header for the configured model. A `404` from a provider usually means the model ID in `.env` is not available to your account.

**`web UI is not built`.** The binary was compiled without `web/dist`. Run `cd web && npm install && npm run build`, then rebuild.

**The browser did not open.** The server logs the address on startup — open it yourself, or set `OPEN_BROWSER=false` to stop it trying. On Termux, install `termux-tools` for `termux-open-url`.

**Port already in use.** Change `APP_PORT` in `.env`.
