# ADR-0002: Transactional Outbox

Status: proposed
Date: 2026-11-11

## Context

Kikapu needs to notify external systems (webhooks, analytics, notifications) when domain events occur (order created, payment captured, stock reserved). The challenge: how to ensure events are published exactly once, even when downstream systems are temporarily unavailable?

Naive approaches fail:
1. **Publish after transaction**: If publish fails, event is lost
2. **Background publisher**: No coordination with transaction, may publish uncommitted state

## Decision

Implement the transactional outbox pattern (planned Phase 5):

1. **Outbox table**: Same database as domain state
2. **Insert event in same transaction**: Atomic with state change
3. **Background worker**: Polls outbox, publishes, marks published
4. **Idempotent consumers**: Webhooks have delivery IDs

Schema:
```sql
CREATE TABLE outbox (
    id            UUID PRIMARY KEY,
    type          TEXT NOT NULL,
    payload       JSONB NOT NULL,
    created_at    TIMESTAMP NOT NULL,
    published_at  TIMESTAMP
);

CREATE INDEX idx_outbox_unpublished ON outbox(created_at) 
WHERE published_at IS NULL;
```

## Consequences

**Positive**:
- Exactly-once event delivery (with idempotent consumers)
- No lost events on publish failure
- Ordering preserved (created_at)
- Dead-letter queue for failed deliveries

**Negative**:
- Polling adds latency (configurable, default: 1s)
- Outbox table grows (requires cleanup of published events)
- Requires transaction support (Phase 3)

**Rejected**:
- **Direct publish**: No transaction, events can be lost
- **Two-phase commit**: Complex, not supported by all databases
- **Kafka/CDC**: Overkill for webhook use case, adds operational complexity

## References

- ROADMAP.md Phase 5
- `internal/events/` (planned)
- INV-4 in `docs/INVARIANTS.md`
