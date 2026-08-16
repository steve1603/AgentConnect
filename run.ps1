# AI Roundtable launcher (PowerShell)
#
# If PowerShell blocks this script, run it once as:
#   powershell -ExecutionPolicy Bypass -File .\run.ps1

$ErrorActionPreference = "Stop"
Set-Location -Path $PSScriptRoot

Write-Host "=========================================="
Write-Host "  AI Roundtable"
Write-Host "=========================================="
Write-Host ""

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "ERROR: Go was not found." -ForegroundColor Red
    Write-Host "Install Go 1.24 or newer from https://go.dev/dl/ and run this again."
    Read-Host "Press Enter to exit"
    exit 1
}

# The React UI is committed pre-built and embedded into the binary, so no
# Node.js toolchain is needed to run the app.
$binary = Join-Path $PSScriptRoot "roundtable.exe"
if (-not (Test-Path $binary)) {
    Write-Host "Building AI Roundtable. This can take a minute on the first run..."
    go build -o roundtable.exe ./cmd/roundtable
    if ($LASTEXITCODE -ne 0) {
        Write-Host "ERROR: The build failed." -ForegroundColor Red
        Read-Host "Press Enter to exit"
        exit 1
    }
}

if (-not (Test-Path ".env")) {
    Copy-Item ".env.example" ".env"
    Write-Host ""
    Write-Host "A new .env file has been created and will now open in Notepad."
    Write-Host "  1. Paste in your OPENAI_API_KEY, ANTHROPIC_API_KEY and GEMINI_API_KEY"
    Write-Host "  2. Save the file and close Notepad"
    Write-Host "  3. Run this launcher again"
    Write-Host ""
    Start-Process notepad ".env" -Wait
    Read-Host "Press Enter to exit"
    exit 0
}

Write-Host "Starting AI Roundtable..."
Write-Host "Your browser will open automatically. Press Ctrl+C to stop the server."
Write-Host ""
& $binary
