.PHONY: setup tools check-lint-version generate assets dev build test test-race fmt verify-converter

# Narrow these during implementation, e.g. make test PACKAGES=./internal/intake
# TEST_FLAGS accepts normal go test flags such as -run and -count=1.
PACKAGES ?= ./...
TEST_FLAGS ?=
GOLANGCI_LINT_VERSION := $(shell cat .golangci-lint-version)
TOOLS_DIR ?= $(shell go env GOPATH)/bin

setup: tools
	bun install --frozen-lockfile
	go mod download
	$(MAKE) generate assets

# Install a normal release binary so editors and assistants use it through PATH.
tools:
	@if [ "$$("$(TOOLS_DIR)/golangci-lint" version --short 2>/dev/null)" != "$(patsubst v%,%,$(GOLANGCI_LINT_VERSION))" ]; then \
		installer=$$(mktemp) || exit 1; \
		trap 'rm -f "$$installer"' EXIT; \
		curl --fail --silent --show-error --location https://golangci-lint.run/install.sh --output "$$installer" && \
		sh "$$installer" -b "$(TOOLS_DIR)" "$(GOLANGCI_LINT_VERSION)" || exit 1; \
	fi
	@$(MAKE) --no-print-directory check-lint-version

check-lint-version:
	@expected="$(patsubst v%,%,$(GOLANGCI_LINT_VERSION))"; \
	actual=$$(golangci-lint version --short) || { echo 'Install the pinned linter with make tools and put its directory on PATH.' >&2; exit 1; }; \
	if [ "$$actual" != "$$expected" ]; then \
		printf 'golangci-lint %s required; found %s at %s. Run make tools and check PATH.\n' "$$expected" "$$actual" "$$(command -v golangci-lint)" >&2; \
		exit 1; \
	fi

generate:
	go generate ./internal/web
assets:
	go tool shadcn-templ bundle
	bun run build

# Local dev reads .env (copy .env.example) into the server process. Shell
# syntax applies: no spaces around "=", no inline comments after a value.
dev:
	set -a; [ -f .env ] && . ./.env; set +a; go run github.com/air-verse/air@v1.67.4 -c .air.toml

build: generate assets
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/bachtel ./cmd/server

test: generate assets
	go test $(TEST_FLAGS) $(PACKAGES)

test-race: generate assets
	go test -race -timeout 20m $(TEST_FLAGS) $(PACKAGES)

# Builds only the converter-test Docker stage: campaign + convert tests
# against the same pinned Pandoc the runtime image packages. Needs a docker
# binary; no other target depends on it.
verify-converter:
	docker build --target converter-test -t bachtel-converter-test .

fmt:
	gofmt -w cmd internal
	go tool templ fmt internal/web

# Keep local verification and CI on the same commands. No .env is sourced.
.PHONY: check check-format check-generated lint lint-tailwind fix-tailwind vulncheck

lint-tailwind:
	bun run lint:tailwind

fix-tailwind:
	bun run fix:tailwind

check-format:
	bash scripts/check-format.sh

# Compare generated output with the index (staged source/output is supported).
check-generated: generate
	git diff --exit-code -- ':(glob)internal/web/**/*_templ.go'
	@test -z "$$(git ls-files --others --exclude-standard -- ':(glob)internal/web/**/*_templ.go')" || { echo 'Untracked generated templates: stage the generated output.' >&2; exit 1; }

lint: check-lint-version generate assets
	golangci-lint config verify
	golangci-lint run --config .golangci.yml ./...

# One dependency graph generates templates/assets once, including with make -j.
# The final gate always checks every package, regardless of focused test flags.
check: check-format check-generated lint lint-tailwind build
	bun run test:js
	go test -race -timeout 20m ./...

# Pin the analysis tool independently; do not change application dependencies.
vulncheck: generate assets
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
