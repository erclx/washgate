import json
import threading
from collections.abc import Iterator
from http.server import BaseHTTPRequestHandler, HTTPServer

import pytest
from conftest import build_plate_read

from plate_reader.forwarder import HttpForwarder, NoopForwarder


class SiteAgentStub:
    def __init__(self, statuses: list[int]) -> None:
        self.statuses = statuses
        self.bodies: list[dict[str, object]] = []
        stub = self

        class Handler(BaseHTTPRequestHandler):
            def do_POST(self) -> None:
                length = int(self.headers['Content-Length'])
                stub.bodies.append(json.loads(self.rfile.read(length)))
                status = stub.statuses[min(len(stub.bodies), len(stub.statuses)) - 1]
                self.send_response(status)
                self.end_headers()

            def log_message(self, format: str, *args: object) -> None:
                return

        self.server = HTTPServer(('127.0.0.1', 0), Handler)
        self.url = f'http://127.0.0.1:{self.server.server_port}/reads'
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)

    def __enter__(self) -> SiteAgentStub:
        self.thread.start()
        return self

    def __exit__(self, *exc: object) -> None:
        self.server.shutdown()
        self.server.server_close()


@pytest.fixture
def unused_url() -> Iterator[str]:
    server = HTTPServer(('127.0.0.1', 0), BaseHTTPRequestHandler)
    url = f'http://127.0.0.1:{server.server_port}/reads'
    server.server_close()
    yield url


class TestHttpForwarder:
    def test_posts_the_read_once_as_json(self) -> None:
        with SiteAgentStub([200]) as stub:
            HttpForwarder(stub.url, timeout_seconds=1).forward(build_plate_read())

        assert stub.bodies == [
            {
                'plate': 'ABC123',
                'confidence': 0.98,
                'box': {'x1': 10, 'y1': 20, 'x2': 110, 'y2': 60},
            }
        ]

    def test_retries_a_server_error_until_it_succeeds(self) -> None:
        with SiteAgentStub([503, 200]) as stub:
            HttpForwarder(stub.url, timeout_seconds=1, backoff_seconds=0).forward(
                build_plate_read()
            )

        assert len(stub.bodies) == 2

    def test_raises_after_the_bounded_attempts_are_spent(self) -> None:
        with SiteAgentStub([503]) as stub:
            forwarder = HttpForwarder(
                stub.url, timeout_seconds=1, attempts=3, backoff_seconds=0
            )
            with pytest.raises(OSError):
                forwarder.forward(build_plate_read())

        assert len(stub.bodies) == 3

    def test_raises_when_the_site_agent_is_unreachable(self, unused_url: str) -> None:
        forwarder = HttpForwarder(
            unused_url, timeout_seconds=1, attempts=2, backoff_seconds=0
        )

        with pytest.raises(OSError):
            forwarder.forward(build_plate_read())


class TestNoopForwarder:
    def test_forwards_nothing_without_raising(self) -> None:
        NoopForwarder().forward(build_plate_read())
