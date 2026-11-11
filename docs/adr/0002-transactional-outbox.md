# ADR-0002: Transactional Outbox Pattern

## Status

Accepted (implementation planned in Phase 5)

## Context

Kikapu needs to notify external systems about domain events:
- Order created → fulfillment system
- Payment captured → accounting system
- Inventory reserved → warehouse management

### Naive Approach: Direct Publishing

```go
// WRONG: Publish after transaction
func PlaceOrder(...) {
    tx.Begin()
    order := createOrder()
    inventory.Reserve()
    payment.Charge()
    tx.Commit()
    
    // RISK: This can fail after commit
    webhook.Publish("order.created", order) // network failure?
}
```

**Problem**: If webhook publish fails after transaction commits, order exists but external systems are not notified. System state is inconsistent.

### Alternative: Message Broker

```go
// BETTER: Publish to broker in transaction
func PlaceOrder(...) {
    tx.Begin()
    order := createOrder()
    broker.Publish("order.created", order) // in transaction
    tx.Commit()
}
```

**Problems with message brokers**:
1. **Complexity**: Requires RabbitMQ, Kafka, or similar
2. **Operational overhead**: Another service to monitor, scale, backup
3. **Dual-write problem**: Can't publish to broker in same DB transaction
4. **Single binary**: Violates kikapu's single-binary architecture

## Decision

We implement the **Transactional Outbox Pattern**.

Events are written to an `outbox` table in the same database transaction as the domain state change. A separate worker polls the `outbox` table and publishes events.

### Architecture

```
┌─────────────┐
│ API Server  │
│             │
│ 1. Order    │
│ 2. Payment  │
│ 3. Outbox   │ ←──── Same transaction
│    INSERT   │
└──────┬──────┘
       │
       ▼
  ┌────────────┐
  │ PostgreSQL │
  │            │
  │ - orders   │
  │ - outbox   │ ← Events stored here
  └─────┬──────┘
        │
        ▼
  ┌──────────────┐
  │ Worker Mode  │
  │              │
  │ 4. Poll      │
  │ 5. Publish   │ ← Retry on failure
  │ 6. Mark done │
  └──────┬───────┘
         │
         ▼
   External Systems
   (webhooks)
```

### Implementation

#### 1. Outbox Table Schema

```sql
CREATE TABLE outbox (
    id            TEXT PRIMARY KEY,
    type          TEXT NOT NULL,           -- "order.created"
    aggregate_id  TEXT NOT NULL,           -- order_id
    payload       JSONB NOT NULL,          -- event data
    created_at    TIMESTAMPTZ NOT NULL,
    published_at  TIMESTAMPTZ,
    attempts      INT DEFAULT 0,
    next_retry    TIMESTAMPTZ
);
```

#### 2. Event Creation in Transaction

```go
func (s *Service) PlaceOrder(...) {
    tx := db.Begin()
    
    order := createOrder(tx)
    inventory.Reserve(tx)
    payment.Charge(tx)
    
    // SAME TRANSACTION
    event := Event{
        ID:          generateID(),
        Type:        "order.created",
        AggregateID: order.ID,
        Payload:     order,
        CreatedAt:   time.Now(),
    }
    tx.Insert("outbox", event)
    
    tx.Commit() // Atomic: order + event
}
```

#### 3. Worker Publishing

```go
func (w *Worker) Run() {
    for {
        events := db.Query("SELECT * FROM outbox WHERE published_at IS NULL AND next_retry < NOW()")
        
        for _, event := range events {
            err := webhook.Publish(event.Type, event.Payload)
            if err != nil {
                // Retry with exponential backoff
                event.Attempts++
                event.NextRetry = time.Now().Add(backoff(event.Attempts))
                db.Update(event)
            } else {
                // Mark as published
                event.PublishedAt = time.Now()
                db.Update(event)
            }
        }
        
        time.Sleep(pollInterval)
    }
}
```

### Event Envelope

All events use this format:

```json
{
  "id": "evt_1705315200",
  "type": "order.created",
  "version": 1,
  "occurred_at": "2026-11-11T09:00:00Z",
  "data": {
    "order_id": "ord_abc123",
    "total": {"value": 2999, "currency": "USD"},
    "items": [...]
  }
}
```

## Consequences

### Positive

- **Atomicity**: Event exists iff state change committed (INV-4)
- **Reliability**: Worker retries failed publishes
- **Ordering**: Events can be ordered by created_at
- **Audit trail**: Outbox is a log of all events
- **Single binary**: No external message broker
- **Operational simplicity**: Only PostgreSQL to manage

### Negative

- **Polling overhead**: Worker polls periodically (not push-based)
- **Latency**: Events published after polling interval (configurable)
- **Storage**: Outbox table grows (need cleanup after retention period)
- **Single point of failure**: PostgreSQL is single point of failure (mitigated by backups)

### Rejected Alternatives

#### Alternative 1: Publish Directly After Commit

```go
tx.Commit()
webhook.Publish(event) // RISK
```

**Why rejected**: If webhook fails, event is lost. System state inconsistent.

#### Alternative 2: Message Broker (RabbitMQ, Kafka)

```go
broker.Publish(event) // in transaction?
```

**Why rejected**:
- Can't participate in DB transaction (dual-write problem)
- Additional operational complexity
- Violates single-binary, Postgres-only architecture

#### Alternative 3: Change Data Capture (Debezium)

```go
// Read from Postgres WAL
```

**Why rejected**: 
- Complex setup (Kafka Connect, Debezium)
- Latency in processing WAL
- Overkill for webhook publishing

## Retry Strategy

Worker uses exponential backoff:

- **Attempt 1**: Immediate retry
- **Attempt 2**: Retry after 1 minute
- **Attempt 3**: Retry after 5 minutes
- **Attempt 4**: Retry after 15 minutes
- **Attempt 5**: Mark as failed (dead-letter queue)

**Configuration**: `KIKAPU_WEBHOOK_RETRY_MAX=5`, `KIKAPU_WEBHOOK_RETRY_BACKOFF=exponential`

## Implementation Plan

- **Phase 5**: Events & Webhooks
- **Estimated effort**: 2-3 weeks
- **Dependencies**: Phase 3 (PostgreSQL, transactions)

## References

- [Transaction Outbox Pattern](https://microservices.io/patterns/data/transactional-outbox.html)
- [Debezium Outbox Pattern](https://debezium.io/blog/2019/10/01/saga-pattern-implementing-with-debezium/)
- [Event Sourcing vs Outbox Pattern](https://event-driven.io/en/outbox_pattern/)
