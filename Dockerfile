FROM golang:1.22 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY ./cmd/seedbox_downloader ./cmd/seedbox_downloader
COPY ./internal ./internal

RUN CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="-s -w" -o seedbox_downloader ./cmd/seedbox_downloader/main.go

# Create /config in builder stage
RUN mkdir -p /config

FROM gcr.io/distroless/cc:nonroot

WORKDIR /app

# Copy /config from builder stage
COPY --from=builder /config /config

COPY --from=builder /app/seedbox_downloader .

ENTRYPOINT ["/app/seedbox_downloader"]
