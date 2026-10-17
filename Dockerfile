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

FROM alpine:3.24 AS runtime
RUN apk add --no-cache ca-certificates tzdata && \
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
