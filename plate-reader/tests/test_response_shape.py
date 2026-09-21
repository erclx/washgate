from conftest import (
    FailingForwarder,
    FakeReader,
    RecordingForwarder,
    build_plate_read,
)
from fastapi.testclient import TestClient

from plate_reader.app import create_app

PHOTO = {'image': ('lane.jpg', b'jpeg-bytes', 'image/jpeg')}


class TestPlateEndpoint:
    def test_returns_plate_confidence_and_box(self) -> None:
        client = TestClient(
            create_app(FakeReader(build_plate_read()), RecordingForwarder())
        )

        response = client.post('/read', files=PHOTO)

        assert response.status_code == 200
        assert response.json() == {
            'plate': 'ABC123',
            'confidence': 0.98,
            'box': {'x1': 10, 'y1': 20, 'x2': 110, 'y2': 60},
        }

    def test_passes_the_uploaded_bytes_to_the_reader(self) -> None:
        reader = FakeReader(build_plate_read())
        client = TestClient(create_app(reader, RecordingForwarder()))

        client.post('/read', files=PHOTO)

        assert reader.received == [b'jpeg-bytes']

    def test_returns_404_when_no_plate_is_found(self) -> None:
        forwarder = RecordingForwarder()
        client = TestClient(create_app(FakeReader(None), forwarder))

        response = client.post('/read', files=PHOTO)

        assert response.status_code == 404
        assert forwarder.forwarded == []

    def test_returns_422_when_no_image_is_sent(self) -> None:
        client = TestClient(create_app(FakeReader(None), RecordingForwarder()))

        response = client.post('/read')

        assert response.status_code == 422

    def test_returns_the_read_when_the_forward_fails(self) -> None:
        client = TestClient(
            create_app(FakeReader(build_plate_read()), FailingForwarder())
        )

        response = client.post('/read', files=PHOTO)

        assert response.status_code == 200
        assert response.json()['plate'] == 'ABC123'

    def test_returns_413_when_the_image_exceeds_the_size_limit(self) -> None:
        reader = FakeReader(build_plate_read())
        client = TestClient(create_app(reader, RecordingForwarder(), max_image_bytes=4))

        response = client.post('/read', files=PHOTO)

        assert response.status_code == 413
        assert reader.received == []
