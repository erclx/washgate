import tempfile
from pathlib import Path

import pytest
from conftest import (
    FailingForwarder,
    FakeReader,
    RecordingForwarder,
    build_plate_read,
)
from fastapi.testclient import TestClient

from plate_reader.app import create_app

JPEG = {'Content-Type': 'image/jpeg'}
IMAGE = b'jpeg-bytes'


def build_client(
    reader: FakeReader,
    forwarder: RecordingForwarder | FailingForwarder | None = None,
    max_image_bytes: int = 10 * 1024 * 1024,
) -> TestClient:
    return TestClient(
        create_app(reader, forwarder or RecordingForwarder(), max_image_bytes)
    )


class TestPlateEndpoint:
    def test_returns_plate_confidence_and_box(self) -> None:
        client = build_client(FakeReader(build_plate_read()))

        response = client.post('/read', content=IMAGE, headers=JPEG)

        assert response.status_code == 200
        assert response.json() == {
            'plate': 'ABC123',
            'confidence': 0.98,
            'box': {'x1': 10, 'y1': 20, 'x2': 110, 'y2': 60},
        }

    def test_passes_the_uploaded_bytes_to_the_reader(self) -> None:
        reader = FakeReader(build_plate_read())
        client = build_client(reader)

        client.post('/read', content=IMAGE, headers=JPEG)

        assert reader.received == [IMAGE]

    def test_forwards_the_read_once(self) -> None:
        forwarder = RecordingForwarder()
        client = build_client(FakeReader(build_plate_read()), forwarder)

        client.post('/read', content=IMAGE, headers=JPEG)

        assert forwarder.forwarded == [build_plate_read()]

    def test_returns_404_when_no_plate_is_found(self) -> None:
        forwarder = RecordingForwarder()
        client = build_client(FakeReader(None), forwarder)

        response = client.post('/read', content=IMAGE, headers=JPEG)

        assert response.status_code == 404
        assert forwarder.forwarded == []

    def test_returns_422_when_the_body_is_empty(self) -> None:
        client = build_client(FakeReader(None))

        response = client.post('/read', content=b'', headers=JPEG)

        assert response.status_code == 422

    def test_returns_the_read_when_the_forward_fails(self) -> None:
        client = build_client(FakeReader(build_plate_read()), FailingForwarder())

        response = client.post('/read', content=IMAGE, headers=JPEG)

        assert response.status_code == 200
        assert response.json()['plate'] == 'ABC123'


class TestImageLimits:
    def test_returns_413_when_the_image_exceeds_the_size_limit(self) -> None:
        reader = FakeReader(build_plate_read())
        client = build_client(reader, max_image_bytes=4)

        response = client.post('/read', content=IMAGE, headers=JPEG)

        assert response.status_code == 413
        assert reader.received == []

    def test_leaves_no_file_on_disk_for_a_large_image(
        self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        monkeypatch.setattr(tempfile, 'tempdir', str(tmp_path))
        client = build_client(FakeReader(build_plate_read()))

        client.post('/read', content=b'x' * 2 * 1024 * 1024, headers=JPEG)

        assert list(tmp_path.iterdir()) == []
