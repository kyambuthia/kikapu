# kikapu engineering standards

Immaculate means boring, explicit, and provable. These rules are enforced by CI, not by taste.

## Go

- **Errors**: return early, wrap with `%w` and context (`fmt.Errorf("catalog: load product %s: %w", id, err)`). No `panic` outside `main`/init; no `log.Fatal` in libraries. Sentinel errors only at API boundaries.
- **Context**: every I/O function takes `ctx context.Context` as the first parameter and honours cancellation. No `context.Background()` outside `main` and tests.
- **Money**: integer minor units only. No floats anywhere in the money path. Every operation that can overflow or divide returns an error. Rounding is explicit (`RoundHalfEven`, `RoundUp`, …), never implicit.
- **Interfaces** are declared by the consumer, not the producer, and stay small (1–3 methods). No interface until a second implementation or a test fake exists.
- **Concurrency**: no goroutine without an owner and a shutdown path. Shared state is guarded or absent. `-race` is part of the test gate.
- **HTTP**: server sets `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, `IdleTimeout`. Responses set `Content-Type` with charset, `X-Content-Type-Options: nosniff`, and a request id. Every handler has a timeout budget.
- **API**: JSON error envelope `{"error":{"code","message","request_id"}}`; mutations accept `Idempotency-Key`; list endpoints use cursor pagination with a documented max limit; unknown query fields are rejected (`400`).
- **Tests**: table-driven, named cases, no sleeps (inject clocks), `t.Parallel()` where safe, no assertion libraries. Coverage floor: 80% on `internal/platform` and `internal/cart`, `internal/checkout`.
- **Lint**: `gofmt`, `goimports`, `go vet`, `staticcheck`, `golangci-lint` (errcheck, gocritic, revive, bodyclose, noctx, sqlclosecheck). Zero findings, no `//nolint` without a reason on the same line.

## Git

- Conventional Commits: `type(scope): imperative summary` — type ∈ feat|fix|refactor|test|docs|chore|perf|build|ci. Summary ≤ 72 chars, no trailing period, imperative mood ("add", not "added").
- One logical change per commit; a commit must build and pass tests on its own.
- Body (when needed) explains **why**, wrapped at 72 columns. Reference issues with `Refs: #123`.
- No `wip`, `fixup`, `tmp`, or checkpoint commits in a pushed branch. Squash them before the PR.
- Branch names: `agent/<task-slug>` for automation, `feat/<slug>`, `fix/<slug>` for humans.

## Web

- Every endpoint documented in `openapi.yaml`; the spec is the contract, generated clients are disposable.
- Idempotency, pagination, and error codes are consistent across resources — no per-resource dialects.
- Security defaults: deny by default, explicit CORS allowlist, no stack traces in responses, structured logs without secrets or PANI.
- Observability: request id, structured JSON logs, latency + status metrics; logs are greppable and never contain card data (PAN), tokens, or phone numbers.
