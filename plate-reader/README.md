# Plate reader

Turns a lane photo into plate text, a confidence score, and a bounding box, then sends the read on to the site agent. The image is decoded from memory and never written to disk or logged.

## Run

```bash
uv sync
uv run uvicorn --factory plate_reader.app:create_default_app --port 8000
```

The first read downloads the model weights, so it needs network once.

| Variable         | Default | Meaning                                                                                                                   |
| ---------------- | ------- | ------------------------------------------------------------------------------------------------------------------------- |
| `SITE_AGENT_URL` | unset   | Where each read is posted, path included, such as `http://site-agent:8081/reads`. Unset skips the send and logs one line. |

## Request

`POST /read` with the raw photo as the body and `Content-Type: image/jpeg`, up to 10 MiB. The body is read as a stream and decoded from memory, so nothing is spooled to disk.

```bash
curl -H 'Content-Type: image/jpeg' --data-binary @lane.jpg http://localhost:8000/read
```

## Response and send-on

The response and the send-on body are the same JSON:

```json
{
  "plate": "EEK828",
  "confidence": 0.97,
  "box": { "x1": 412, "y1": 388, "x2": 560, "y2": 430 }
}
```

`confidence` is the OCR confidence for the plate with the highest detection confidence in the photo, averaged over characters. The reader returns it raw and never decides admit, pay, or staff. That cutoff belongs to the site.

| Status | Meaning                            |
| ------ | ---------------------------------- |
| 200    | A plate was read                   |
| 404    | No plate found, nothing is sent on |
| 413    | The image exceeds the size limit   |
| 422    | The request body is empty          |

A send-on that fails after its retries is logged and does not change the response.
