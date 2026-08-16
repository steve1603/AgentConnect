# Changelog

All notable changes to AI Roundtable are recorded here.
This project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2026-08-16

Initial release: a Go backend and a React frontend, shipped as one
self-contained binary with the built UI embedded.

### Added

- Three-phase orchestration for every turn: parallel independent answers, a
  parallel peer-review round, and a chair synthesis round.
- Provider adapters for OpenAI (Responses API), Anthropic (Messages API, via
  the official Go SDK), and Google (Gemini Interactions API), behind one shared
  interface with a uniform result type.
- Automatic retry with exponential backoff and jitter for transient provider
  failures; authentication and validation errors fail fast without retrying.
- Provider-failure isolation: a vendor that errors is dropped from the turn and
  the remaining models carry it.
- Chair fallback: if the selected chair cannot synthesise, another provider that
  answered successfully takes over; if all of them fail, the strongest single
  first-round answer is returned instead of losing the turn.
- Full Roundtable, Panel, and Direct modes.
- Selectable synthesis chair and per-browser model overrides.
- Go HTTP server on the standard library, with a Server-Sent Event progress
  stream per turn, buffered so a late subscriber still receives the whole run.
- SQLite storage (pure-Go driver, no cgo) for conversations, messages, and the
  complete visible deliberation trace.
- React 19 + TypeScript UI: live status board, expandable "Roundtable
  deliberation" panel on every answer, conversation sidebar, and settings
  dialog. No third-party runtime dependencies beyond React itself.
- Browser security headers, including a Content-Security-Policy that permits no
  inline scripts or styles and no external origins.
- Windows `run.bat` and `run.ps1` launchers that build the binary and seed
  `.env` on first run, plus a `Makefile` for development.
- Test suite covering configuration loading, prompt construction, orchestration
  behaviour, provider retry and response parsing, SQLite storage, and the HTTP
  and SSE surface. Every test uses fake providers, so the suite makes no network
  calls and spends no API credits.

### Security

- API keys are read from `.env` on the server and are never included in
  `/api/config` or any other response.
- The server binds to `127.0.0.1` by default and warns at startup if `APP_HOST`
  is changed to a network-visible address.
- The application never requests or stores providers' hidden chain-of-thought;
  traces contain only normal visible model output.
