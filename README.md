# Bachtel Email Automation

A Go application using templ, htmx, Alpine.js, Tailwind CSS, locally owned shadcn-templ components, and
PocketBase. One executable serves the UI, static assets, PocketBase API, and
admin dashboard on one port. PocketBase runs in-process with SQLite; there is
no separate database service or frontend server.

The application includes operator sign-in, a Microsoft connection for the fixed
HR mailbox, campaign templates, Excel intake, and durable background sending
and recovery controls. Candidate sending remains gated in the production
entrypoint until change 5 completes export and release verification. Explicit
diagnostic test emails are available through the connected mailbox.

## Run locally

Prerequisites: Go 1.27.2+, Node.js 26+, npm, and Make.

```sh
make setup
make dev
```

Open <http://127.0.0.1:8090>. The starter page demonstrates an htmx HTML
fragment backed by a real database query and an Alpine disclosure panel.

Health endpoints:

- `GET /health` — public, minimal JSON `{"status":"ok"}` for platform probes;
  it exposes no application state.
- `GET /api/health` — PocketBase's own public health endpoint.

The application UI requires the operator account: provision it (see below),
then sign in at <http://127.0.0.1:8090/login>. Anonymous visitors can only
reach the sign-in page, `/health`, and the public static assets. The
PocketBase dashboard at <http://127.0.0.1:8090/_/> is a separate
administrative surface that requires a superuser.

On a fresh database, use the one-time installer link printed in the server
console to create your first superuser. No credentials are seeded.

`make dev` uses [Air](https://github.com/air-verse/air) to regenerate templates
and assets, rebuild, and restart the server when Go, `.templ`, CSS, or
JavaScript sources change. The pinned Air version downloads automatically on
the first run; no global installation is required. It also
sources a local `.env` into the server process when one exists: copy
`.env.example` to `.env` and fill it in instead of exporting variables by
hand. Restart `make dev` after changing `.env` or `.air.toml`. Refresh the
browser after a rebuild to see UI changes. Generated CSS and JavaScript are ignored by Git;
`make setup` restores them on a fresh checkout. Generated `*_templ.go` files
are committed; edit the `.templ` sources instead.

## Routes and access

| Route | Access |
|---|---|
| `GET /health` | Public, minimal JSON `{"status":"ok"}` for platform probes |
| `GET /login`, `POST /login` | Public sign-in form; login attempts are rate limited |
| `POST /logout` | Operator session |
| `GET /` | Operator session (dashboard) |
| `GET /status` | Operator session (HTML fragment) |
| `GET /campaigns`, `POST /campaigns`, `POST /campaigns/{id}/…` | Operator session; campaign authoring (create, revision upload, labels, sample preview) |
| `GET /campaigns/{id}/tasks…`, `POST /campaigns/{id}/tasks/{task}/…` | Operator session; task intake (upload, validation, review, duplicate and exclusion downloads) |
| `GET /connection`, `POST /connection/connect`, `GET /connection/callback`, `POST /connection/disconnect` | Operator session (the callback must match an in-flight authorization attempt) |
| `GET /static/`, `GET /robots.txt` | Public assets; `robots.txt` disallows everything |
| `GET /api/health` | Public PocketBase health probe |
| other `/api/*` | PocketBase API; the application's private collections are locked down (see below) |
| `/_/` | PocketBase admin dashboard (superuser only) |

Custom UI routes and the private collections are unreachable anonymously.

## Operator provisioning

Provision the single operator account once, using the same `--dir` as the
server so it writes to the same database:

```sh
./bin/bachtel provision-operator --email operator@example.com --dir=./pb_data
# during development:
go run ./cmd/server provision-operator --email operator@example.com --dir=./pb_data
```

The password is read from the terminal without echo, or piped on stdin:

```sh
echo "$PASSWORD" | ./bin/bachtel provision-operator --email operator@example.com --dir=./pb_data
```

No credentials are seeded anywhere. A second invocation fails with
`an operator is already provisioned`.

## Administrator versus operator access

Two distinct roles:

- **Administrator** — the PocketBase superuser, created from the one-time
  installer link printed in the console on a fresh database. It owns the
  admin dashboard (`/_/`), migrations, settings, and backups. This is an
  administrative role, not a day-to-day account.
- **Operator** — the application-level account created by
  `provision-operator`. All application UI is used through this account.

The `operators` collection is unreachable through the public PocketBase API:
its record rules and auth rule are locked to administrators only, so
anonymous visitors cannot list, read, or create operators. Sign-in therefore
goes through `/login`, which adds rate limiting and cookie protection on top
of PocketBase's password verification.

## Configuration

All application settings are read from the environment (documented with
placeholders in `.env.example`; Compose maps them into the container, and
`make dev` sources a local `.env`).
Microsoft settings may be absent at startup: the server starts anyway and the
connection reports an actionable configuration error instead.

| Variable | Purpose |
|---|---|
| `APP_ORIGIN` | Public `https` origin, no trailing slash (e.g. `https://mail.example.com`). Drives cookie security, trusted-origin checks, and the default callback URL. |
| `APP_ENCRYPTION_KEY` | **Secret.** Base64 encoding of exactly 32 bytes; encrypts stored Microsoft credentials at rest. Generate with `openssl rand -base64 32`. |
| `MS_TENANT_ID` | Microsoft Entra tenant (directory) id. |
| `MS_CLIENT_ID` | Application (client) id from the app registration. |
| `MS_CLIENT_SECRET` | **Secret.** Client secret from the app registration. |
| `MS_REDIRECT_URL` | Redirect URI sent to Microsoft. Defaults to `APP_ORIGIN` + `/connection/callback` and must match the app registration exactly. |
| `MS_SENDER_ADDRESS` | The single mailbox allowed to connect. Defaults to `hr@bachtelcorporation.com`. |
| `MS_AUTHORITY_URL`, `MS_GRAPH_BASE_URL` | Advanced overrides for simulated authorization/Graph servers; leave unset in production. |
| `INTAKE_MAX_UPLOAD_BYTES` | Upload limit for one task workbook, in bytes. Defaults to 10 MiB, which is also the supported ceiling. |
| `INTAKE_UNZIP_SIZE_LIMIT`, `INTAKE_UNZIP_XML_SIZE_LIMIT` | Expanded workbook archive and single-part limits, in bytes. Default to 64 MiB and 16 MiB. |

`APP_ENCRYPTION_KEY` is separate from PocketBase's own `--encryptionEnv`
settings key and separate from `MS_CLIENT_SECRET`; each protects different
material. Back it up independently of the database: losing it leaves the
stored ciphertext intact but requires reconnecting the mailbox. A missing or
wrong key does not crash the server — the connection reports an actionable
error and no email can be sent.

See [docs/microsoft-setup.md](docs/microsoft-setup.md) for the full first-time
Microsoft setup.

## DOCX converter

Campaign bodies are uploaded as DOCX files and converted to HTML with Pandoc
at authoring time.

- **Deployment:** the application image packages a pinned Pandoc, so a
  deployed container needs no extra setup.
- **Local development:** install Pandoc 3.x so it is on `PATH`
  (Arch: `sudo pacman -S pandoc`; Debian/Ubuntu: `sudo apt install pandoc`),
  or set `PANDOC_PATH` to a Pandoc executable. When no converter is
  available, authoring reports an actionable error instead of failing
  silently.
- Supported document features and conversion limits are documented in
  [docs/campaign-templates.md](docs/campaign-templates.md).
- `make verify-converter` builds the Dockerfile's `converter-test` stage and
  runs the converter tests against the same pinned Pandoc the runtime image
  packages. It is the one target that needs a `docker` binary; no other
  target depends on Docker.

## Task intake

Each email task starts with an uploaded `.xlsx` workbook. Uploading stores the
original bytes privately and creates a **staging** task; the operator then
chooses the worksheet and header row, and validation either publishes an
immutable snapshot of the eligible rows or fails with an actionable reason.

- Validation runs before any candidate exists: every mapped source header,
  including the revision's explicit `recipient` row, must exist exactly once.
  The recipient supplies the sending address without needing a DOCX placeholder.
  Duplicate recipient keys hold
  every row of their group, and unusable addresses are excluded. Excluded rows
  live only in the stored workbook, a private validation manifest and a
  downloadable exclusion report — never as candidate records.
- Candidates are identified by campaign plus trimmed lowercase email address,
  with generated internal ids, so a corrected upload creates a new task over the
  same identities and an earlier task keeps its snapshot.
- A validated task is **ready**, not running. Its sending page provides explicit
  test, received-format approval, start, stop and recovery actions. Candidate
  actions recheck and claim eligibility transactionally; production candidate
  dispatch remains gated pending change 5.
- The upload screen shows the workbook constraints (supported format, size,
  expanded-archive limits and the 1,000-row task bound) and reports every
  rejection inline.

Workbook support, header matching, recipient-key rules, resource limits,
duplicate handling and the private artifacts are documented in
[docs/task-intake.md](docs/task-intake.md).

## History and downloads

Completed, stopped and recovered tasks publish a results workbook built from
the retained original upload. Campaign pages publish an Excel report and a ZIP
package containing report, per-attempt HTML and an index. Generated files are
private, stored until explicit manual deletion, and regenerable from retained
records. See [docs/history-exports.md](docs/history-exports.md) for workbook
statuses, package contents and the two deletion scopes.

## Background sending and recovery

Open a validated task and choose **Sending and recovery**. Send a diagnostic
test to an explicitly entered test address, verify the received email, and
approve that exact content hash. A successful Graph response alone does not
approve a campaign. Starting or resuming a task requires a fresh recipient
count and confirmation; browser closure and logout do not stop accepted work.

All sends share a default rate of 25 starts per minute (integer settings 1–30),
four in-flight requests, persisted pacing and provider cooldowns. **SUBMITTED**
means Microsoft accepted the request, not that it was delivered. Uncertain
attempts are held for review and never automatically resent. Stop drains
requests already started; restart requires explicit resume of safe work.

A validated task also accepts a one-time scheduled start at least 30 minutes
ahead, explicit start authorization that fires from durable state across
restarts. It starts only the candidates free at that instant; a window that
passes while the server is down pauses the task with a missed-schedule reason
and no attempt, and resume or reschedule stays explicit.

See [docs/background-sending.md](docs/background-sending.md) for the states,
approval workflow, scheduling, retry and review actions, shutdown behavior and
release gate.

## Layout

```text
cmd/server/main.go              Application entrypoint and PocketBase setup
cmd/server/provision.go         One-time operator provisioning command
internal/
  config/                       Environment configuration (origin, Microsoft settings, encryption key)
  campaign/                     Campaign revisions, mapping validation, normalization and the single render path
  convert/                      Sandboxed DOCX-to-HTML conversion (pinned Pandoc) and document inspection
  intake/                       Task workbook validation, candidate identity, membership snapshots and reservations
  microsoft/                    Microsoft connection service (OAuth flow, encrypted credentials)
  operator/                     Single-operator provisioning
  migrations/
    1792233000_initial_settings.go      App name and PocketBase rate limiter
    1792233010_operator_collection.go   Single-operator auth collection with locked rules
    1792233020_microsoft_connection.go  Microsoft connection metadata collection
    1792233040_campaign_collections.go  Campaigns and immutable configuration revisions
    1792233050_intake_collections.go    Tasks, campaign candidate identity and membership snapshots
  web/
    web.go                      Route table, handler assembly, render helper
                                (/health, /robots.txt, /static, GET /{$}, GET /status)
    session.go                  Operator session cookie creation/validation and authorization middleware
    login.go                    GET /login, POST /login, POST /logout
    connection.go               GET /connection, connect, callback, and disconnect handlers
    campaign.go                 /campaigns pages: create, revision upload, labels, sample preview
    task.go                     Task intake pages: upload, worksheet selection, review, downloads
    ratelimit.go                Fixed-window login rate limiter
    web_test.go                 Route, auth, CSRF, rate-limit, and DB integration tests
    views/                      Page view models, layouts and templates
    ui/                         Locally owned shadcn-templ components and class utilities
    static/                     Embedded files and generated CSS/JS
docs/campaign-templates.md      Placeholder syntax, mapping/formatting rules, DOCX support and limits
docs/task-intake.md             Task workbook support, header matching, duplicate handling and limits
docs/microsoft-setup.md         Microsoft app registration and first-time connection setup
web/css/app.css                 Tailwind entry and application layout styles
web/css/theme.css               Bachtel theme tokens
web/css/components.css          Bachtel styling for owned UI primitives
scripts/copy-assets.mjs         Copy pinned Alpine/htmx distributions locally
Dockerfile                     Frontend build, Go build, non-root runtime
compose.yaml                   One service, application environment, persistent data volume
```

`cmd` and `internal` follow [Go's server layout guidance](https://go.dev/doc/modules/layout#server-project).
There is no universal Go folder layout for this entire stack. Keep app-specific
packages under `internal`, adding packages around real features as they appear.
Avoid a public `pkg` directory or repository/service interfaces until needed.
The existing OpenSpec configuration and skills are preserved.

## Development commands

```sh
make generate    # go generate -> module-pinned go tool templ
make assets      # copy Alpine/htmx and compile Tailwind CSS
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
the runtime library. Tailwind and its CLI are pinned in `package.json` and
`package-lock.json`. UI component sources are pinned to shadcn-templ
`v2.0.0-beta.15`, copied into `internal/web/ui` and adapted to preserve application structure and workflows. The active theme is
shadcn-templ preset `b870VEw0in` (Luma, Mist/Cyan, Noto Sans and EB Garamond). See [UI component maintenance](docs/ui-components.md)
before adding or upgrading components.

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
lifecycle, and shutdown handling. Custom UI routes require the operator
session; among the custom routes only `GET /health`, `/static/`, and
`robots.txt` are public. Login, session cookies, authorization, origin/CSRF
protection and rate limiting live in shared middleware under `internal/web`.
Server-side PocketBase record access bypasses collection API rules, so custom
handlers must enforce the operator boundary themselves.

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

Copy `.env.example` to `.env` to override the published host or port and to
supply the application configuration (`APP_ORIGIN`, `APP_ENCRYPTION_KEY`, and
the `MS_*` variables from the configuration table). `compose.yaml` maps those
values into the container; the Go process also takes PocketBase CLI flags.
The default host is loopback. For a remote host, place an HTTPS reverse
proxy in front and configure trusted proxy headers in PocketBase settings.
Set `APP_HOST=0.0.0.0` only when direct external access is intended. The
container health check probes the public `GET /api/health` endpoint.

```sh
docker compose down           # stop; retain data
docker compose up --build -d  # rebuild/recreate; retain data
```

Run one app instance against this local SQLite volume; do not share it between
replicas. The Coolify deployment, backup, restore, restore-review and rollback
procedures are in [docs/coolify-deployment.md](docs/coolify-deployment.md);
release acceptance steps are in
[docs/release-checklist.md](docs/release-checklist.md). Use PocketBase's dashboard backup facility before upgrades. For raw
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
- [shadcn-templ components](https://shadcn-templ.com/docs)
- [UI component structure and maintenance](docs/ui-components.md)
- [Alpine installation](https://alpinejs.dev/essentials/installation)
- [htmx installation and HTML responses](https://htmx.org/docs/)
