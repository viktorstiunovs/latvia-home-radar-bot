#!/usr/bin/env python3
import dataclasses
import http.server
import json
import logging
import os
import queue
import re
import signal
import threading
import time
from typing import Any

from playwright.sync_api import BrowserContext, Error as PlaywrightError
from playwright.sync_api import TimeoutError as PlaywrightTimeoutError
from playwright.sync_api import sync_playwright


LOGGER = logging.getLogger("domimaps-detail")
ADVERT_ID_PATTERN = re.compile(r"[0-9]{1,20}")
DETAIL_PATH = "/v1/domimaps/details"
DESCRIPTION_SELECTOR = "#addition-info"
MAX_REQUEST_BYTES = 4 << 10
MAX_DESCRIPTION_BYTES = 64 << 10


class DetailUnavailableError(Exception):
    pass


class BrowserBusyError(DetailUnavailableError):
    pass


@dataclasses.dataclass(frozen=True)
class Settings:
    bind: str = "0.0.0.0"
    port: int = 8080
    timeout_seconds: float = 20.0

    @classmethod
    def from_environment(cls) -> "Settings":
        bind = os.getenv("DOMIMAPS_DETAIL_BIND", cls.bind).strip()
        port = int(os.getenv("DOMIMAPS_DETAIL_PORT", str(cls.port)))
        timeout_seconds = float(
            os.getenv("DOMIMAPS_DETAIL_TIMEOUT_SECONDS", str(cls.timeout_seconds))
        )
        if not bind:
            raise ValueError("DOMIMAPS_DETAIL_BIND must not be empty")
        if port < 1 or port > 65535:
            raise ValueError("DOMIMAPS_DETAIL_PORT must be between 1 and 65535")
        if timeout_seconds < 5 or timeout_seconds > 20:
            raise ValueError(
                "DOMIMAPS_DETAIL_TIMEOUT_SECONDS must be between 5 and 20"
            )

        return cls(bind=bind, port=port, timeout_seconds=timeout_seconds)


@dataclasses.dataclass
class _Job:
    external_id: str
    result: queue.Queue


class BrowserWorker:
    def __init__(self, timeout_seconds: float) -> None:
        self._timeout_seconds = timeout_seconds
        self._jobs: queue.Queue = queue.Queue(maxsize=1)
        self._request_slot = threading.Lock()
        self._state_lock = threading.Lock()
        self._browser_ready = False
        self._closed = False
        self._thread = threading.Thread(
            target=self._run,
            name="domimaps-browser",
            daemon=True,
        )

    def start(self) -> None:
        self._thread.start()

    def healthy(self) -> bool:
        with self._state_lock:
            return self._thread.is_alive() and self._browser_ready and not self._closed

    def extract(self, external_id: str) -> str:
        if not self._request_slot.acquire(blocking=False):
            raise BrowserBusyError("browser is busy; retry later")
        try:
            with self._state_lock:
                if self._closed:
                    raise DetailUnavailableError("browser worker is shutting down")
            result: queue.Queue = queue.Queue(maxsize=1)
            self._jobs.put_nowait(_Job(external_id=external_id, result=result))
            try:
                value = result.get(timeout=self._timeout_seconds + 5)
            except queue.Empty as error:
                raise DetailUnavailableError("browser worker did not respond") from error
            if isinstance(value, Exception):
                raise value

            return value
        finally:
            self._request_slot.release()

    def close(self) -> None:
        with self._state_lock:
            self._closed = True
        try:
            self._jobs.put_nowait(None)
        except queue.Full:
            pass
        self._thread.join(timeout=self._timeout_seconds + 5)

    def _set_ready(self, ready: bool) -> None:
        with self._state_lock:
            self._browser_ready = ready

    def _run(self) -> None:
        with sync_playwright() as playwright:
            browser = None
            context = None
            try:
                browser, context = self._open_browser(playwright)
            except PlaywrightError:
                LOGGER.exception("could not start Chromium; requests will retry startup")

            while True:
                job = self._jobs.get()
                if job is None:
                    break
                try:
                    if context is None:
                        browser, context = self._open_browser(playwright)
                    description = extract_description(
                        context,
                        job.external_id,
                        int(self._timeout_seconds * 1000),
                    )
                    job.result.put_nowait(description)
                except DetailUnavailableError as error:
                    job.result.put_nowait(error)
                except PlaywrightError as error:
                    self._set_ready(False)
                    close_browser(context, browser)
                    browser = None
                    context = None
                    job.result.put_nowait(
                        DetailUnavailableError(f"browser extraction failed: {error}")
                    )
                except Exception as error:
                    LOGGER.exception("unexpected browser extraction failure")
                    job.result.put_nowait(
                        DetailUnavailableError(f"browser extraction failed: {error}")
                    )

            self._set_ready(False)
            close_browser(context, browser)

    def _open_browser(self, playwright: Any) -> tuple[Any, BrowserContext]:
        browser = playwright.chromium.launch(headless=True)
        context = browser.new_context(locale="lv-LV")
        self._set_ready(True)
        LOGGER.info("Chromium is ready")

        return browser, context


def close_browser(context: Any, browser: Any) -> None:
    if context is not None:
        try:
            context.close()
        except PlaywrightError:
            pass
    if browser is not None:
        try:
            browser.close()
        except PlaywrightError:
            pass


def extract_description(
    context: BrowserContext, external_id: str, timeout_ms: int
) -> str:
    expected_url = f"https://ad.domimaps.lv/{external_id}"
    deadline = time.monotonic() + timeout_ms / 1000
    page = context.new_page()
    try:
        page.goto(
            expected_url,
            wait_until="domcontentloaded",
            timeout=remaining_milliseconds(deadline),
        )
        description = page.locator(DESCRIPTION_SELECTOR)
        description.wait_for(
            state="attached",
            timeout=remaining_milliseconds(deadline),
        )
        if page.url.rstrip("/") != expected_url:
            raise DetailUnavailableError("advert page resolved to an unexpected URL")
        value = description.inner_text(timeout=remaining_milliseconds(deadline)).strip()
    except PlaywrightTimeoutError as error:
        raise DetailUnavailableError(
            "advert description did not appear before the timeout"
        ) from error
    finally:
        try:
            page.close()
        except PlaywrightError:
            pass

    if not value:
        raise DetailUnavailableError("advert description is empty")
    if len(value.encode("utf-8")) > MAX_DESCRIPTION_BYTES:
        raise DetailUnavailableError("advert description is too large")

    return value


def remaining_milliseconds(deadline: float) -> int:
    remaining = int((deadline - time.monotonic()) * 1000)
    if remaining < 1:
        raise DetailUnavailableError("advert extraction deadline expired")

    return remaining


def valid_advert_id(value: Any) -> bool:
    return isinstance(value, str) and ADVERT_ID_PATTERN.fullmatch(value) is not None


def process_detail_payload(payload: Any, worker: Any) -> tuple[int, dict[str, str]]:
    if not isinstance(payload, dict) or not valid_advert_id(payload.get("external_id")):
        LOGGER.warning(
            "description request rejected status=400 reason=invalid_external_id"
        )
        return 400, {"error": "external_id must contain 1 to 20 digits"}
    external_id = payload["external_id"]
    started_at = time.monotonic()
    try:
        description = worker.extract(external_id)
    except DetailUnavailableError as error:
        reason = safe_error_reason(error)
        LOGGER.warning(
            "description request completed external_id=%s status=503 "
            "duration_ms=%d outcome=retryable error=%s",
            external_id,
            elapsed_milliseconds(started_at),
            reason,
        )
        return 503, {"error": reason}

    LOGGER.info(
        "description request completed external_id=%s status=200 "
        "duration_ms=%d outcome=success description_bytes=%d",
        external_id,
        elapsed_milliseconds(started_at),
        len(description.encode("utf-8")),
    )

    return 200, {
        "external_id": external_id,
        "description": description,
    }


def elapsed_milliseconds(started_at: float) -> int:
    return max(0, round((time.monotonic() - started_at) * 1000))


def safe_error_reason(error: Exception) -> str:
    return " ".join(str(error).split())[:300]


class DetailHTTPServer(http.server.ThreadingHTTPServer):
    daemon_threads = True

    def __init__(self, address: tuple[str, int], worker: BrowserWorker) -> None:
        super().__init__(address, DetailHandler)
        self.worker = worker


class DetailHandler(http.server.BaseHTTPRequestHandler):
    server: DetailHTTPServer

    def do_GET(self) -> None:
        if self.path != "/healthz":
            self._send_json(404, {"error": "not found"})
            return
        if not self.server.worker.healthy():
            self._send_json(503, {"status": "starting"})
            return

        self._send_json(200, {"status": "ok"})

    def do_POST(self) -> None:
        if self.path != DETAIL_PATH:
            self._send_json(404, {"error": "not found"})
            return
        content_type = self.headers.get("Content-Type", "").split(";", 1)[0]
        if content_type.strip().lower() != "application/json":
            self._send_json(415, {"error": "Content-Type must be application/json"})
            return
        try:
            content_length = int(self.headers.get("Content-Length", ""))
        except ValueError:
            self._send_json(400, {"error": "valid Content-Length is required"})
            return
        if content_length < 1 or content_length > MAX_REQUEST_BYTES:
            self._send_json(413, {"error": "request body is too large or empty"})
            return
        try:
            payload = json.loads(self.rfile.read(content_length))
        except (json.JSONDecodeError, UnicodeDecodeError):
            self._send_json(400, {"error": "request body must be valid JSON"})
            return

        status, response = process_detail_payload(payload, self.server.worker)
        self._send_json(status, response)

    def log_message(self, message: str, *args: Any) -> None:
        LOGGER.debug("HTTP %s - %s", self.address_string(), message % args)

    def _send_json(self, status: int, payload: dict[str, str]) -> None:
        body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Cache-Control", "no-store")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def main() -> None:
    logging.basicConfig(
        level=os.getenv("LOG_LEVEL", "INFO").upper(),
        format="%(asctime)s %(levelname)s %(name)s %(message)s",
    )
    settings = Settings.from_environment()
    worker = BrowserWorker(settings.timeout_seconds)
    worker.start()
    server = DetailHTTPServer((settings.bind, settings.port), worker)

    def request_shutdown(_signum: int, _frame: Any) -> None:
        threading.Thread(target=server.shutdown, daemon=True).start()

    signal.signal(signal.SIGTERM, request_shutdown)
    signal.signal(signal.SIGINT, request_shutdown)
    LOGGER.info("listening on %s:%d", settings.bind, settings.port)
    try:
        server.serve_forever(poll_interval=0.5)
    finally:
        server.server_close()
        worker.close()


if __name__ == "__main__":
    main()
