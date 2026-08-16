@echo off
setlocal enabledelayedexpansion
cd /d "%~dp0"

echo ==========================================
echo   AI Roundtable
echo ==========================================
echo.

REM --- Locate Python -------------------------------------------------------
set "PY_CMD="
where py >nul 2>&1 && set "PY_CMD=py -3"
if not defined PY_CMD (
    where python >nul 2>&1 && set "PY_CMD=python"
)
if not defined PY_CMD (
    echo ERROR: Python was not found on this system.
    echo Install Python 3.11 or newer from https://www.python.org/downloads/
    echo and enable "Add Python to PATH" during installation.
    echo.
    pause
    exit /b 1
)

REM --- Create the virtual environment on first run -------------------------
if not exist ".venv\Scripts\python.exe" (
    echo Creating virtual environment...
    %PY_CMD% -m venv .venv
    if errorlevel 1 (
        echo ERROR: Could not create the virtual environment.
        pause
        exit /b 1
    )
    set "FIRST_RUN=1"
)

set "VENV_PY=.venv\Scripts\python.exe"

if defined FIRST_RUN (
    echo Installing dependencies. This can take a few minutes...
    "%VENV_PY%" -m pip install --upgrade pip
    "%VENV_PY%" -m pip install -r requirements.txt
    if errorlevel 1 (
        echo ERROR: Dependency installation failed.
        pause
        exit /b 1
    )
)

REM --- Create .env on first run and stop for API keys ----------------------
if not exist ".env" (
    copy /y ".env.example" ".env" >nul
    echo.
    echo A new .env file has been created and will now open in Notepad.
    echo.
    echo   1. Paste in your OPENAI_API_KEY, ANTHROPIC_API_KEY and GEMINI_API_KEY
    echo   2. Save the file and close Notepad
    echo   3. Run this launcher again
    echo.
    notepad .env
    pause
    exit /b 0
)

echo Starting AI Roundtable...
echo Your browser will open automatically. Press Ctrl+C here to stop the server.
echo.
"%VENV_PY%" -m app.start

echo.
echo Server stopped.
pause
