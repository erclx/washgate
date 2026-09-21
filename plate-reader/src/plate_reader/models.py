from pydantic import BaseModel


class BoundingBox(BaseModel, frozen=True):
    x1: int
    y1: int
    x2: int
    y2: int


class PlateRead(BaseModel, frozen=True):
    plate: str
    confidence: float
    box: BoundingBox
