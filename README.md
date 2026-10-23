# Bachtel Email Automation

A Go application using templ, htmx, Alpine.js, Tailwind CSS, locally owned shadcn-templ components, and
PocketBase. One executable serves the UI, static assets, PocketBase API, and
admin dashboard on one port. PocketBase runs in-process with SQLite; there is
no separate database service or frontend server.

The application includes operator sign-in, operator-owned mailbox connections
(Microsoft 365 delegated OAuth or GoDaddy Titan SMTP), campaign templates,
Excel intake, and durable background sending and recovery controls. Candidate
starts require explicit operator authorization
and received-format approval for a successfully submitted test of the exact
configuration. Explicit diagnostic test emails are available through the
connected mailbox.

## Run locally

Prerequisites: Go 1.27.2+, Node.js 26+, npm, and Make.

```sh
make setup
make dev
```

Open <http://127.0.0.1:8090/login> to sign in to the operator workspace. The
application provides campaign setup, workbook intake, sending review and
recovery pages behind the operator account.

Health endpoints:

- `GET /health` — public, minimal JSON `{"status":"ok"}` for platform probes;
  it exposes no application state.
- `GET /api/health` — PocketBase's own public health endpoint.

The application UI requires an operator account: provision one per operator (see below),
then sign in at <http://127.0.0.1:8090/login>. Anonymous visitors can only
reach the sign-in page, `/health`, and the public static assets. The
PocketBase dashboard at <http://127.0.0.1:8090/_/> is a separate
administrative surface that requires a superuser.

On a fresh database, use the one-time installer link printed in the server
console to create your first superuser. No credentials are seeded.

`make dev` uses [Air](https://github.com/air-verse/air) to regenerate templates
and assets, rebuild, and restart the server when Go, `.templ`, CSS, or
JavaScript or WOFF2 font sources change. The pinned Air version downloads automatically on
the first run; no global installation is required. It also
sources a local `.env` into the server process when one exists: copy
`.env.example` to `.env` and fill it in instead of exporting variables by
hand. Restart `make dev` after changing `.env` or `.air.toml`. Refresh the
browser after a rebuild to see UI changes. Generated CSS, JavaScript and copied fonts are ignored by Git;
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
| `GET /connection`, `POST /connection/microsoft/connect`, `GET /connection/callback`, `POST /connection/titan`, `POST /connection/disconnect`, `GET /connection/replace`, `POST /connection/replace/confirm`, `POST /connection/replace/cancel` | Operator session; only the signed-in operator's own mailbox can be connected, reconnected, disconnected or replaced (the Microsoft callback must match an in-flight authorization attempt) |
| `GET /static/`, `GET /robots.txt` | Public assets; `robots.txt` disallows everything |
| `GET /api/health` | Public PocketBase health probe |
| other `/api/*` | PocketBase API; the application's private collections are locked down (see below) |
| `/_/` | PocketBase admin dashboard (superuser only) |

Custom UI routes and the private collections are unreachable anonymously.

## Operator provisioning

Provision one account per operator, using the same `--dir` as the server so it
writes to the same database. Each account gets an independent private workspace
with its own campaigns, uploads, history and mailbox connection; accounts cannot
read each other's records:

```sh
./bin/bachtel provision-operator --email operator@example.com --dir=./pb_data
# during development:
go run ./cmd/server provision-operator --email operator@example.com --dir=./pb_data
```

The password is read from the terminal without echo, or piped on stdin:

```sh
echo "$PASSWORD" | ./bin/bachtel provision-operator --email operator@example.com --dir=./pb_data
```

No credentials are seeded anywhere, public registration stays disabled, and an
already used login identity is refused without changing that account's password.
Operators cannot create or administer peer accounts.

### Local schema reset

This branch changes the schema from one fixed mailbox to operator-owned
mailbox connections. Local data written by the previous schema is not migrated.
For local development only, stop the server (and any worker) and reset the
selected data directory explicitly:

```sh
./bin/bachtel reset-local-data --data-dir ./pb_data --yes
```

The command refuses a directory another dispatcher owns and requires the path to
look like a PocketBase data directory. It deletes the local database, uploads,
generated downloads, connections and encrypted credentials in that directory.
Start the server afterwards to create the final schema, provision each operator,
and reconnect each mailbox. It is never run by startup or by automated checks;
CI always uses throwaway data directories. There is no production compatibility
migration for the old singleton schema.

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

All deployment settings are read from the environment (documented with
placeholders in `.env.example`; Compose maps them into the container, and
`make dev` sources a local `.env`). The environment holds deployment settings:
the public origin, the credential-encryption key, restore review, the converter
path, the intake bounds and the Titan test endpoint.

There are no Microsoft environment credentials. Each operator saves their own
Microsoft **Tenant ID**, **Client ID** and **Client Secret** on the protected
`/account/mailbox` page before signing in; the Client Secret is write-only and
is never shown or pre-filled again. The callback URI is fixed and derived from
`APP_ORIGIN`: exactly `APP_ORIGIN` + `/connection/callback`. The connection
Mailbox page displays it for registration.

| Variable | Purpose |
|---|---|
| `APP_ORIGIN` | Public `https` origin, no trailing slash (e.g. `https://mail.example.com`). Drives cookie security, trusted-origin checks, and the derived callback URL (`APP_ORIGIN` + `/connection/callback`). |
| `APP_ENCRYPTION_KEY` | **Secret.** Base64 encoding of exactly 32 bytes; encrypts every operator's mailbox credentials at rest — Microsoft tokens, Titan passwords and saved Microsoft client secrets. Generate with `openssl rand -base64 32`. |
| `PANDOC_PATH` | Optional converter executable; defaults to the packaged `pandoc`. |
| `APP_RESTORE_REVIEW` | Optional; `true` disables sending, scheduling, retries and resends until a restored instance is reconciled. |
| `TITAN_SMTP_ADDRESS` | Advanced override of the Titan SMTP endpoint for simulated servers. Leave unset in production; the fixed provider endpoint is `smtpout.secureserver.net:465`. |
| `TITAN_IMAP_ADDRESS` | Advanced override of the Titan mailbox endpoint used only for Sent copies, for simulated servers. Leave unset in production; the fixed endpoint is `imap.secureserver.net:993`. |
| `INTAKE_MAX_UPLOAD_BYTES` | Upload limit for one task workbook, in bytes. Defaults to 10 MiB, which is also the supported ceiling. |
| `INTAKE_UNZIP_SIZE_LIMIT`, `INTAKE_UNZIP_XML_SIZE_LIMIT` | Expanded workbook archive and single-part limits, in bytes. Default to 64 MiB and 16 MiB. |

`APP_ENCRYPTION_KEY` is separate from PocketBase's own `--encryptionEnv`
settings key; each protects different material. It protects both the stored
mailbox credentials (Microsoft tokens, Titan passwords) and every operator's
saved Microsoft client secret. Back it up independently of the database:
losing it leaves the stored ciphertext intact but requires reconnecting
affected mailboxes. A missing or wrong key does not crash the server — affected connections report an actionable
error and no email can be sent.

See [docs/microsoft-setup.md](docs/microsoft-setup.md) for the full per-operator
Microsoft setup, [docs/titan-setup.md](docs/titan-setup.md) for connecting a
GoDaddy Titan mailbox, and
[docs/mailbox-replacement.md](docs/mailbox-replacement.md) for reconnecting the
same mailbox, rotating its application settings, or replacing it with a
different one. [docs/account-settings.md](docs/account-settings.md) covers the
Account settings sections, the private AI connection, connection locking and
terminal task closure.

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
  A recipient cell may hold
  several comma-separated addresses; each becomes its own recipient entry with
  the row's other mapped values, counted toward the 1,000-entry bound before any
  exclusion. Duplicate recipient keys hold every occurrence of their group,
  including repetitions inside one cell, and unusable addresses exclude only
  their own entry. Excluded entries live only in the stored workbook, a private
  validation manifest and a downloadable exclusion report — never as candidate
  records.
- Candidates are identified by campaign plus trimmed lowercase email address,
  with generated internal ids, so a corrected upload creates a new task over the
  same identities and an earlier task keeps its snapshot.
- A validated task is **ready**, not running. Its sending page provides explicit
  test, received-format approval, start, stop and recovery actions. Candidate
  actions recheck and claim eligibility transactionally; production candidate
  dispatch requires received-format approval for the exact configuration and
  an explicit operator action.
- A validated task can optionally prepare its mapped values through the
  operator's confirmed AI connection. Only mapped values are sent; every run
  keeps its captured input, base prompt version, task instructions, exact
  provider/endpoint/model and published output. Resolved corrections are kept
  apart from unresolved findings, and a shared-field finding holds all recipient
  siblings. Preparation is durable evidence only: it never writes candidates or
  attempts, never authorizes sending, and later review/finalization is a
  separate stage.
- The upload screen shows the workbook constraints (supported format, size,
  expanded-archive limits and the 1,000-recipient-entry bound after comma
  splitting) and reports every rejection inline.

Workbook support, header matching, recipient-key rules, resource limits,
duplicate handling and the private artifacts are documented in
[docs/task-intake.md](docs/task-intake.md).

## History and downloads

Completed, stopped and recovered tasks publish a results workbook that keeps
the retained original upload untouched and adds a `Recipient results` worksheet
with one row per expanded recipient. Campaign pages publish an Excel report and a ZIP
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

Pacing is per mailbox with a default rate of 25 starts per minute (integer
settings 1–30), persisted pacing and provider cooldowns. At most four
submission requests run at once per mailbox; there is no application-wide cap.
Cooldowns, throttling, authorization failures and service pauses affect only
the affected mailbox; a shared storage failure stops all launches.
**SUBMITTED** means the provider accepted the request, not that it was
delivered. Uncertain
attempts are held for review and never automatically resent. Stop drains
requests already started; restart requires explicit resume of safe work.

A validated task also accepts a one-time scheduled start at least 30 minutes
ahead, explicit start authorization that fires from durable state across
restarts. It starts only the candidates free at that instant; a window that
passes while the server is down pauses the task with a missed-schedule reason
and no attempt, and resume or reschedule stays explicit.

See [docs/background-sending.md](docs/background-sending.md) for the states,
approval workflow, scheduling, retry and review actions, and shutdown behavior.

## Layout

```text
cmd/server/main.go              Application entrypoint and PocketBase setup
cmd/server/provision.go         One-time operator provisioning command
internal/
  config/                       Deployment configuration (origin, encryption key, restore review, intake bounds, provider endpoints)
  campaign/                     Campaign revisions, mapping validation, normalization and the single render path
  convert/                      Sandboxed DOCX-to-HTML conversion (pinned Pandoc) and document inspection
  intake/                       Task workbook validation, candidate identity, membership snapshots and reservations
  preparation/                  AI data-preparation runs, structured response validation and the durable preparation worker
  mailbox/                      Operator-owned mailbox claims, connection status and encrypted credentials
  microsoft/                    Microsoft connection service (OAuth flow, encrypted credentials)
  operator/                     Operator account provisioning
  reset/                        Destructive mailbox replacement reset (durable, per operator)
  sending/                      Dispatcher, pacing, Microsoft Graph and Titan senders, schedules, approvals
  exports/                      Task workbooks, campaign reports and packages
  retention/                    Campaign deletion scope and active-work refusal
  migrations/
    1792233000_initial_settings.go      App name and PocketBase rate limiter
    1792233010_operator_collection.go   Operator auth collection with locked rules
    1792233020_mail_connections.go      Operator-owned mailbox connections and claims
    1792233040_campaign_collections.go  Campaigns and immutable configuration revisions
    1792233050_intake_collections.go    Tasks, campaign candidate identity and membership snapshots
    1792320000_recipient_mapping.go     Recipient source-header mapping
    1792320010_sending.go               Attempts, approvals, actions, resolutions, dispatcher state
    1792320020_task_schedule.go         Single-shot task schedules
    1792320030_exports.go               Generated download artifacts
    1792800000_account_connections.go   Private AI connection and terminal task closure
    1792900000_task_data_preparation.go Preparation runs/jobs, instruction drafts and run selection
  web/
    web.go                      Route table, handler assembly, render helper
                                (/health, /robots.txt, /static, GET /{$}, GET /status, GET /account/mailbox)
    session.go                  Operator session cookie creation/validation and authorization middleware
    login.go                    GET /login, POST /login, POST /logout
    connection.go               Mailbox handlers: Microsoft connect/callback, Titan connect, disconnect, replacement
    campaign.go                 /campaigns pages: create, revision upload, labels, sample preview
    task.go                     Task intake pages: upload, worksheet selection, review, downloads
    preparation.go              Preparation actions: prepare, retry, rerun, select and instruction drafts
    sending.go                  Sending and recovery pages and actions
    ratelimit.go                Fixed-window login rate limiter
    web_test.go                 Route, auth, CSRF, rate-limit, and DB integration tests
    views/                      Page view models, layouts and templates
    ui/                         Locally owned shadcn-templ components and class utilities
    static/                     Embedded files and generated CSS/JS
docs/campaign-templates.md      Placeholder syntax, mapping/formatting rules, DOCX support and limits
docs/task-intake.md             Task workbook support, header matching, duplicate handling and limits
docs/microsoft-setup.md         Per-operator Microsoft app registration and connection setup
docs/titan-setup.md             GoDaddy Titan mailbox connection setup
docs/mailbox-replacement.md     Reconnecting the same mailbox, rotating its application settings, and replacing it with a different one
docs/account-settings.md        Account mailbox/AI sections, private AI connection, connection lock and Close task
web/css/app.css                 Tailwind entry and application layout styles
web/css/theme.css               Resolved preset tokens and application status colors
web/css/components.css          Bachtel styling for owned UI primitives
web/fonts/                     Self-hosted Noto Sans and EB Garamond font files and OFL licenses
components.json                 shadcn-templ component and asset paths
preset.json                     Pinned shadcn-templ preset selection
web/css/fonts.css               Font-face declarations for local preset fonts
web/css/vendor/                 Pinned upstream component and animation styles
web/js/workspace.js            Tracked workspace adapter: URL fragment, disclosures, htmx swaps
scripts/copy-assets.mjs         Copy pinned Alpine/htmx, licensed fonts and the adapter locally
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
make assets      # bundle component JS, copy Alpine/htmx, fonts and web/js; compile Tailwind CSS
make fmt         # format Go and templ sources
make lint-tailwind # check all tracked and non-ignored new .templ files for Tailwind diagnostics
make fix-tailwind # apply Tailwind canonical-class code actions, then check remaining diagnostics
make test        # regenerate, build assets, go test (full vet runs in make lint)
make test PACKAGES=./internal/intake TEST_FLAGS='-run TestUpload' # focused feedback
make test-race PACKAGES=./internal/sending # focused race checks
make check       # final formatting, generation, lint, build, full race suite
make build       # build bin/bachtel with assets embedded
./bin/bachtel serve --http=127.0.0.1:8090 --dir=./pb_data
```

During implementation, batch edits and run the affected tests. Run the full
final checks once after integration, rather than after each OpenSpec task.
See [testing and test inventory](docs/testing.md) for coverage and timing.

Run `npm ci` first to install the pinned development tools. `npm run lint:tailwind`
is equivalent to `make lint-tailwind` and needs no editor. It uses Tailwind's
language server with `web/css/app.css`, including canonical-class suggestions,
and prints `file:line:column` diagnostics. It checks source without changing it.
For the npm command, exit 0 means clean, 1 means diagnostics were found, and 2
means the check could not complete. Run `npm run test:tailwind` to verify it against temporary
clean and warning-producing templates. This is a separate check from `make check`.

Use `npm run fix:tailwind` (or `npm run lint:tailwind -- --fix`) to apply the
language server's canonical-class code actions, as an editor would. It only
applies canonical suggestions with one replacement, then checks the changed
text again; other warnings remain visible and keep the exit code nonzero.
Review the diff, format changed `.templ` sources and regenerate their committed
`*_templ.go` output before running `make check`.

Node.js is build tooling only. The binary can be copied to another compatible
machine and run from any working directory; use an absolute `--dir` path to
keep the database location stable. The default data directory is `./pb_data`.
All frontend libraries are served locally, with no runtime CDN dependencies.

The templ CLI is pinned with Go's `tool` directive so its generator matches
the runtime library. Tailwind and its CLI are pinned in `package.json` and
`package-lock.json`. UI component sources are pinned to shadcn-templ
`v2.0.0-beta.15`, copied into `internal/web/ui` and adapted to preserve application structure and workflows. Workspaces use the owned
`tabs`/`collapsible` primitives with the tracked `web/js/workspace.js` adapter, and confirmations use `checkbox` and `alertdialog`. The active theme is
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
container health check probes the public minimal `GET /health` endpoint.

The Docker build context must include `package.json`, `package-lock.json`,
`scripts/`, `web/` (including local fonts and their licenses),
`internal/web/views/`, and `internal/web/ui/`. The image builds the frontend
with Node in a build stage, embeds the generated CSS, JavaScript and fonts in
the Go binary, and runs without Node, a component CLI or CDN access. This UI
migration changes no database schema; deploy it by rebuilding and replacing
the image while retaining the `pb_data` volume. Follow the separate release
checklist before enabling candidate sending.

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
