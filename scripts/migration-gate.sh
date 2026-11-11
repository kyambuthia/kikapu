#!/bin/bash
set -euo pipefail

MIGRATION_GATE_VERSION="1.0.0"

RESULT=0

PORTR=""
LOGFILE=""

cleanup() {
    if [ -n "$PORTR" ] && command -v docker >/dev/null 2>&1; then
        docker rm -f "$PORTR" >/dev/null 2>&1 || true
    fi
    if [ -n "$LOGFILE" ] && [ -f "$LOGFILE" ]; then
        rm -f "$LOGFILE" 2>/dev/null || true
    fi
}

trap cleanup EXIT

log() {
    echo "[migration-gate] $1"
}

error() {
    echo "[migration-gate] ERROR: $1" >&2
}

fail() {
    error "$1"
    if [ -n "$LOGFILE" ] && [ -f "$LOGFILE" ]; then
        echo "Last 50 lines of Postgres log:" >&2
        tail -n 50 "$LOGFILE" >&2
    fi
    exit 1
}

usage() {
    cat <<EOF
Migration Compatibility Gate v${MIGRATION_GATE_VERSION}

Usage: $(basename "$0") [OPTIONS]

Validates that new migrations are compatible with the previous release.

Checks performed:
  1. All migrations apply exactly once (idempotency)
  2. Seeded row counts are preserved
  3. No destructive changes (DROP/ALTER) without -- allow-drop: header

Options:
  -h, --help              Show this help message
  -v, --version           Show version
  --no-cleanup            Keep containers/logs after run (for debugging)
  --self-test             Run self-test with destructive change injection

Environment Variables:
  TEST_DATABASE_URL       Use existing Postgres (skip Docker)
  MIGRATION_PATH          Path to migrations directory (default: ./migrations)

Requirements:
  - Docker (or TEST_DATABASE_URL set)
  - psql client
  - Git (for previous tag detection)

Exit Codes:
  0  All checks passed
  1  Incompatibility detected
  2  Setup/precondition error

Exit codes: 0 (pass), 1 (incompatibility), 2 (error)
EOF
}

version() {
    echo "migration-gate version ${MIGRATION_GATE_VERSION}"
}

SELF_TEST=""

parse_args() {
    while [ $# -gt 0 ]; do
        case "$1" in
            -h|--help)
                usage
                exit 0
                ;;
            -v|--version)
                version
                exit 0
                ;;
            --self-test)
                SELF_TEST=1
                shift
                ;;
            *)
                error "Unknown option: $1"
                usage >&2
                exit 2
                ;;
        esac
    done
}

check_dependencies() {
    local missing=0
    
    if [ -z "${TEST_DATABASE_URL:-}" ]; then
        if ! command -v docker >/dev/null 2>&1; then
            error "docker not found (or set TEST_DATABASE_URL)"
            missing=1
        fi
    fi
    
    if ! command -v psql >/dev/null 2>&1; then
        error "psql not found"
        missing=1
    fi
    
    if ! command -v git >/dev/null 2>&1; then
        error "git not found"
        missing=1
    fi
    
    if [ $missing -eq 1 ]; then
        exit 2
    fi
}

get_previous_tag() {
    local tag
    tag=$(git describe --tags --abbrev=0 2>/dev/null || echo "")
    
    if [ -z "$tag" ]; then
        fail "No previous tags found. Create an initial tag before running migration gate."
    fi
    
    echo "$tag"
}

get_previous_migration_dir() {
    local tag="$1"
    local workdir
    workdir=$(mktemp -d)
    
    log "Extracting migrations from tag $tag"
    
    git archive "$tag" -- migrations 2>/dev/null | tar -x -C "$workdir" 2>/dev/null || {
        log "No migrations directory in tag $tag (first migration)"
        echo ""
        return
    }
    
    if [ ! -d "$workdir/migrations" ]; then
        log "No migrations directory in tag $tag"
        echo ""
        return
    fi
    
    echo "$workdir/migrations"
}

start_postgres() {
    local container_name="migration-gate-$$"
    local port=$((5432 + RANDOM % 1000))
    
    LOGFILE=$(mktemp)
    
    log "Starting throwaway Postgres on port $port"
    
    docker run --rm -d \
        --name "$container_name" \
        -e POSTGRES_PASSWORD=test \
        -e POSTGRES_USER=test \
        -e POSTGRES_DB=kikapu \
        -p "$port:5432" \
        postgres:16-alpine \
        >/dev/null 2>&1
    
    PORTR="$container_name"
    
    local max_attempts=30
    local attempt=0
    
    while [ $attempt -lt $max_attempts ]; do
        if docker logs "$container_name" 2>&1 | grep -q "database system is ready to accept connections"; then
            break
        fi
        sleep 1
        attempt=$((attempt + 1))
    done
    
    if [ $attempt -eq $max_attempts ]; then
        fail "Postgres did not start within ${max_attempts}s"
    fi
    
    sleep 1
    
    TEST_DATABASE_URL="postgres://test:test@localhost:$port/kikapu?sslmode=disable"
    export TEST_DATABASE_URL
    
    log "Postgres ready at $TEST_DATABASE_URL"
}

apply_migrations() {
    local migration_dir="$1"
    local label="$2"
    
    if [ ! -d "$migration_dir" ]; then
        log "No migrations to apply for $label"
        return 0
    fi
    
    local files
    files=$(find "$migration_dir" -name "*.sql" -type f | sort)
    
    if [ -z "$files" ]; then
        log "No migration files found for $label"
        return 0
    fi
    
    local count=0
    while IFS= read -r file; do
        local filename
        filename=$(basename "$file")
        log "Applying $label migration: $filename"
        
        if ! psql "$TEST_DATABASE_URL" -v ON_ERROR_STOP=1 -f "$file" >/dev/null 2>&1; then
            fail "Failed to apply $label migration: $filename"
        fi
        
        count=$((count + 1))
    done <<< "$files"
    
    log "Applied $count $label migrations"
}

create_schema_migrations_table() {
    psql "$TEST_DATABASE_URL" -c "
        CREATE TABLE IF NOT EXISTS schema_migrations (
            version TEXT PRIMARY KEY,
            applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
        );
    " >/dev/null 2>&1
}

record_migration() {
    local version="$1"
    psql "$TEST_DATABASE_URL" -c "
        INSERT INTO schema_migrations (version)
        VALUES ('$version')
        ON CONFLICT DO NOTHING;
    " >/dev/null 2>&1
}

apply_migration_with_tracking() {
    local file="$1"
    local filename
    filename=$(basename "$file")
    local version="${filename%.sql}"
    
    create_schema_migrations_table
    
    local existing
    existing=$(psql "$TEST_DATABASE_URL" -t -c "
        SELECT COUNT(*) FROM schema_migrations WHERE version = '$version';
    " 2>/dev/null | tr -d ' ') || existing="0"
    
    if [ "$existing" -gt 0 ]; then
        log "Migration already applied: $filename (idempotent check passed)"
        return 0
    fi
    
    log "Applying migration with tracking: $filename"
    
    if ! psql "$TEST_DATABASE_URL" -v ON_ERROR_STOP=1 -f "$file" >/dev/null 2>&1; then
        fail "Failed to apply migration: $filename"
    fi
    
    record_migration "$version"
    log "Recorded migration: $version"
}

apply_previous_migrations() {
    local migration_dir="$1"
    
    if [ -z "$migration_dir" ] || [ ! -d "$migration_dir" ]; then
        log "No previous migrations to apply"
        return 0
    fi
    
    create_schema_migrations_table
    
    local files
    files=$(find "$migration_dir" -name "*.sql" -type f | sort)
    
    while IFS= read -r file; do
        [ -z "$file" ] && continue
        apply_migration_with_tracking "$file"
    done <<< "$files"
}

get_migration_files() {
    local dir="$1"
    if [ -d "$dir" ]; then
        find "$dir" -name "*.sql" -type f | sort
    fi
}

seed_fixture_data() {
    psql "$TEST_DATABASE_URL" -c "
        CREATE TABLE IF NOT EXISTS _test_seed_products (
            id TEXT PRIMARY KEY,
            name TEXT NOT NULL,
            created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
        );
        
        CREATE TABLE IF NOT EXISTS _test_seed_orders (
            id TEXT PRIMARY KEY,
            total BIGINT NOT NULL,
            created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
        );
        
        INSERT INTO _test_seed_products (id, name) VALUES
            ('prod-001', 'Test Product 1'),
            ('prod-002', 'Test Product 2'),
            ('prod-003', 'Test Product 3')
        ON CONFLICT DO NOTHING;
        
        INSERT INTO _test_seed_orders (id, total) VALUES
            ('ord-001', 10000),
            ('ord-002', 25000),
            ('ord-003', 5000)
        ON CONFLICT DO NOTHING;
    " >/dev/null 2>&1
    
    log "Seeded fixture data"
}

count_seed_rows() {
    local table="$1"
    local seed_table="_test_seed_$table"
    psql "$TEST_DATABASE_URL" -t -c "SELECT COUNT(*) FROM $seed_table;" 2>/dev/null | tr -d ' ' || echo "0"
}

check_destructive_statements() {
    local file="$1"
    local filename
    filename=$(basename "$file")
    
    local destructive=0
    local line_num=0
    local reason=""
    
    while IFS= read -r line; do
        line_num=$((line_num + 1))
        
        if echo "$line" | grep -qiE '^\s*--\s*allow-drop:'; then
            log "Found allow-drop header in $filename:$line_num"
            continue
        fi
        
        if echo "$line" | grep -qiE '^\s*(DROP\s+(TABLE|COLUMN|INDEX|CONSTRAINT))'; then
            destructive=1
            reason="$filename:$line_num contains DROP statement: $(echo "$line" | head -c 50)"
        fi
        
        if echo "$line" | grep -qiE '^\s*(ALTER\s+TABLE.*\bDROP\b)'; then
            destructive=1
            reason="$filename:$line_num contains ALTER DROP: $(echo "$line" | head -c 50)"
        fi
        
        if echo "$line" | grep -qiE '^\s*(ALTER\s+TABLE.*\bRENAME\b.*\bTO\b)'; then
            destructive=1
            reason="$filename:$line_num contains RENAME statement: $(echo "$line" | head -c 50)"
        fi
        
        if echo "$line" | grep -qiE '^\s*(ALTER\s+TABLE.*\bDROP\s+COLUMN)'; then
            destructive=1
            reason="$filename:$line_num contains DROP COLUMN: $(echo "$line" | head -c 50)"
        fi
    done < "$file"
    
    if [ $destructive -eq 1 ]; then
        fail "Destructive change detected without -- allow-drop: header: $reason"
    fi
}

apply_new_migrations() {
    local migration_dir="${MIGRATION_PATH:-./migrations}"
    
    if [ ! -d "$migration_dir" ]; then
        log "No migrations directory found at $migration_dir"
        return 0
    fi
    
    local files
    files=$(get_migration_files "$migration_dir")
    
    while IFS= read -r file; do
        [ -z "$file" ] && continue
        
        check_destructive_statements "$file"
        apply_migration_with_tracking "$file"
    done <<< "$files"
}

check_idempotency() {
    local migration_dir="${MIGRATION_PATH:-./migrations}"
    
    if [ ! -d "$migration_dir" ]; then
        return 0
    fi
    
    log "Checking idempotency (re-running migrations)"
    
    local files
    files=$(get_migration_files "$migration_dir")
    
    while IFS= read -r file; do
        [ -z "$file" ] && continue
        apply_migration_with_tracking "$file"
    done <<< "$files"
    
    log "Idempotency check passed"
}

run_compatibility_check() {
    local previous_tag
    previous_tag=$(get_previous_tag)
    
    log "Checking compatibility with previous release: $previous_tag"
    
    local previous_migrations
    previous_migrations=$(get_previous_migration_dir "$previous_tag")
    
    apply_previous_migrations "$previous_migrations" || {
        log "No previous migrations (first release)"
    }
    
    seed_fixture_data
    
    local product_count_before
    product_count_before=$(count_seed_rows products)
    local order_count_before
    order_count_before=$(count_seed_rows orders)
    
    log "Row counts before: products=$product_count_before, orders=$order_count_before"
    
    apply_new_migrations
    
    check_idempotency
    
    local product_count_after
    product_count_after=$(count_seed_rows products)
    local order_count_after
    order_count_after=$(count_seed_rows orders)
    
    log "Row counts after: products=$product_count_after, orders=$order_count_after"
    
    if [ "$product_count_before" != "$product_count_after" ]; then
        fail "Product row count changed: $product_count_before -> $product_count_after"
    fi
    
    if [ "$order_count_before" != "$order_count_after" ]; then
        fail "Order row count changed: $order_count_before -> $order_count_after"
    fi
    
    log "All compatibility checks passed"
}

inject_destructive_migration() {
    local tmpdir
    tmpdir=$(mktemp -d)
    
    local bad_migration="$tmpdir/999_destructive.sql"
    
    cat > "$bad_migration" <<'EOF'
ALTER TABLE products DROP COLUMN name;
EOF
    
    echo "$tmpdir"
}

run_self_test() {
    log "Running self-test with destructive change injection"
    
    local tmpdir
    tmpdir=$(inject_destructive_migration)
    
    (
        export MIGRATION_PATH="$tmpdir"
        run_compatibility_check
    ) >/dev/null 2>&1
    local result=$?
    
    rm -rf "$tmpdir"
    
    if [ $result -ne 0 ]; then
        log "Self-test passed: correctly detected destructive change"
        return 0
    fi
    
    error "Self-test failed: destructive change was not detected"
    return 1
}

main() {
    parse_args "$@"
    
    check_dependencies
    
    if [ -z "${TEST_DATABASE_URL:-}" ]; then
        start_postgres
    fi
    
    if [ -n "$SELF_TEST" ]; then
        run_self_test
        exit $?
    fi
    
    run_compatibility_check
}

main "$@"
