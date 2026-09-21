import logging
import time
import urllib.request
from typing import Protocol

from plate_reader.models import PlateRead

logger = logging.getLogger(__name__)


class Forwarder(Protocol):
    def forward(self, plate_read: PlateRead) -> None: ...


class NoopForwarder:
    def forward(self, plate_read: PlateRead) -> None:
        logger.info('site agent url not set, read not forwarded')


class HttpForwarder:
    def __init__(
        self,
        url: str,
        timeout_seconds: float = 2.0,
        attempts: int = 3,
        backoff_seconds: float = 0.2,
    ) -> None:
        self.url = url
        self.timeout_seconds = timeout_seconds
        self.attempts = attempts
        self.backoff_seconds = backoff_seconds

    def forward(self, plate_read: PlateRead) -> None:
        request = urllib.request.Request(
            self.url,
            data=plate_read.model_dump_json().encode(),
            headers={'Content-Type': 'application/json'},
            method='POST',
        )
        for attempt in range(1, self.attempts + 1):
            try:
                with urllib.request.urlopen(request, timeout=self.timeout_seconds):
                    return
            except OSError:
                if attempt == self.attempts:
                    raise
                time.sleep(self.backoff_seconds * attempt)
