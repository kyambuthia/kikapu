# Failure Modes and Recovery Procedures

This document describes the failure scenarios, expected behavior, and operator actions for the Kikapu checkout system. All scenarios are tested with fault injection in `internal/checkout/fault_test.go` and `internal/events/fault_test.go`.

## Checkout Failure Scenarios

| Scenario | Expected Behavior | Code Reference | Test |
|---|---|---|---|
| Payment provider times out after stock reservation | Reservation released, no order created, 502 with `payment_timeout` | `internal/checkout/checkout.go:182` | `TestPaymentProviderTimeout_ReleasesReservations` |
| Payment declines | Reservation released, cart intact, 402 `payment_declined` | `internal/checkout/checkout.go:182` | `TestPaymentDeclined_ReleasesReservationsAndCartIntact` |
| Inventory commit fails after capture | Order marked `payment_captured_fulfilment_failed`, event emitted, alert metric incremented | `internal/checkout/checkout.go:187-198` | `TestInventoryCommitFailsAfterCapture_OperationFails` |
| Partial reservation failure (insufficient stock) | All previous reservations rolled back, no order, 409 conflict | `internal/checkout/checkout.go:169` | `TestPartialReservationFailure_RollsBackAllReservations` |

## Webhook/Event Delivery Failure Scenarios

| Scenario | Expected Behavior | Code Reference | Test |
|---|---|---|---|
| Webhook consumer returns 500 for 3 attempts then 200 | Delivered exactly once, attempts recorded | `internal/worker/deliverer.go:133-140` | `TestWebhookDelivery_RetriesOn5xx_ThenSucceeds` |
| Outbox append succeeds but delivery never runs | Event claimed and delivered on next tick | `internal/outbox/outbox.go:47-78` | `TestOutbox_AppendSucceeds_DeliveryNeverRuns` |
| Webhook consumer returns 4xx | Event moved to dead letter queue, no retry | `internal/worker/deliverer.go:136-137` | `TestWebhookDelivery_4xxGoesToDeadLetter` |
| Delivery attempts recorded | Attempts count incremented in outbox record | `internal/outbox/outbox.go:96-120` | `TestWebhookDelivery_RecordsAttempts` |

## Operator Actions

### Payment Timeout

**Symptoms**: Customer sees 502 error, money may or may not be charged.

**Automatic Recovery**: 
- Stock reservations automatically released
- No order created
- Cart remains intact for retry

**Manual Action Required**:
1. Check payment provider dashboard for transaction status
2. If charged but no order: Create manual order OR issue refund via payment provider
3. If not charged: Customer can retry checkout

**Monitoring**: Alert on checkout failures with `payment_timeout` error code

### Payment Declined

**Symptoms**: Customer sees 402 error, payment rejected by provider.

**Automatic Recovery**:
- Stock reservations automatically released
- Cart remains intact
- Customer can retry with different payment method

**Manual Action Required**: None - fully automatic

**Monitoring**: Track decline rates by provider and card type

### Inventory Commit Failure After Payment Capture

**Symptoms**: Payment captured but inventory not reserved, order in `payment_captured_fulfilment_failed` status.

**Automatic Recovery**:
- Order created with special status
- `order.fulfilment_failed` event emitted for downstream processing
- Stock remains available (not decremented)

**Manual Action Required**:
1. Query orders with status `payment_captured_fulfilment_failed`
2. Options:
   - **Retry**: Manually reserve inventory if available
   - **Refund**: Issue refund via payment provider
   - **Source from alternate location**: Adjust inventory and fulfill
   - **Backorder**: Create fulfilment delay, notify customer

**Monitoring**:
- Alert on `payment_captured_fulfilment_failed` orders
- Track inventory commit failure rate
- Monitor inventory accuracy (compare reserved vs. actually reserved)

### Webhook Delivery Failure (5xx)

**Symptoms**: Downstream service returns 500 errors, events not delivered.

**Automatic Recovery**:
- Exponential backoff retry: 1s, 5s, 30s, 5min
- Up to 8 retry attempts
- Event remains in pending status during retries

**Manual Action Required**:
1. Check downstream service health
2. If service down: Wait for recovery, events will auto-retry
3. If service up but failing: Debug integration issue
4. If max retries exceeded: Event goes to dead letter queue (see below)

**Monitoring**:
- Track webhook retry attempts
- Alert on high retry rates (indicates downstream issues)
- Monitor events stuck in pending status > 1 hour

### Webhook Dead Letter Queue (4xx or max retries)

**Symptoms**: Events in `dead_letter` status, will not be retried.

**Automatic Recovery**: None - events are terminal

**Manual Action Required**:
1. Query `outbox` table for `status = 'dead_letter'`
2. For each dead letter event:
   - Review error in `last_error` column
   - Fix integration issue if 4xx (bad request, auth failure, etc.)
   - If 5xx after max retries: Downstream service may be malfunctioning
3. Replay event:
   - Update status to `pending`
   - Set `next_attempt` to now
   - Worker will pick up on next tick
4. Or manually POST event to downstream service

**Monitoring**:
- Alert on events in dead letter queue
- Track dead letter rate by event type and subscriber

### Outbox Append Success, Delivery Never Runs

**Symptoms**: Events in `pending` status, `next_attempt` NULL, worker not picking up.

**Automatic Recovery**: Once worker runs, events will be claimed and delivered

**Manual Action Required**:
1. Check worker health (process running? CPU/memory OK?)
2. Check `next_attempt` - if in future, event won't be claimed yet
3. If worker is dead: Restart worker process
4. If worker hung: Check for deadlocks, restart process

**Monitoring**:
- Track events in pending status
- Alert if pending count > threshold or > 1 hour old
- Monitor worker process health

### Partial Reservation Failure

**Symptoms**: Customer sees 409 error, some items unavailable.

**Automatic Recovery**:
- All stock reservations rolled back atomically
- No partial order created
- Cart remains intact

**Manual Action Required**: None - fully automatic

**Monitoring**: Track partial reservation failures (indicates stock accuracy issues)

## Testing Compensations

All failure compensations are tested with fault injection. Tests verify:

1. **Stock levels match expectations** - Tests assert `OnHand` and `Reserved` counts, not just errors
2. **Order counts are correct** - Tests verify number of orders created
3. **Release calls are made** - Tests check `ReleaseCallCount()` to prove compensation

### Example: Proving Release is Required

```go
// This test will FAIL if the releaseAll call is removed
func TestPaymentProviderTimeout_ReleasesReservations(t *testing.T) {
    // ... setup ...
    
    _, err := svc.PlaceOrder("cart001", "idem-key-timeout")
    if err == nil {
        t.Fatal("Expected error on payment timeout")
    }
    
    // Verify stock released (test fails if releaseAll not called)
    if levels[0].Reserved != 0 {
        t.Errorf("Reserved = %d, want 0 (reservation should be released)", levels[0].Reserved)
    }
    
    if inventoryStore.ReleaseCallCount() != 1 {
        t.Errorf("ReleaseCallCount = %d, want 1", inventoryStore.ReleaseCallCount())
    }
}
```

To verify compensations work:
1. Run `go test -race ./internal/checkout`
2. Temporarily comment out `s.releaseAll(ctx, reservations)` in checkout.go:182
3. Run tests - they should FAIL
4. Restore the code
5. Run tests - they should PASS

## Database Recovery (Future: PostgreSQL Transactions)

When PostgreSQL lands, the system will ensure:

- All operations in a single transaction
- Automatic rollback on any error
- No partial orders created
- Stock reservations atomic with order creation

This will be tested by:
- Injecting DB errors mid-checkout
- Verifying transaction rollback
- Asserting no partial data persisted

## Monitoring & Alerting

Key metrics to monitor:

| Metric | Threshold | Action |
|---|---|---|
| `kikapu_checkout_total{result="payment_timeout"}` | > 1% of checkouts | Investigate payment provider connectivity |
| `kikapu_checkout_total{result="payment_declined"}` | > 10% of checkouts | Review fraud rules, card acceptance |
| Orders with status `payment_captured_fulfilment_failed` | Any | Manual intervention required |
| Webhook retry attempts | > 5% of events | Check downstream service health |
| Events in dead letter queue | Any | Review integration, replay if needed |
| Events in pending > 1 hour | Any | Check worker health, worker may be dead |

## Incident Response Playbook

1. **Detect**: Monitor alerts for failure scenarios
2. **Triage**: Check severity and impact radius
3. **Contain**: If systemic, consider:
   - Enable maintenance mode
   - Disable faulty payment provider
   - Throttle traffic
4. **Recover**: Follow operator actions above
5. **Postmortem**: Document root cause, improve tests, add monitoring
