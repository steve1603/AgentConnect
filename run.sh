#!/bin/sh
# AI Roundtable launcher for Linux, macOS, and Android/Termux.
#
#   chmod +x run.sh
#   ./run.sh
#
# Windows users: use run.bat or run.ps1 instead.

set -eu
cd "$(dirname "$0")"

echo "=========================================="
echo "  AI Roundtable"
echo "=========================================="
echo

if ! command -v go >/dev/null 2>&1; then
    echo "ERROR: Go was not found."
    echo
    echo "  Termux:  pkg install golang"
    echo "  Debian:  sudo apt install golang"
    echo "  macOS:   brew install go"
    echo "  Or download it from https://go.dev/dl/"
    exit 1
fi

# The required Go version comes from the `go` directive in go.mod. Go reports a
# clear error itself if the toolchain is too old, so this is informational.
echo "Using $(go version)"
echo

# The React UI is committed pre-built and embedded into the binary, so no
# Node.js toolchain is needed to run the app.
if [ ! -x ./roundtable ]; then
    echo "Building AI Roundtable. This can take a few minutes on the first run..."
    go build -o roundtable ./cmd/roundtable
    echo
fi

if [ ! -f .env ]; then
    cp .env.example .env
    echo "A new .env file has been created."
    echo
    echo "  1. Open it and add your API keys:"
    echo "       nano .env"
    echo "     (Termux: pkg install nano, or use any editor you have)"
    echo "  2. Save it, then run this launcher again:"
    echo "       ./run.sh"
    echo
    exit 0
fi

# Warn about a .env that still has no key in it, rather than starting a server
# that cannot answer anything.
if ! grep -qE '^[A-Z]+_API_KEY=.+' .env; then
    echo "WARNING: no API keys are set in .env - the roundtable cannot answer yet."
    echo "         Add at least one key, then restart."
    echo
fi

PORT=$(grep -E '^APP_PORT=' .env 2>/dev/null | tail -1 | cut -d= -f2 || true)
echo "Starting AI Roundtable on http://127.0.0.1:${PORT:-8000}"
echo "Press Ctrl+C to stop."
echo

exec ./roundtable
