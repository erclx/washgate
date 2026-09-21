from typing import Protocol

import cv2
import numpy as np
from fast_alpr import ALPR

from plate_reader.models import BoundingBox, PlateRead


class PlateReader(Protocol):
    def read_plate(self, image_bytes: bytes) -> PlateRead | None: ...


class AlprReader:
    def __init__(self, alpr: ALPR | None = None) -> None:
        self.alpr = alpr or ALPR(
            detector_model='yolo-v9-t-384-license-plate-end2end',
            ocr_model='cct-xs-v2-global-model',
        )

    def read_plate(self, image_bytes: bytes) -> PlateRead | None:
        image = cv2.imdecode(np.frombuffer(image_bytes, np.uint8), cv2.IMREAD_COLOR)
        if image is None:
            return None
        results = [result for result in self.alpr.predict(image) if result.ocr]
        if not results:
            return None
        best = max(results, key=lambda result: result.detection.confidence)
        assert best.ocr is not None
        plate = best.ocr.text.replace(' ', '').upper()
        if not plate:
            return None
        box = best.detection.bounding_box
        return PlateRead(
            plate=plate,
            confidence=_average(best.ocr.confidence),
            box=BoundingBox(x1=box.x1, y1=box.y1, x2=box.x2, y2=box.y2),
        )


def _average(confidence: float | list[float]) -> float:
    if isinstance(confidence, list):
        return sum(confidence) / len(confidence) if confidence else 0.0
    return float(confidence)
