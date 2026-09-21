from plate_reader.models import BoundingBox, PlateRead


def build_plate_read(
    plate: str = 'ABC123',
    confidence: float = 0.98,
    box: BoundingBox | None = None,
) -> PlateRead:
    return PlateRead(
        plate=plate,
        confidence=confidence,
        box=box or BoundingBox(x1=10, y1=20, x2=110, y2=60),
    )


class FakeReader:
    def __init__(self, read: PlateRead | None) -> None:
        self.read = read
        self.received: list[bytes] = []

    def read_plate(self, image_bytes: bytes) -> PlateRead | None:
        self.received.append(image_bytes)
        return self.read


class RecordingForwarder:
    def __init__(self) -> None:
        self.forwarded: list[tuple[PlateRead, bytes]] = []

    def forward(self, plate_read: PlateRead, image_bytes: bytes) -> None:
        self.forwarded.append((plate_read, image_bytes))


class FailingForwarder:
    def forward(self, plate_read: PlateRead, image_bytes: bytes) -> None:
        raise OSError('site agent down')
