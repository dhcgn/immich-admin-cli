@.github/copilot-instructions.md

## Standing orders (every change)

- **Latest API spec first**: refresh `api/immich-openapi-specs.json` via `api/download.ps1`, then `go generate ./...` — never implement against a stale spec.
- **Skill must cover the feature**: every new or changed command has to show up in `immich-admin return-agent-skill` with accurate `Usage`/`ArgsUsage`/flags, plus updated examples in `skill_best_practices.md` — regenerate, commit, and verify an AI agent could use the new feature from the skill alone.
- **Expose new API calls natively**: any Immich operation used along the way gets a plain `<tag> <operation>` command in `internal/commands/`, never workflow-internal use only.