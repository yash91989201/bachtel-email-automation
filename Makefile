.PHONY: setup generate assets dev build test fmt verify-converter

setup:
	npm ci
	go mod download
	$(MAKE) generate assets

generate:
	go generate ./internal/web

assets:
	npm run build

# Local dev reads .env (copy .env.example) into the server process. Shell
# syntax applies: no spaces around "=", no inline comments after a value.
dev:
	set -a; [ -f .env ] && . ./.env; set +a; go run github.com/air-verse/air@v1.67.4 -c .air.toml

build: generate assets
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/bachtel ./cmd/server

test: generate assets
	go test ./...
	go vet ./...

# Builds only the converter-test Docker stage: campaign + convert tests
# against the same pinned Pandoc the runtime image packages. Needs a docker
# binary; no other target depends on it.
verify-converter:
	docker build --target converter-test -t bachtel-converter-test .

fmt:
	gofmt -w cmd internal
	go tool templ fmt internal/web/views
