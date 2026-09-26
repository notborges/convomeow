# ConvoMeow repository guidance

## Code organization

- Keep `cmd/convomeow` focused on startup, configuration, and shutdown.
- Put account and message behavior in `internal/app`. Keep `internal/core` limited to types and contracts shared across providers.
- Keep whatsmeow imports and WhatsApp-specific data in `internal/providers/whatsapp`. Translate provider data at that boundary so future connectors can use the same app layer.
- Keep persistence and schema setup in `internal/store/sqlite`, HTTP handling in `internal/api/native`, and CLI commands in `internal/cli`.
- Keep the browser client in `web/src`, grouped by feature. Serve its built files through `internal/api/web` when the web view is enabled.
- Keep shared browser components in `web/src/ui`, semantic design tokens in `ui/tokens.css`, and feature layout styles alongside their feature. Reuse the shared controls and add component examples to the development design-system catalog.
- Before 1.0, create the current schema for fresh installations. Do not add upgrade migrations for unreleased versions.
- Keep `/api/v1` as ConvoMeow's native API. Put Evolution-compatible routes in a separate adapter and check paths, authentication, bodies, responses, and webhooks against Evolution's published contract.
- Add abstractions when a working feature needs them. Do not add unused routes or provider capabilities in anticipation of Telegram or a web client.
- Never commit tokens, session files, QR codes, message dumps, or the local `planning/` directory.

## Code and comments

- Use clear names, small functions, and `gofmt`. Handle errors at the boundary where they can be explained or acted on.
- Use Bun for web dependencies and scripts. Keep frontend dependency declarations at `latest` and commit `web/bun.lock`; run `bun update` when refreshing the lockfile. Format and lint web code with Biome.
- Comment only to explain a non-obvious invariant, protocol detail, concurrency rule, or security decision. Do not narrate what the next line does or leave placeholder TODOs.
- Keep user-facing text direct and factual. Avoid first-person phrasing in documentation. Name the WhatsApp behaviors that still need a real-account check.

## Commits and checks

- Write commit subjects as `<type>: <imperative summary>`, for example `fix: preserve account state after logout`. Use `feat`, `fix`, `docs`, `refactor`, or `chore`. Keep the subject under 72 characters and omit the final period.
- Keep commits focused. Add a body when the reason for a change or its compatibility impact is not clear from the subject and diff.
- Before committing, run `make check`. It checks the web build, formatting, module files, dependencies, Go build and vet, and Go tests with the race detector. After changing Go dependencies, run `go mod tidy` and review `go.mod` and `go.sum` before `make check`. Live WhatsApp behavior needs an account to verify.

- Keep browser copy in `web/src/i18n/locales`. Use typed `react-i18next` selectors and `Trans` for rich text; translate at render time, keep user/provider content intact, and update every supported locale. Format dates with the selected locale. Run `bun test` in `web` after translation changes.
