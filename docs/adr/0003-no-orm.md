# ADR-0003: No ORM

Status: accepted
Date: 2026-11-11

## Context

Kikapu needs to persist domain state to PostgreSQL. The Go ecosystem offers several approaches:
- Raw SQL with `database/sql`
- SQL builders (`squirrel`, `goqu`)
- ORMs (`GORM`, `ent`, `sqlc`)
- Query builders with code generation (`sqlc`, `xo`)

The service has clear requirements:
- Simple CRUD operations (products, carts, orders)
- Transaction boundaries need explicit control
- Performance matters (avoid N+1 queries)
- SQL should be reviewable without learning ORM DSL

## Decision

Use raw SQL with `database/sql` and `lib/pq` (Postgres driver). No ORM.

Reasoning:
1. **Transparency**: SQL is visible in code review
2. **Control**: Explicit transactions, prepared statements
3. **Performance**: No hidden queries, no reflection overhead
4. **Skill transfer**: SQL is universal, ORMs are framework-specific

Repository pattern:
```go
type ProductRepository interface {
    GetBySlug(ctx context.Context, slug string) (*Product, error)
    List(ctx context.Context, limit int, cursor string) ([]Product, string, error)
}
```

Implementation uses `sql.DB` directly with hand-written queries.

## Consequences

**Positive**:
- Full SQL visibility in code review
- No hidden N+1 queries
- Easier performance tuning
- No ORM instability risk
- Smaller binary (no reflection)

**Negative**:
- More boilerplate (scan structs manually)
- No auto-migration (use migration tools)
- No query composition (need to write joins manually)

**Mitigation**: Use migration tool (`golang-migrate`, `goose`) for schema management.

**Rejected**:
- **GORM**: Hidden queries, reflection, large binaries
- **ent**: Learning curve, code generation complexity
- **sqlc**: Good option, but adds build step; defer to Phase 3

## References

- `internal/catalog/product.go` (in-memory, will use SQL)
- ARCHITECTURE.md Section 3 (Module Map)
- ROADMAP.md Phase 3
