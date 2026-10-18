# syntax=docker/dockerfile:1
FROM node:26.10.0-alpine AS assets
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY scripts/ scripts/
COPY web/ web/
COPY internal/web/views/ internal/web/views/
RUN npm run build

FROM golang:1.27.2-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=assets /src/internal/web/static/ internal/web/static/
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go generate ./internal/web && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/bachtel ./cmd/server

# Proves the packaged converter works with no host dependency: the same pinned
# Pandoc as the runtime image, the built frontend assets, and the full Go test
# suite (including the campaign authoring flow, which is skipped without a
# converter). Not in the production chain - the runtime stage below stays the
# final stage. Build it with `make verify-converter`.
FROM golang:1.27.2-alpine AS converter-test
RUN apk add --no-cache pandoc-cli=3.10-r0
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=assets /src/internal/web/static/ internal/web/static/
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go vet ./... && \
    go test ./...

FROM alpine:3.24 AS runtime
# Pandoc is packaged because the app converts an operator-uploaded DOCX body
# to HTML inside the container; no runtime Node and no Word are involved.
# Pinned exactly: pandoc 3.10 (the "pandoc" name is provided by pandoc-cli).
RUN apk add --no-cache ca-certificates tzdata pandoc-cli=3.10-r0 && \
    addgroup -S -g 10001 app && adduser -S -D -H -u 10001 -G app app && \
    mkdir -p /app/pb_data && chown app:app /app/pb_data
WORKDIR /app
COPY --from=build /out/bachtel /usr/local/bin/bachtel
USER app:app
EXPOSE 8090
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8090/api/health || exit 1
ENTRYPOINT ["bachtel"]
CMD ["serve", "--http=0.0.0.0:8090", "--dir=/app/pb_data"]
