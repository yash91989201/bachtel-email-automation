.PHONY: setup generate assets dev build test fmt

setup:
	npm ci
	go mod download
	$(MAKE) generate assets

generate:
	go generate ./internal/web

assets:
	npm run build

dev: generate assets
	go run ./cmd/server serve --http=127.0.0.1:8090 --dir=./pb_data

build: generate assets
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/bachtel ./cmd/server

test: generate assets
	go test ./...
	go vet ./...

fmt:
	gofmt -w cmd internal
	go tool templ fmt internal/web/views
