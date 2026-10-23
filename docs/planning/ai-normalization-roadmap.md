# AI normalization implementation order

1. **account-connection-settings** — Account sections, private AI setup/adapters, pending-task locks, credential repair and terminal closure. Standalone with existing intake.
2. **ai-task-data-preparation** — Requires 1. Durable mapped-field normalization, prompts, corrections/findings and run history. Preparation evidence only; no new sending path.
3. **reviewed-task-sending-snapshots** — Requires 1 and 2. Both final-sheet paths, backend validation, downloads, mapping changes, direct rendering and exact-snapshot approval. Activate the complete guarded workflow together.

Each change includes proposal, design, delta specs and unchecked verification tasks. Shared capability deltas are applied in this order; later ADDED requirements extend earlier capabilities. Do not apply all three concurrently or infer completion from planning validation. The retained ai-normalized-task-review-source directory is superseded reference material. No app implementation is part of this split.
