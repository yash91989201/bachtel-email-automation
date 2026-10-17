# Bachtel Email Automation

A Go application using templ, htmx, Alpine.js, Tailwind CSS, daisyUI, and
PocketBase. One executable serves the UI, static assets, PocketBase API, and
admin dashboard on one port. PocketBase runs in-process with SQLite; there is
no separate database service or frontend server.

This is the project foundation. Email workflows, domain collections, and user
authentication for the custom UI have not been implemented.

## Run locally

Prerequisites: Go 1.27.2+, Node.js 26+, npm, and Make.

```sh
make setup
make dev
```

Open <http://127.0.0.1:8090>. The starter page demonstrates an htmx HTML
fragment backed by a real database query and an Alpine disclosure panel.
PocketBase's dashboard is at <http://127.0.0.1:8090/_/> and its health endpoint
is <http://127.0.0.1:8090/api/health>.

On a fresh database, use the one-time installer link printed in the server
console to create your first superuser. No credentials are seeded.

`make dev` regenerates templates and assets, then starts the server. Stop and
rerun it after changes. It intentionally doesn't install an additional reload
daemon. Generated CSS and JavaScript are ignored by Git; `make setup` restores
them on a fresh checkout. Generated `*_templ.go` files are committed; edit the
`.templ` sources instead.

## Layout

```text
cmd/server/main.go              Application entrypoint and PocketBase setup
internal/
  migrations/                  Go migrations compiled into the executable
  web/
    web.go                     net/http handlers and embedded asset filesystem
    web_test.go                Routing, assets, migration, and DB integration check
    views/                     templ layouts, pages, and HTML fragments
    static/                    Embedded files and generated CSS/JS
web/css/app.css                Tailwind and daisyUI source
scripts/copy-assets.mjs         Copy pinned Alpine/htmx distributions locally
Dockerfile                     Frontend build, Go build, non-root runtime
compose.yaml                   One service and a persistent data volume
```

`cmd` and `internal` follow [Go's server layout guidance](https://go.dev/doc/modules/layout#server-project).
There is no universal Go folder layout for this entire stack. Keep app-specific
packages under `internal`, adding packages around real features as they appear.
Avoid a public `pkg` directory or repository/service interfaces until needed.
The existing OpenSpec configuration and skills are preserved.

## Development commands

```sh
make generate    # go generate -> module-pinned go tool templ
make assets      # copy Alpine/htmx and compile Tailwind/daisyUI
make fmt         # format Go and templ sources
make test        # regenerate, build assets, go test, go vet
make build       # build bin/bachtel with assets embedded
./bin/bachtel serve --http=127.0.0.1:8090 --dir=./pb_data
```

Node.js is build tooling only. The binary can be copied to another compatible
machine and run from any working directory; use an absolute `--dir` path to
keep the database location stable. The default data directory is `./pb_data`.
All frontend libraries are served locally, with no runtime CDN dependencies.

The templ CLI is pinned with Go's `tool` directive so its generator matches
the runtime library. Tailwind and its CLI are pinned to 4.3.0: npm audit flagged
the newer 4.3.3 CLI's pinned transitive watcher dependency during scaffolding.
The 4.3.0 CLI permits the patched watcher 2.6.0, recorded in the lockfile;
`npm audit` is clean. Recheck the audit when upgrading.

## Database migrations

The initial migration sets the app name and enables PocketBase's rate limiter.
Migrations run automatically before the server starts. Keep their filenames
stable after deployment.

```sh
go run ./cmd/server migrate create "create_contacts"
go run ./cmd/server migrate up --dir=./pb_data
```

New Go migration files go in `internal/migrations` and are already covered by
the entrypoint's blank import. During `go run`, dashboard collection changes
also generate Go migrations there. Review and commit those files, then rebuild
the binary/image to ship them. In compiled deployments, migration generation
is disabled; manage schema changes in source. Restart a running server after
manual migration commands. The initial settings migration has no rollback;
write explicit down functions for future reversible schema changes.

Custom handlers use `net/http`, adapted through PocketBase's official
`apis.WrapStdHandler`. PocketBase retains its `/api/` and `/_/` routes, server
lifecycle, and shutdown handling. The custom starter routes are public and
read-only. Before adding private or write operations, add authentication,
authorization, input validation, and CSRF protection to those routes. Server-side
PocketBase record access bypasses collection API rules, so custom handlers must
enforce their own access checks.

## Docker

```sh
docker compose up --build -d
docker compose logs -f app
```

Open <http://127.0.0.1:8090>. On first start, retrieve the installer link from
the logs and replace its `0.0.0.0` hostname with `127.0.0.1` if necessary.
The container runs as UID/GID 10001, has a health check, and stores databases,
uploads, backups, and local settings in the named `pb_data` volume at
`/app/pb_data`. Templates, assets, and migrations are already in the binary;
source folders are not mounted into the runtime container.

Copy `.env.example` to `.env` to override the published host or port.
These variables configure Compose only; the Go process uses PocketBase CLI
flags. The default host is loopback. For a remote host, place an HTTPS reverse
proxy in front and configure trusted proxy headers in PocketBase settings.
Set `APP_HOST=0.0.0.0` only when direct external access is intended.

```sh
docker compose down           # stop; retain data
docker compose up --build -d  # rebuild/recreate; retain data
```

Run one app instance against this local SQLite volume; do not share it between
replicas. Use PocketBase's dashboard backup facility before upgrades. For raw
volume copies, stop the app first. `docker compose down -v` deletes the database
volume. Configure SMTP through the dashboard when implementing email features;
this runtime image does not provide sendmail.

PocketBase is still pre-1.0: review its changelog and migration notes when
upgrading, and keep backups. See the [production guide](https://pocketbase.io/docs/going-to-production/).

## Convention references

Researched with Firecrawl against the official documentation:

- [Go module and server layout](https://go.dev/doc/modules/layout)
- [PocketBase as a Go application](https://pocketbase.io/docs/go-overview/)
- [PocketBase routing and standard handler adapters](https://pocketbase.io/docs/go-routing/)
- [PocketBase Go migrations](https://pocketbase.io/docs/go-migrations/)
- [templ generation](https://templ.guide/core-concepts/template-generation/)
- [daisyUI with htmx and Tailwind CLI](https://daisyui.com/docs/install/htmx/)
- [Alpine installation](https://alpinejs.dev/essentials/installation)
- [htmx installation and HTML responses](https://htmx.org/docs/)
