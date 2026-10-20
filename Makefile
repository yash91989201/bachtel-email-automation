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
	go tool templ fmt internal/web

# Keep local verification and CI on the same commands. No .env is sourced.
.PHONY: check check-format check-generated lint vulncheck

check-format:
	bash scripts/check-format.sh

# Compare generated output with the index (staged source/output is supported).
check-generated: generate
	git diff --exit-code -- ':(glob)internal/web/**/*_templ.go'
	@test -z "$$(git ls-files --others --exclude-standard -- ':(glob)internal/web/**/*_templ.go')" || { echo 'Untracked generated templates: stage the generated output.' >&2; exit 1; }

lint: generate assets
	bash scripts/golangci-lint.sh config verify
	bash scripts/golangci-lint.sh run --config .golangci.yml ./...

check:
	$(MAKE) check-format
	$(MAKE) check-generated lint
	go test -race ./...
	$(MAKE) build

# Pin the analysis tool independently; do not change application dependencies.
vulncheck: generate assets
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
