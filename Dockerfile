# syntax=docker/dockerfile:1.6
FROM golang:1.24-alpine AS builder
RUN apk add --no-cache build-base pkgconfig libwebp-dev esbuild bash git
WORKDIR /src
COPY . .
ENV CGO_ENABLED=1 GOOS=linux
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    make build BIN_DIR=/out

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata libwebp
RUN addgroup -S app && adduser -S app -G app
RUN mkdir -p /data /app && chown -R app:app /data /app
COPY --from=builder /out/catwatch /app/catwatch
COPY --from=builder /out/catwatch_bot /app/catwatch_bot
COPY docker-entrypoint.sh /app/docker-entrypoint.sh
RUN chmod +x /app/docker-entrypoint.sh
HEALTHCHECK --interval=10s --timeout=2s --start-period=15s --retries=3 \
  CMD ["/app/catwatch", "healthz", "alive"]
USER app
EXPOSE 8080
WORKDIR /data
ENTRYPOINT ["/app/docker-entrypoint.sh"]
