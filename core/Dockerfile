FROM golang:1.27-alpine AS build
ARG CMD=central
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/app ./cmd/${CMD} && mkdir /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /app
# A named volume mounted here starts as a copy of this folder, so it inherits the nonroot owner.
COPY --from=build --chown=nonroot:nonroot /out/data /data
ENTRYPOINT ["/app"]
