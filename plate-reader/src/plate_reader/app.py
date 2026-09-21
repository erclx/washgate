import logging

from fastapi import FastAPI, HTTPException, Request
from fastapi.concurrency import run_in_threadpool

from plate_reader.config import build_forwarder
from plate_reader.forwarder import Forwarder
from plate_reader.models import PlateRead
from plate_reader.reader import AlprReader, PlateReader

logger = logging.getLogger(__name__)

DEFAULT_MAX_IMAGE_BYTES = 10 * 1024 * 1024


async def read_capped_body(request: Request, max_bytes: int) -> bytes:
    body = bytearray()
    async for chunk in request.stream():
        body.extend(chunk)
        if len(body) > max_bytes:
            raise HTTPException(status_code=413, detail='image too large')
    return bytes(body)


def create_app(
    reader: PlateReader,
    forwarder: Forwarder,
    max_image_bytes: int = DEFAULT_MAX_IMAGE_BYTES,
) -> FastAPI:
    app = FastAPI(title='washgate plate reader')

    @app.post('/read')
    async def read_plate(request: Request) -> PlateRead:
        image_bytes = await read_capped_body(request, max_image_bytes)
        if not image_bytes:
            raise HTTPException(status_code=422, detail='no image in request body')
        plate_read = await run_in_threadpool(reader.read_plate, image_bytes)
        if plate_read is None:
            raise HTTPException(status_code=404, detail='no plate found in image')
        try:
            await run_in_threadpool(forwarder.forward, plate_read)
        except OSError:
            logger.warning('forward to site agent failed, read returned anyway')
        return plate_read

    return app


def create_default_app() -> FastAPI:
    return create_app(AlprReader(), build_forwarder())
