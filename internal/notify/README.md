# notify

Notification adapters for the kikapu commerce engine using the outbox pattern.

## Templates

Notification templates are stored in `internal/notify/templates/`:

- `order.paid.txt` - Plain text order payment notification
- `order.paid.html` - HTML order payment notification
- `order.fulfilled.txt` - Plain text order fulfillment notification
- `order.fulfilled.html` - HTML order fulfillment notification
- `order.refunded.txt` - Plain text order refund notification
- `order.refunded.html` - HTML order refund notification

Each template supports:
- `{{.OrderID}}` - Order identifier
- `{{.CustomerName}}` - Customer name
- `{{.Amount}}` - Payment/fulfillment amount
- `{{.TransactionID}}` - Transaction reference
- `{{.Items}}` - Array of order items (for fulfillment)
- `{{.ReturnAddress}}` - Delivery address
- `{{.FulfillmentDate}}` - Date of fulfillment
- `{{.OccurredAt}}` - Timestamp

## Providers

### SMTP Email Adapter

Send email notifications via SMTP.

Configuration:
```go
cfg := SMTPConfig{
    Server:   "smtp.example.com",
    Port:     587,
    Username: "user",
    Password: "pass",
}
sender := NewSMTP(cfg)
```

Features:
- Plain text and HTML variants
- Polls from client pool (connection reuse)
- Fast exit on template errors

### Africa's Talking SMS Adapter

Send SMS notifications via Africa's Talking API.

Configuration:
```go
cfg := AfricaTalkingConfig{
    Username: "username",
    APIKey:   "api_key",
    SenderID: "kikapu",
}
sender := NewAfricaTalking(cfg)
```

Features:
- Kenya phone number validation (`+254` format)
- Normalizes to 12-digit format
- Phone number redaction in logs
- Rejects non-KE numbers

## Usage

### Consumer

```go
logger := zap.NewProduction()
sender := NewSMTP(cfg)
consumer := NewConsumer(logger, sender, 3)

events := make(chan events.Event)
outbox := NewInMemoryOutbox()

if err := consumer.Consume(context.Background(), events, outbox); err != nil {
    logger.Error("consumer error", zap.Error(err))
}
```

### Emit Notifications

Notifications are emitted via events:

```go
event, _ := events.NewEvent(events.EventTypeOrderPaid, orderID, data)
outbox.Append(ctx, tx, event)
```

## Testing

Template rendering tests use golden files:

```bash
go test ./internal/notify/...
```

Run:
```bash
make test
```

## Architecture

- **Single Binary**: All code in `internal/notify/`
- **Outbox-Driven**: Notifications via `events.Event`
- **No In-Process State**: Uses PostgreSQL outbox for persistence
- **Failure Retry**: Provider 500 → Outbox retry with exponential backoff
- **Fast Exit**: Unknown event types are ignored
- **Redacted Logs**: Phone numbers are redacted for SMS logs
