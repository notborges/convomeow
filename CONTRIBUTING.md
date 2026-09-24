# Contributing to ConvoMeow

Bug reports and pull requests are welcome. For a bug report, include the command or API request, what happened, what you expected, your operating system, and your Go version. Redact logs before posting them. Keep tokens, QR codes, session files, and message contents out of issues and pull requests.

## Set up

Install Go 1.27 or newer and a C compiler for SQLite. Clone the repository, then run:

```sh
make check
```

Use `make fmt` to format Go files. `make check` checks formatting and module files, verifies dependencies, builds, vets, and runs tests with the race detector.

## Make a change

Keep WhatsApp code in `internal/providers/whatsapp` and shared behavior in `internal/app`. Put shared types and contracts in `internal/core` only when a working feature needs them. Update `docs/api/openapi-v1.yaml` when you change the native API.

Add tests for behavior you change. Describe any manual checks in the pull request.

Run `make check` before opening a pull request. GitHub runs the same command on pull requests and pushes to `main`. If you changed dependencies, run `go mod tidy`, review the changes to `go.mod` and `go.sum`, and commit any changes to those files. Keep runtime data and the local `planning/` directory out of commits.

Use commit subjects in the form `<type>: <imperative summary>`, such as `fix: preserve account state after logout`. Use `feat`, `fix`, `docs`, `refactor`, or `chore`. In the pull request, describe the behavior change and the checks you ran.
