@echo off
setlocal enabledelayedexpansion
cd /d "%~dp0"

echo ==========================================
echo   AI Roundtable
echo ==========================================
echo.

REM --- Locate Go ------------------------------------------------------------
where go >nul 2>&1
if errorlevel 1 (
    echo ERROR: Go was not found on this system.
    echo Install Go 1.24 or newer from https://go.dev/dl/ and run this again.
    echo.
    pause
    exit /b 1
)

REM --- Build the binary on first run ----------------------------------------
REM The React UI is committed pre-built and embedded into the binary, so no
REM Node.js toolchain is needed to run the app.
if not exist "roundtable.exe" (
    echo Building AI Roundtable. This can take a minute on the first run...
    go build -o roundtable.exe ./cmd/roundtable
    if errorlevel 1 (
        echo ERROR: The build failed.
        pause
        exit /b 1
    )
)

REM --- Create .env on first run and stop for API keys ------------------------
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
roundtable.exe

echo.
echo Server stopped.
pause
