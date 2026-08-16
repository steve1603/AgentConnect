# AI Roundtable

**Version 1.0.0**

A single local chat application where OpenAI/ChatGPT, Anthropic/Claude, and Google/Gemini collaborate on each user turn.

## How one turn works

1. **Independent answers**: every configured model receives the canonical conversation history and current request in parallel.
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

## Features

- One responsive dark-mode web chat
- ChatGPT + Claude + Gemini in one orchestration pipeline
- Parallel first-pass requests for lower latency
- Parallel peer-review round
- Selectable synthesis chair
- Full Roundtable, Panel, and Direct modes
- Per-provider model overrides
- Persistent SQLite conversation history
- Expandable deliberation trace on every answer
- Server-Sent Event progress updates
- Automatic retry with exponential backoff
- Chair fallback when the selected synthesis provider fails
- API keys kept server-side in `.env`
- Browser security headers and no third-party frontend dependencies
- Windows `.bat` and PowerShell launchers
- Unit tests for prompt construction and orchestration behavior

## Provider interfaces

| Provider  | Interface                                          | Default model       |
| --------- | -------------------------------------------------- | ------------------- |
| OpenAI    | Responses API (`POST /v1/responses`)                 | `gpt-5.6`           |
| Anthropic | Messages API (`POST /v1/messages`, official SDK)     | `claude-sonnet-5`   |
| Google    | Gemini Interactions API (`POST /v1beta/interactions`) | `gemini-3.7-flash`  |

Each provider is a small adapter behind a shared interface, so swapping a model
ID or adding a fourth vendor touches one file in `app/providers/`.

## Windows setup

### 1. Install Python

Install **Python 3.11 or newer** from python.org. During installation, enable the option to add Python to PATH if offered.

### 2. Extract the project

Extract the project to a permanent folder, for example:

```text
C:\Users\<your-user>\Applications\ai-roundtable
```

### 3. Run the launcher

Double-click:

```text
run.bat
```

On the first run it will:

- create `.venv`
- install dependencies
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

## Manual setup

```powershell
py -3.11 -m venv .venv
.\.venv\Scripts\Activate.ps1
python -m pip install --upgrade pip
pip install -r requirements.txt
Copy-Item .env.example .env
notepad .env
python -m app.start
```

On macOS or Linux the equivalent is:

```bash
python3 -m venv .venv
source .venv/bin/activate
python -m pip install --upgrade pip
pip install -r requirements.txt
cp .env.example .env
python -m app.start
```

## Tests

With the virtual environment active:

```powershell
pytest -q
```

The orchestration tests use fake providers and do not spend API credits.

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
DATABASE_URL=sqlite+aiosqlite:///./data/roundtable.db
REQUEST_TIMEOUT_SECONDS=120
PROVIDER_RETRIES=2
MAX_PROMPT_CHARS=30000
MAX_OUTPUT_TOKENS=16000
HISTORY_MESSAGE_LIMIT=24
HISTORY_CHAR_LIMIT=60000
DEFAULT_CHAIR=openai
OPEN_BROWSER=true
LOG_LEVEL=INFO
```

The web settings dialog can override model IDs per browser without changing `.env`.

## Failure behaviour

- A provider that errors is **isolated**: its answer is dropped and the turn continues with the rest.
- Transient failures (timeouts, 429, 5xx) are retried with exponential backoff up to `PROVIDER_RETRIES` times. Authentication and validation errors fail immediately.
- If the selected chair fails, another provider that answered successfully takes over, and the answer is labelled as a fallback.
- If every chair candidate fails, the strongest single first-round answer is returned rather than losing the turn.
- If every provider fails, the turn reports the error and nothing is written to history.

## HTTP API

| Method   | Path                             | Purpose                                       |
| -------- | -------------------------------- | --------------------------------------------- |
| `GET`    | `/api/config`                    | Providers, models, modes (never API keys)     |
| `GET`    | `/api/conversations`             | Conversation list                             |
| `GET`    | `/api/conversations/{id}`        | One conversation with messages and traces     |
| `DELETE` | `/api/conversations/{id}`        | Delete a conversation and its traces          |
| `POST`   | `/api/chat`                      | Start a turn; returns a `run_id`              |
| `GET`    | `/api/runs/{run_id}/events`      | Server-sent progress stream for that turn     |
| `GET`    | `/healthz`                       | Liveness check                                |

## Security notes

- API keys never leave the backend and are never returned by `/api/config`.
- The default server binds to `127.0.0.1`, so other devices cannot access it.
- Do not change `APP_HOST` to `0.0.0.0` unless you intentionally want LAN access and understand the network exposure.
- Responses carry `Content-Security-Policy`, `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, and `Permissions-Policy` headers. The CSP allows no inline scripts or styles and no third-party origins.
- `.env`, the local database, and the Python virtual environment are excluded by `.gitignore` and should not be committed.

## Project structure

```text
ai-roundtable/
├── app/
│   ├── main.py                 FastAPI app, routes, SSE, security headers
│   ├── start.py                uvicorn launcher, opens the browser
│   ├── config.py               settings loaded from .env
│   ├── db.py                   async SQLite engine and sessions
│   ├── models.py               Conversation and Message tables
│   ├── schemas.py              request/response models
│   ├── services.py             conversation storage and the SSE run registry
│   ├── orchestration/
│   │   ├── engine.py           three-phase turn orchestration
│   │   └── prompts.py          prompt construction for every phase
│   ├── providers/
│   │   ├── base.py             provider interface, retries, result type
│   │   ├── factory.py          builds providers from settings + overrides
│   │   ├── openai_provider.py
│   │   ├── anthropic_provider.py
│   │   └── gemini_provider.py
│   └── static/
│       ├── index.html
│       ├── styles.css
│       └── app.js
├── tests/
│   ├── conftest.py             fake providers (no network, no credits)
│   ├── test_engine.py
│   └── test_prompts.py
├── .env.example
├── .gitignore
├── pytest.ini
├── requirements.txt
├── run.bat
├── run.ps1
├── CHANGELOG.md
└── README.md
```

## Troubleshooting

**"No provider API keys are configured."** `.env` is missing or its keys are blank. Fill it in and restart; the file is read at startup only.

**One provider always shows red.** Hover its dot in the header for the configured model. A `404` from a provider usually means the model ID in `.env` is not available to your account.

**The browser did not open.** Set `OPEN_BROWSER=false` and open `http://127.0.0.1:8000` yourself; the server logs the address on startup.

**Port already in use.** Change `APP_PORT` in `.env`.
