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

function Resolve-Python {
    foreach ($candidate in @(@("py", "-3"), @("python"))) {
        $exe = $candidate[0]
        if (Get-Command $exe -ErrorAction SilentlyContinue) {
            return $candidate
        }
    }
    return $null
}

$python = Resolve-Python
if (-not $python) {
    Write-Host "ERROR: Python was not found." -ForegroundColor Red
    Write-Host "Install Python 3.11 or newer from https://www.python.org/downloads/"
    Read-Host "Press Enter to exit"
    exit 1
}

$venvPython = Join-Path $PSScriptRoot ".venv\Scripts\python.exe"
$firstRun = $false

if (-not (Test-Path $venvPython)) {
    Write-Host "Creating virtual environment..."
    & $python[0] @($python[1..($python.Length - 1)]) -m venv .venv
    $firstRun = $true
}

if ($firstRun) {
    Write-Host "Installing dependencies. This can take a few minutes..."
    & $venvPython -m pip install --upgrade pip
    & $venvPython -m pip install -r requirements.txt
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
& $venvPython -m app.start
