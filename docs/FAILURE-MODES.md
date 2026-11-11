# Failure Modes and Operator Actions

This document describes the failure scenarios tested in the fault injection tests and the appropriate operator responses.

## Checkout Failure Scenarios

### 1. Payment Provider Timeout After Stock Reservation

**Scenario**: Payment provider times out after stock has been reserved but before payment capture.

**Expected Behavior**:
- Reservation released automatically
- No order created
- HTTP 502 with error code `payment_timeout`
- Stock levels restored to original state
- Cart remains intact (not cleared)

**Compensation**: Automatic - `releaseAll()` called in checkout service

**Operator Action**: Retry
- Customer can retry the checkout
- No manual intervention required
- Monitor for repeated timeouts - may indicate payment provider issues

**Impact**: Transient failure, customer impact minimal

---

### 2. Payment Declined

**Scenario**: Payment is declined by payment provider (insufficient funds, fraud detection, etc.)

**Expected Behavior**:
- Reservation released automatically
- Cart remains intact with all items
- HTTP 402 with error code `payment_declined`
- Stock levels restored

**Compensation**: Automatic - `releaseAll()` called in checkout service

**Operator Action**: Retry
- Customer should use different payment method
- No manual intervention required
- Monitor decline rates for anomalies

**Impact**: Customer-facing, but self-service resolution available

---

### 3. Inventory Commit Fails After Payment Capture

**Scenario**: Payment captured successfully but inventory commit operation fails.

**Expected Behavior**:
- Order created with status indicating fulfillment issue
- Event emitted for alerting
- Alert metric incremented
- HTTP 200 (order created, but needs attention)

**Compensation**: Partial - payment captured, stock committed if commit succeeds

**Operator Action**: Manual reconcile
- Alert triggered automatically
- Investigate inventory system
- Manually reserve stock or cancel order with refund
- Contact fulfillment team

**Impact**: Requires operator intervention, customer order at risk

---

### 4. Partial Reservation Failure

**Scenario**: Multiple items in cart, one item fails to reserve (insufficient stock).

**Expected Behavior**:
- All successfully reserved items released
- No order created
- HTTP 400 with error indicating insufficient stock
- All stock levels restored

**Compensation**: Automatic - rollback all successful reservations

**Operator Action**: Retry
- Customer can adjust cart quantities
- No manual intervention required
- Monitor stock levels and adjust reorder points

**Impact**: Customer-facing, but clear error message provided

---

### 5. DB Error Mid-Checkout (PostgreSQL)

**Scenario**: Database error occurs during checkout process (when PostgreSQL is implemented).

**Expected Behavior**:
- Transaction rolled back
- No partial order created
- No reservations persisted
- HTTP 500 with error code `internal_error`

**Compensation**: Automatic - database transaction rollback

**Operator Action**: Retry
- Customer can retry checkout
- Investigate database connectivity/logs
- May need to check for connection pool exhaustion

**Impact**: Transient failure, but may indicate systemic issues

---

## Webhook Delivery Failure Scenarios

### 6. Webhook Consumer Returns 500

**Scenario**: Webhook endpoint returns HTTP 500 for first 3 attempts, then succeeds on 4th.

**Expected Behavior**:
- Event delivered exactly once (no duplicates)
- All attempts recorded in outbox
- Exponential backoff between retries
- Event marked as sent after success

**Compensation**: Automatic retry with backoff

**Operator Action**: Monitor
- Check webhook endpoint health
- Review webhook consumer logs
- May need to contact integration partner

**Impact**: Delayed notification, but eventual delivery guaranteed

---

### 7. Webhook Consumer Returns 4xx

**Scenario**: Webhook endpoint returns HTTP 400/404/etc.

**Expected Behavior**:
- Event moved to dead letter queue immediately
- No retries (permanent failure)
- Event marked as `dead_letter` status

**Compensation**: None - permanent failure

**Operator Action**: Manual reconcile
- Investigate webhook endpoint configuration
- Check payload format compatibility
- May need to reprocess event manually after fix

**Impact**: Requires manual intervention, potential data inconsistency

---

### 8. Outbox Append Succeeds, Delivery Never Runs

**Scenario**: Event appended to outbox but delivery worker doesn't process it immediately.

**Expected Behavior**:
- Event remains in `pending` status in outbox
- Event claimed and delivered on next worker tick
- Event marked as sent when successful

**Compensation**: Automatic - delivered on next tick

**Operator Action**: Monitor
- Check delivery worker status
- Verify worker is running
- Check for stuck events in outbox

**Impact**: Delayed delivery, but not lost

---

## Monitoring and Alerting

### Metrics to Watch

- `kikapu_checkout_total{result="failed"}` - Checkout failure rate
- `kikapu_payment_failures_total` - Payment provider issues
- `kikapu_outbox_pending_events` - Events awaiting delivery
- `kikapu_webhook_failures_total` - Webhook delivery failures

### Alert Thresholds

- **Payment timeout rate > 1%**: Investigate payment provider
- **Inventory commit failures > 0.1%**: Check inventory system
- **Webhook dead letters > 1/hour**: Review webhook endpoints
- **Outbox pending > 100**: Check delivery worker health

---

## Test Coverage

All scenarios above have automated tests in:
- `internal/checkout/fault_test.go` - Checkout failure scenarios
- `internal/events/fault_test.go` - Webhook delivery scenarios

### Running Tests

```bash
go test -race ./internal/checkout
go test -race ./internal/events
```

### Proving Compensations Work

Each test validates:
1. **Stock levels match** - No leaked reservations
2. **Order counts correct** - No duplicate or partial orders
3. **Error codes specific** - Correct HTTP status and error codes
4. **Compensations executed** - `Release()` calls verified

Tests will fail if compensation logic is removed.
