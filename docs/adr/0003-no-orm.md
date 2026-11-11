# ADR-0003: No ORM — database/sql + Explicit SQL

## Status

Accepted

## Context

Go applications need to interact with relational databases. Several approaches exist:
1. Raw SQL with `database/sql` package
2. ORM libraries (GORM, ent, XORM)
3. Query builders (sqlx, squirrel)
4. Code generation (sqlc)

### ORM Example (GORM)

```go
type Product struct {
    gorm.Model
    Name   string
    Price  int64
}

db.First(&product, 1)
```

### Raw SQL Example

```go
var product Product
db.QueryRow("SELECT id, name, price FROM products WHERE id = $1", 1).Scan(&product.ID, &product.Name, &product.Price)
```

## Decision

We use the standard library `database/sql` package with explicit SQL queries.

No ORM. No query builder. Raw SQL with prepared statements.

### Implementation

#### 1. Connection Pool

```go
import "database/sql"

func NewDB(dataSourceName string) (*sql.DB, error) {
    db, err := sql.Open("postgres", dataSourceName)
    if err != nil {
        return nil, err
    }
    
    db.SetMaxOpenConns(25)
    db.SetMaxIdleConns(5)
    db.SetConnMaxLifetime(5 * time.Minute)
    
    return db, nil
}
```

#### 2. Explicit Queries

```go
func (r *PostgresProductRepository) Get(ctx context.Context, id string) (*Product, error) {
    const query = `
        SELECT id, slug, name, status
        FROM products
        WHERE id = $1
    `
    
    var p Product
    err := r.db.QueryRowContext(ctx, query, id).Scan(
        &p.ID,
        &p.Slug,
        &p.Name,
        &p.Status,
    )
    
    if errors.Is(err, sql.ErrNoRows) {
        return nil, ErrProductNotFound
    }
    if err != nil {
        return nil, fmt.Errorf("get product: %w", err)
    }
    
    return &p, nil
}
```

#### 3. Prepared Statements

```go
func (r *PostgresProductRepository) Create(ctx context.Context, p *Product) error {
    const query = `
        INSERT INTO products (id, slug, name, status)
        VALUES ($1, $2, $3, $4)
    `
    
    _, err := r.db.ExecContext(ctx, query, p.ID, p.Slug, p.Name, p.Status)
    if err != nil {
        return fmt.Errorf("create product: %w", err)
    }
    
    return nil
}
```

#### 4. Transaction Support

```go
func (s *Service) PlaceOrder(ctx context.Context, req CheckoutRequest) (*Order, error) {
    tx, err := s.db.BeginTx(ctx, nil)
    if err != nil {
        return nil, err
    }
    defer tx.Rollback()
    
    order, err := s.createOrder(tx, req)
    if err != nil {
        return nil, err
    }
    
    err = s.inventory.Reserve(tx, order.ID)
    if err != nil {
        return nil, err
    }
    
    err = s.outbox.Append(tx, NewOrderEvent(order))
    if err != nil {
        return nil, err
    }
    
    return order, tx.Commit()
}
```

## Consequences

### Positive

- **Explicit control**: Every SQL query is visible in code (no magic)
- **Performance**: Direct SQL is the fastest option
- **Learning curve**: Standard library, widely documented
- **No vendor lock-in**: Not coupled to specific ORM
- **Transparency**: Schema is in migrations, not struct tags
- **Reliability**: `database/sql` is tested by millions of Go developers
- **Connection pooling**: Built into standard library

### Negative

- **Boilerplate**: More code than ORM
- **Manual mapping**: Must write Scan() for each query
- **No migrations**: Need separate migration tool (golang-migrate)
- **Type safety**: Less compile-time checking than ORM
- **No struct tags**: Schema not visible in model structs

### Rejected Alternatives

#### Alternative 1: GORM

```go
import "gorm.io/gorm"

db.First(&product, 1)
```

**Why rejected**:
- **Hidden queries**: SQL generated invisibly, hard to optimize
- **N+1 queries**: ORM encourages lazy loading
- **Magic behavior**: Hooks, associations, soft deletes are implicit
- **Runtime errors**: Schema mismatches caught at runtime
- **Verbose errors**: GORM error messages are cryptic
- **Performance overhead**: Reflection-based, slower than raw SQL

#### Alternative 2: ent (Facebook)

```go
import "entgo.io/ent"

product, err := client.Product.Query().Where(product.ID(id)).Only(ctx)
```

**Why rejected**:
- **Code generation**: Requires running `go generate`
- **Complex schema**: Schema defined in Go code, not SQL
- **Learning curve**: New query DSL to learn
- **Overkill**: Too feature-rich for our needs

#### Alternative 3: sqlx

```go
import "github.com/jmoiron/sqlx"

db.Get(&product, "SELECT * FROM products WHERE id = $1", id)
```

**Why rejected**:
- **Close to standard library**: Doesn't add enough value to justify dependency
- **Struct mapping**: Uses reflection (slower than explicit Scan)
- **Not enough benefit**: Minor convenience over raw SQL

## Best Practices

With explicit SQL, we follow these practices:

### 1. All Queries in Constants

```go
const getProductQuery = `
    SELECT id, slug, name, status
    FROM products
    WHERE id = $1
`
```

**Benefits**: SQL is reviewable, searchable, optimizable.

### 2. Avoid SELECT *

```go
// WRONG
SELECT * FROM products

// CORRECT
SELECT id, slug, name, status FROM products
```

**Benefits**: Explicit column list, schema changes are caught.

### 3. Use Context for Cancellation

```go
db.QueryRowContext(ctx, query, id).Scan(...)
```

**Benefits**: Request timeout propagates to database.

### 4. Wrap Errors

```go
if err != nil {
    return fmt.Errorf("get product %s: %w", id, err)
}
```

**Benefits**: Error trail for debugging.

### 5. Test with Real Database

```go
func TestPostgresProductRepository_Get(t *testing.T) {
    db := setupTestDB(t)
    defer db.Close()
    
    // Insert test data
    db.Exec("INSERT INTO products (id, name) VALUES ($1, $2)", "prod-1", "Test")
    
    repo := NewPostgresProductRepository(db)
    product, err := repo.Get(context.Background(), "prod-1")
    
    assert.NoError(t, err)
    assert.Equal(t, "Test", product.Name)
}
```

**Benefits**: Catches real SQL errors, no mocking.

### 6. Use Migrations

```sql
-- migrations/001_initial_schema.up.sql
CREATE TABLE products (
    id TEXT PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft'
);
```

**Benefits**: Schema is versioned, reproducible.

## Performance Characteristics

| Operation | Raw SQL | GORM | Overhead |
|-----------|---------|------|----------|
| Single SELECT | ~1ms | ~3ms | 3x slower |
| INSERT | ~1ms | ~4ms | 4x slower |
| JOIN query | ~2ms | ~8ms | 4x slower |
| N+1 detection | Manual review | Easy to miss | Risk |

## Implementation Timeline

- **Phase 3**: PostgreSQL persistence
- **Estimated effort**: 3-4 weeks for all repositories
- **Dependencies**: Phase 1 (foundation)

## References

- [database/sql documentation](https://pkg.go.dev/database/sql)
- [Go database/sql tutorial](http://go-database-sql.org/)
- [Why not to use an ORM](https://www.alexedwards.net/blog/why-you-should-not-use-an-orm)
- [GORM performance issues](https://github.com/go-gorm/gorm/issues?q=performance+slow)
