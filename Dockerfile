# syntax=docker/dockerfile:1
FROM oven/bun:1.4.4-alpine AS assets
WORKDIR /src
COPY package.json bun.lock bunfig.toml ./
RUN --mount=type=cache,target=/root/.bun/install/cache \
  bun install --frozen-lockfile
COPY scripts/ scripts/
COPY web/ web/
COPY internal/web/views/ internal/web/views/
COPY internal/web/ui/ internal/web/ui/
RUN bun run build

FROM golang:1.27.2-alpine AS go-deps
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

FROM go-deps AS go-source
COPY . .
COPY --from=assets /src/internal/web/static/ internal/web/static/
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
  go tool shadcn-templ bundle

FROM go-source AS build
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
  go generate ./internal/web && \
  CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/bachtel ./cmd/server

# Proves the packaged converter works with no host dependency: the same pinned
# Pandoc as the runtime image, the built frontend assets, and converter/campaign
# tests plus the HTTP campaign authoring flow. The full race suite runs in
# make check. Not in the production chain - the runtime stage below stays the
# final stage. Build it with `make verify-converter`.
FROM go-source AS converter-test
RUN apk add --no-cache pandoc-cli=3.10-r0
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
  go test ./internal/convert ./internal/campaign && \
  go test ./internal/web -run '^TestAcceptanceCampaignAuthoringFlow$'

FROM alpine:3.24 AS runtime
# Pandoc is packaged because the app converts an operator-uploaded DOCX body
# to HTML inside the container; no runtime Node and no Word are involved.
# Pinned exactly: pandoc 3.10 (the "pandoc" name is provided by pandoc-cli).
RUN apk add --no-cache ca-certificates=20261020-r0 tzdata=2026e-r0 pandoc-cli=3.10-r0 && \
  addgroup -S -g 10001 app && adduser -S -D -H -u 10001 -G app app && \
  mkdir -p /app/pb_data && chown app:app /app/pb_data
WORKDIR /app
COPY --from=build /out/bachtel /usr/local/bin/bachtel
USER 10001:10001
EXPOSE 8090
# Minimal liveness only: the application health route never includes database,
# mailbox or task detail.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8090/health"]
ENTRYPOINT ["bachtel"]
CMD ["serve", "--http=0.0.0.0:8090", "--dir=/app/pb_data"]
