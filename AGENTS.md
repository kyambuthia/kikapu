# AGENTS.md

This file contains invariants and guidelines for AI agents working on this codebase.

## Architecture Invariants

1. **Single Binary**: The service must remain a single binary. All functionality is compiled into one executable.

2. **No In-Process State**: Never use in-process state as source of truth. All state lives in PostgreSQL. In-memory caches are acceptable but must not be the authoritative source.

3. **Money in Minor Units**: All monetary values are stored as `int64` in minor units (cents, pence, satoshis, etc.). Use `internal/platform/money` package for all money operations. Never use `float64` for money.

4. **Idempotent Mutations**: All mutation endpoints must be idempotent where possible. Use idempotency keys for operations that need them.

5. **Small Commits**: Keep commits small and focused. Never accumulate more than ~200 lines of uncommitted changes.

6. **Conventional Commits**: Use conventional commit messages:
   - `feat:` for new features
   - `test:` for tests
   - `docs:` for documentation
   - `chore:` for maintenance
   - `fix:` for bug fixes
   - `refactor:` for refactoring

## Code Standards

1. **Standard Library First**: Use standard library packages when possible. Only add external dependencies when there's a clear benefit.

2. **Table-Driven Tests**: Use table-driven tests for test suites. See `internal/platform/money/money_test.go` for examples.

3. **No Unimplemented Code**: Do not create directories or packages for features that are not implemented yet.

4. **Environment Configuration**: All configuration comes from environment variables with sensible defaults. See `internal/platform/config`.

5. **JSON Error Responses**: All HTTP errors use the error envelope format:
   ```json
   {
     "error": {
       "code": "error_code",
       "message": "Human readable message"
     }
   }
   ```

6. **Request Tracing**: Every HTTP request should have a request ID (from `X-Request-ID` header or generated).

## Testing Requirements

- Every package should have at least one test file
- Money package must have comprehensive tests for parse/format/add/sub/mul/rounding
- HTTP utilities must have tests for error envelope serialization
- All tests must be table-driven where appropriate
- Minimum 8 test cases for money package

## Build and CI

- `gofmt -l .` must print nothing
- `go vet ./...` must be clean
- `go build ./...` must succeed
- `go test ./...` must pass
- `go run ./cmd/kikapu --help` must exit 0

## Running Tests

```bash
make test
```

Or directly:
```bash
go test ./...
```

## Before Committing

Always run:
```bash
make vet && make fmt && make test
```
