# Coolify deployment and recovery

One application instance owns one persistent data directory. Do not run
replicas against the same volume: the sending dispatcher takes an exclusive
lock on the data directory and SQLite requires a single writer.

## Container contract

- The image runs as UID/GID `10001` and stores everything under `/app/pb_data`:
  the database, private uploads, generated downloads, ownership lock and
  PocketBase metadata.
- `pandoc-cli=3.10-r0` is packaged in the runtime image and pinned. No runtime
  Node, Word or LibreOffice is required. The frontend is built during the image
  build and served from the binary.
- The application listens on `0.0.0.0:8090` inside the image.
- Health: `GET /health` returns `{"status":"ok"}` and no database, mailbox or
  task detail. Use it as the Coolify health check.
- The container is granted a 45-second stop allowance so a shutdown can drain
  bounded Graph requests (20-second deadlines) and persist final outcomes
  (35-second application drain budget). A forced kill is still recovered
  conservatively on the next start: in-flight attempts become uncertain, running
  tasks become interrupted, and nothing is resent automatically.
- Export activation uses committed task notifications and one startup scan after
  sending recovery. A healthy idle export worker does not poll the database.
  Interrupted task exports and previously requested campaign exports rebuild
  automatically on startup. Ordinary generation failures require explicit retry.
  Cancelling export work during shutdown leaves its durable claim recoverable;
  markers saved by the dispatcher later in the drain are picked up next start.

Local Compose equivalent:

```sh
docker compose up --build -d
docker compose logs -f app
```

## Volume ownership on first deployment

Create the persistent volume before starting, and make sure the application
user can write to it. If the platform creates the volume as root:

```sh
docker run --rm -v <volume-name>:/data alpine:3.24 chown -R 10001:10001 /data
```

Mount that volume at `/app/pb_data`. Do not mount source folders into the
runtime container.

## Coolify configuration

1. Create one application resource from this repository and let Coolify build
   the Dockerfile.
2. Set the exposed/internal port to `8090`. Do not rely on a loopback host
   publish for public access.
3. Attach the persistent volume at `/app/pb_data`.
4. Configure the public HTTPS domain. Let Coolify's proxy terminate TLS and
   forward to the internal port. Set `APP_ORIGIN` to the exact public origin,
   without a trailing slash and without a path.
5. Add the environment variables below as Coolify secrets/environment values.
   They are read from the process environment only; never commit real values or
   bake them into the image.
6. Health check path: `/health`. Start period: at least 10 seconds.
7. Set the stop grace period to `45` seconds.
8. Register the Microsoft redirect URI exactly as `APP_ORIGIN` +
   `/connection/callback` (unless `MS_REDIRECT_URL` overrides it) on the Entra
   app registration.

| Variable | Required | Purpose |
| --- | --- | --- |
| `APP_ORIGIN` | yes | Public HTTPS origin: cookie security, origin checks, callback default |
| `APP_ENCRYPTION_KEY` | yes for mailbox use | Base64 of 32 random bytes; encrypts stored Microsoft credentials |
| `MS_TENANT_ID` | yes for mailbox use | Entra tenant id |
| `MS_CLIENT_ID` | yes for mailbox use | Entra application id |
| `MS_CLIENT_SECRET` | yes for mailbox use | Entra client secret |
| `MS_REDIRECT_URL` | optional | Exact callback override; must match the registration |
| `MS_SENDER_ADDRESS` | optional | Allowed mailbox; defaults to `hr@bachtelcorporation.com` |
| `PANDOC_PATH` | optional | Converter executable; defaults to the packaged `pandoc` |
| `APP_RESTORE_REVIEW` | only after a restore | `true` disables sending, scheduling, retries and resends |
| `INTAKE_*` | optional | Upload and expanded-archive bounds |

Missing Microsoft settings do not prevent startup: the connection page reports
the missing names without showing values, and dispatch cannot proceed until a
mailbox is connected and approved. A malformed origin or callback fails startup.

### Proxy trust

`APP_ORIGIN` is the canonical origin used for cookies, origin checks and the
callback. If the proxy sends forwarded headers, configure PocketBase's trusted
proxy headers in its settings so client addresses and scheme are interpreted
correctly. Do not infer the origin from request headers in application code.

## Backup

A consistent backup must contain the database, private uploads, generated
downloads and PocketBase metadata together. PocketBase's dashboard backup
facility copies its data directory; verify that the backup archive includes the
`storage` directory containing record files. If any application-owned file is
outside that facility, stop the application and copy the complete `pb_data`
volume instead. Never copy only the live SQLite database file.

Back up `APP_ENCRYPTION_KEY` separately from the backup. Losing it leaves the
stored connection ciphertext intact but requires reconnecting the mailbox.

## Restore

1. Restore into an isolated instance with no production traffic.
2. Restore the matching `APP_ENCRYPTION_KEY` separately. A matching key makes
   the stored connection usable again without re-consent; a missing key leaves
   the connection unreadable and sending disabled.
3. Set `APP_RESTORE_REVIEW=true` and start the instance. Sending, scheduling,
   retries and resends are refused; stop, history resolution and exports remain
   available.
4. Compare the backup's as-of time with any mailbox or provider evidence
   collected since that time. Recipients submitted after the backup are absent
   from the restored database, and a restored row that says "never submitted"
   is not proof. Reconcile those gaps explicitly (for example by marking
   verified submissions as already submitted).
5. Remove `APP_RESTORE_REVIEW` and restart only after reconciliation. Review
   recovered tasks on their sending page before resuming any remainder.

## Rollback

Stop and drain the instance, preserve the sending ledger and attempt history,
and restore compatible code and schema together. Do not restore code that
treats uncertain attempts as ordinary unsent candidates. A database restore
must use restore-review mode until the post-backup submission gap is
reconciled.

Backup deletion is manual and separate from live campaign deletion. Removing a
campaign in the application never rewrites an existing backup.

See [release-checklist.md](release-checklist.md) for the deployment acceptance
steps.
