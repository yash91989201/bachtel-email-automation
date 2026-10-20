# Migration verification — 2026-10-20

## Before the requested preset

- Original UI was reconstructed from HEAD in an isolated synthetic fixture with its original locked asset dependencies, avoiding live development rebuilds affecting the baseline.
- 34 desktop/mobile screenshots covered overview, login, mailbox, campaign list/form/template/reference/settings, task review/preview/results and sending test/send/recovery/recent/results/pacing. All page dimensions and layout positions matched. Remaining raster differences were small control rendering differences; the largest measured difference before final neutral/disabled-button corrections was below 1% of pixels.
- Native HTML comparisons passed for all 10 representative routes. Four browser interaction comparisons passed: campaign keyboard/hash tabs and retained input, task preview selection, progress updates with persistent diagnostic address/tab/disclosure, and confirmations without unintended submissions. No POST request occurred.
- The exploratory expectation that the approval checkbox remains checked across polling failed in both original and migrated fixtures. This is a shared baseline limitation, not evidence of a migration regression; it was not silently changed as part of this refactor.
- Focused UI and class-merging tests, full web integration tests, `make lint`, `make check` (format, generated-output consistency, lint, full race tests and production build), and `make verify-converter` passed.
- Fresh `npm ci` succeeded; repeated asset builds produced the same CSS checksum. daisyUI is absent from package manifests/lock and active source/configuration.

## Existing dependency advisories

`make vulncheck` ran and failed with four reachable advisories in the unchanged
`github.com/xuri/excelize/v2 v2.11.0`: GO-2026-6689, GO-2026-6688,
GO-2026-6685 and GO-2026-6684. None names the added class-merging dependency.
The migration does not claim a clean vulnerability check or production release acceptance.

`npm audit` reports four high-severity findings in the existing Tailwind CLI /
Parcel watcher / micromatch / braces dependency chain. The original HEAD lockfile
was audited separately and reports the same four findings. Tool upgrades remain
outside this UI migration.

## Requested preset

The operator subsequently requested `--preset b870VEw0in`, intentionally changing
the migrated interface's theme. The pinned beta.15 CLI decoded and resolved it
in an isolated module. The resolved token sequence matches the pinned CLI output.
The application uses Luma, Mist/Cyan, small radius, Noto Sans body text and EB
Garamond headings; the complete preset preferences are recorded in `preset.json`.
Eight local font files have verified source checksums, with OFL licenses retained.
Browser network checks confirmed the active body/heading fonts return HTTP 200
from local `/static/fonts/` paths.

34 final desktop/mobile screenshots cover all representative routes and tab states
with no horizontal page overflow. Body and heading font loading and computed
preset colors passed. Selected and hovered sidebar descriptions use the accent
foreground after a contrast correction. Ten native HTML comparisons and four
interaction comparisons retain migration parity with no POSTs. The shared
approval-checkbox limitation remains as documented above.

The console sweep found a missing `/favicon.ico` in the synthetic fixture; a
focused resource check identified that URL and confirmed both active font
responses succeed. No application script exception was reported. Synthetic QA
artifacts and reusable fixture scripts are retained locally under ignored
`tmp/_ui-migration/`; tests used no production database, credentials or mailbox.
The final packaged converter check and strict OpenSpec validation passed.

Final preset `make check` passed, including formatting, regeneration consistency,
lint, all race tests and the production binary build. `make verify-converter`
also passed after the final sidebar contrast correction.
