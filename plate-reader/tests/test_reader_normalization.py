from types import SimpleNamespace

import cv2
import numpy as np

from plate_reader.reader import AlprReader


class FakeAlpr:
    def __init__(self, text: str) -> None:
        self.text = text

    def predict(self, image: object) -> list[SimpleNamespace]:
        detection = SimpleNamespace(
            confidence=0.9,
            bounding_box=SimpleNamespace(x1=1, y1=2, x2=3, y2=4),
        )
        ocr = SimpleNamespace(text=self.text, confidence=[0.5, 0.7])
        return [SimpleNamespace(detection=detection, ocr=ocr)]


def encode_blank_jpeg() -> bytes:
    _, encoded = cv2.imencode('.jpg', np.zeros((8, 8, 3), np.uint8))
    return encoded.tobytes()


class TestAlprReader:
    def test_returns_none_when_the_plate_text_is_empty(self) -> None:
        reader = AlprReader(alpr=FakeAlpr(' '))

        assert reader.read_plate(encode_blank_jpeg()) is None

    def test_normalizes_the_text_and_averages_the_confidence(self) -> None:
        reader = AlprReader(alpr=FakeAlpr('eek 828'))

        plate_read = reader.read_plate(encode_blank_jpeg())

        assert plate_read is not None
        assert plate_read.plate == 'EEK828'
        assert plate_read.confidence == 0.6
