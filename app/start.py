"""Entry point: `python -m app.start`.

Starts uvicorn on the configured host/port and opens a browser once the
server is actually accepting connections.
"""

from __future__ import annotations

import socket
import threading
import time
import webbrowser

import uvicorn

from .config import get_settings


def _wait_and_open(host: str, port: int, timeout: float = 30.0) -> None:
    deadline = time.monotonic() + timeout
    target = "127.0.0.1" if host in ("0.0.0.0", "::") else host
    while time.monotonic() < deadline:
        try:
            with socket.create_connection((target, port), timeout=0.5):
                webbrowser.open(f"http://{target}:{port}")
                return
        except OSError:
            time.sleep(0.3)


def main() -> None:
    settings = get_settings()

    if settings.app_host not in ("127.0.0.1", "localhost", "::1"):
        print(
            f"WARNING: APP_HOST is {settings.app_host!r}. The server will be reachable "
            "from other machines on your network. Use 127.0.0.1 unless that is intended."
        )

    if settings.open_browser:
        threading.Thread(
            target=_wait_and_open,
            args=(settings.app_host, settings.app_port),
            daemon=True,
        ).start()

    uvicorn.run(
        "app.main:app",
        host=settings.app_host,
        port=settings.app_port,
        log_level=settings.log_level.lower(),
    )


if __name__ == "__main__":
    main()
