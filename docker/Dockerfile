# syntax=docker/dockerfile:1

# Build stage: static binary, no CGO (SQLite driver is pure Go).
FROM golang:1.25-alpine AS build
WORKDIR /src
ARG VERSION=dev
COPY server/go.mod ./server/
COPY server/ ./server/
RUN cd server && CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags="-s -w -X github.com/caleblewallen/lewnotes/server/internal/version.Version=${VERSION}" \
      -o /out/lewnotes-core ./cmd/lewnotes-core
# Prepare a writable data directory for the nonroot user (distroless has no shell).
RUN mkdir -p /out/data && chown 65532:65532 /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/lewnotes-core /usr/local/bin/lewnotes-core
COPY --from=build --chown=nonroot:nonroot /out/data /data

ENV LEWNOTES_LISTEN=:8080 \
    LEWNOTES_VAULT=/data/vault \
    LEWNOTES_DATA_DIR=/data

EXPOSE 8080
VOLUME ["/data"]
USER nonroot:nonroot

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD ["/usr/local/bin/lewnotes-core", "-healthcheck", "-listen", ":8080"]

ENTRYPOINT ["/usr/local/bin/lewnotes-core"]
