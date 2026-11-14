#!/usr/bin/env python3
"""POSIX PTY integration test for a prebuilt CLI; synthetic loopback data only.
Usage: python3 build/test-cli-secret-input.py /absolute/path/to/supashare-cli
No production config, services or credentials are read.
"""
import base64
import errno
import fcntl
import http.server
import json
import os
import pathlib
import pty
import select
import signal
import subprocess
import sys
import tempfile
import termios
import threading
import time

API_KEY = b"synthetic-group-f-api-marker"
E2E_KEY = base64.b64encode(b"01234567890123456789012345678901")
# An empty E2E file list encrypted by the repository's EncryptData using the
# public synthetic key above; not a production secret or captured response.
E2E_DATA = {"version": 1, "nonce": "qdv7QDHyq4UGsv5V", "content": "gHvUQEl1kSAPZUUiN2w/bfLs6Jd/X7+G5sI0fzpQouce0fMwtzLZFhvLFlZgKorMVfgA7aF1NrUwFysUhwaGy8ydVUGQxG4lz5hlD9BUTLAe8axb11dRXyssutzeHv6W", "availablefiles": None}

class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def do_GET(self):
        if self.headers.get("apikey") != API_KEY.decode():
            self.send_error(401)
            return
        path = self.path.removeprefix("/e2e")
        if path == "/api/info/version":
            body = {"Version": "2.18.0", "VersionInt": 21800}
        elif path == "/api/info/config":
            body = {"MaxFilesize": 100, "MaxChunksize": 10,
                    "EndToEndEncryptionEnabled": self.path.startswith("/e2e/")}
        elif path in ("/api/e2e/mutex/lock", "/api/e2e/mutex/unlock"):
            body = {}
        elif path == "/api/e2e/get":
            body = E2E_DATA
        else:
            self.send_error(404)
            return
        content = json.dumps(body).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(content)))
        self.end_headers()
        self.wfile.write(content)

class Session:
    def __init__(self, binary, config):
        self.master, self.slave = pty.openpty()
        self.original = termios.tcgetattr(self.slave)
        self.output = b""
        def terminal_session():
            os.setsid()
            fcntl.ioctl(0, termios.TIOCSCTTY, 0)
        self.process = subprocess.Popen([binary, "login", "-c", str(config)],
            stdin=self.slave, stdout=self.slave, stderr=self.slave,
            preexec_fn=terminal_session)

    def pump(self, timeout=0.05):
        if select.select([self.master], [], [], timeout)[0]:
            try:
                self.output += os.read(self.master, 65536)
            except OSError as err:
                if err.errno != errno.EIO:
                    raise

    def until(self, marker):
        deadline = time.monotonic() + 10
        while marker not in self.output:
            self.pump()
            if self.process.poll() is not None or time.monotonic() > deadline:
                raise AssertionError("CLI did not reach expected prompt")

    def hidden(self):
        deadline = time.monotonic() + 5
        while termios.tcgetattr(self.slave)[3] & termios.ECHO:
            if time.monotonic() > deadline:
                raise AssertionError("terminal echo was not disabled")
            time.sleep(0.005)

    def write(self, value):
        os.write(self.master, value)

    def finish(self, success):
        self.process.wait(timeout=10)
        self.pump()
        assert (self.process.returncode == 0) == success, "unexpected login result"
        assert termios.tcgetattr(self.slave) == self.original, "terminal state not restored"
        for marker in (API_KEY, E2E_KEY, b"invalid-e2e-marker"):
            assert marker not in self.output, "secret appeared in terminal output"

    def close(self):
        if self.process.poll() is None:
            self.process.kill()
            self.process.wait()
        os.close(self.master)
        os.close(self.slave)


def main():
    binary = str(pathlib.Path(sys.argv[1]).resolve(strict=True))
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    origin = f"http://127.0.0.1:{server.server_port}"
    try:
        with tempfile.TemporaryDirectory(prefix="supashare-cli-secret-") as tmp:
            config = pathlib.Path(tmp) / "credentials.json"
            cases = ("api", "repeat", "e2e", "empty-api", "eof-api", "interrupt-api",
                     "terminate-api", "partial-interrupt-api", "empty-e2e", "invalid-e2e", "eof-e2e", "interrupt-e2e", "partial-interrupt-e2e", "terminate-e2e")
            for case in cases:
                old = config.read_bytes() if config.exists() else None
                is_e2e = "e2e" in case
                session = Session(binary, config)
                try:
                    session.until(b"SupaShare URL:")
                    session.write((origin + ("/e2e" if is_e2e else "") + "\n").encode())
                    session.until(b"API key (input hidden):")
                    session.hidden()
                    if case == "empty-api":
                        session.write(b"\n")
                    elif case == "eof-api":
                        session.write(b"\x04")
                    elif case == "interrupt-api":
                        session.write(b"\x03")
                    elif case == "partial-interrupt-api":
                        session.write(API_KEY + b"\x03")
                    elif case == "terminate-api":
                        session.process.send_signal(signal.SIGTERM)
                    else:
                        session.write(API_KEY + b"\r")
                        if is_e2e:
                            session.until(b"End-to-end encryption key (input hidden):")
                            session.hidden()
                            value = {"empty-e2e": b"\n", "invalid-e2e": b"invalid-e2e-marker\n",
                                     "eof-e2e": b"\x04", "interrupt-e2e": b"\x03",
                                     "partial-interrupt-e2e": E2E_KEY + b"\x03"}.get(case, E2E_KEY + b"\r")
                            if case == "terminate-e2e":
                                session.process.send_signal(signal.SIGTERM)
                            else:
                                session.write(value)
                    success = case in ("api", "repeat", "e2e")
                    session.finish(success)
                    if success:
                        saved = json.loads(config.read_text())
                        assert saved["Apikey"].encode() == API_KEY
                        if is_e2e:
                            assert saved["E2Ekey"].encode() == E2E_KEY
                        assert config.stat().st_mode & 0o777 == 0o600
                    else:
                        assert config.read_bytes() == old, "failed login changed prior credentials"
                    print("PASS", case)
                finally:
                    session.close()
            old = config.read_bytes()
            result = subprocess.run([binary, "login", "-c", str(config)],
                input=origin.encode() + b"\n" + API_KEY + b"\n", capture_output=True, timeout=10)
            assert result.returncode != 0
            assert b"interactive terminal" in result.stdout
            assert API_KEY not in result.stdout + result.stderr
            assert config.read_bytes() == old
            print("PASS redirected-stdin-rejected")
    finally:
        server.shutdown()
        server.server_close()

if __name__ == "__main__":
    main()
