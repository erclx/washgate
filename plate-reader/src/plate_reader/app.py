import logging
from typing import Annotated

from fastapi import FastAPI, File, HTTPException, UploadFile

from plate_reader.config import build_forwarder
from plate_reader.forwarder import Forwarder
from plate_reader.models import PlateRead
from plate_reader.reader import AlprReader, PlateReader

logger = logging.getLogger(__name__)

DEFAULT_MAX_IMAGE_BYTES = 10 * 1024 * 1024


def create_app(
    reader: PlateReader,
    forwarder: Forwarder,
    max_image_bytes: int = DEFAULT_MAX_IMAGE_BYTES,
) -> FastAPI:
    app = FastAPI(title='washgate plate reader')

    @app.post('/read')
    def read_plate(image: Annotated[UploadFile, File()]) -> PlateRead:
        image_bytes = image.file.read(max_image_bytes + 1)
        if len(image_bytes) > max_image_bytes:
            raise HTTPException(status_code=413, detail='image too large')
        plate_read = reader.read_plate(image_bytes)
        if plate_read is None:
            raise HTTPException(status_code=404, detail='no plate found in image')
        try:
            forwarder.forward(plate_read)
        except OSError:
            logger.warning('forward to site agent failed, read returned anyway')
        return plate_read

    return app


def create_default_app() -> FastAPI:
    return create_app(AlprReader(), build_forwarder())
