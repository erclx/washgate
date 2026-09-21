FROM python:3.14-slim
COPY --from=ghcr.io/astral-sh/uv:latest /uv /usr/local/bin/uv
ENV UV_COMPILE_BYTECODE=1 \
    UV_LINK_MODE=copy \
    PYTHONPATH=/app/src \
    PATH="/app/.venv/bin:$PATH"
WORKDIR /app
RUN useradd --create-home --uid 10001 reader && chown reader /app
USER reader
COPY --chown=reader pyproject.toml uv.lock README.md ./
RUN uv sync --frozen --no-dev
COPY --chown=reader src ./src
RUN python -c "from plate_reader.reader import AlprReader; AlprReader()"
EXPOSE 8000
CMD ["uvicorn", "--factory", "plate_reader.app:create_default_app", "--host", "0.0.0.0", "--port", "8000"]
