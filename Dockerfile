FROM golang:1.22 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY ./cmd/seedbox_downloader ./cmd/seedbox_downloader
COPY ./internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o seedbox_downloader ./cmd/seedbox_downloader/main.go

FROM gcr.io/distroless/base:nonroot

WORKDIR /app

COPY --from=builder /app/seedbox_downloader .

ENTRYPOINT ["/app/seedbox_downloader"]
