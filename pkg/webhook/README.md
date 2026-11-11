# Webhook SDK for Kikapu

A simple, ten-line webhook consumer SDK for Kikapu events with signature verification and idempotent handling.

## Get Started

```go
package main

import (
	"context"
	"net/http"
	"time"

	"github.com/kyambuthia/kikapu/pkg/webhook"
)

func main() {
	secret := "your-webhook-secret"

	handler := webhook.Handler{
		Secret:     secret,
		Tolerance:  5 * time.Minute,
		Idempotent: IdempotentFunc(func(ctx context.Context, info webhook.DeliveryInfo) error {
			return processEvent(ctx, info.DeliveryID, info.Event)
		}),
		StoreKey: func(ctx context.Context, deliveryID string) error {
			return saveToDatabase(deliveryID)
		},
	}

	http.HandleFunc("/webhook", handler.ServeHTTP)
	http.ListenAndServe(":8080", nil)
}

func IdempotentFunc(f func(ctx context.Context, info webhook.DeliveryInfo) error) func(context.Context, webhook.DeliveryInfo) error {
	return f
}

func processEvent(ctx context.Context, deliveryID, eventType string) error {
	// Your event processing logic here
	// If the event is already processed (same deliveryID),
	// this function could return early or the SDK handles idempotency
	return nil
}

func saveToDatabase(deliveryID string) error {
	// Save the delivery ID to ensure idempotency
	// Implement your database persistence here
	return nil
}
```

## Features

- **Signature Verification**: Validates HMAC-SHA256 signatures
- **Idempotent Handling**: Prevents duplicate processing of the same event
- **Delivery IDs**: Tracks processed delivery IDs in a store
- **Error Handling**: Clear HTTP error responses with status codes

## How It Works

1. **Receive**: HTTP POST request with webhook payload
2. **Verify**: Check signature header (`X-Kikapu-Signature`)
3. **Dedup**: Check delivery ID (`X-Kikapu-Delivery`) against store
4. **Process**: Run your idempotent event processing
5. **Acknowledge**: Return 200 OK if processed

## Signature Format

```
X-Kikapu-Signature: t=<unix_epoch_s>,v1=<hmac_sha256_signature>
```

The signature is computed as:
```
HMAC-SHA256(webhook_secret, timestamp + "." + body)
```

## Managing Idempotency

Implement your idempotent store to persist delivery IDs:

```go
type DatabaseStore struct {
    db *sql.DB
}

func (s *DatabaseStore) Exists(ctx context.Context, id string) (bool, error) {
    var exists bool
    err := s.db.QueryRowContext(ctx,
        "SELECT EXISTS(SELECT 1 FROM processed_deliveries WHERE delivery_id = ?)",
        id,
    ).Scan(&exists)
    return exists, err
}

func (s *DatabaseStore) Save(ctx context.Context, id string) error {
    _, err := s.db.ExecContext(ctx,
        `INSERT INTO processed_deliveries (delivery_id, processed_at)
         VALUES (?, NOW())
         ON CONFLICT (delivery_id) DO NOTHING`,
        id,
    )
    return err
}
```

## Error Handling

- **401 Invalid Signature**: Signature verification failed
- **401 Stale Timestamp**: Event older than tolerance period
- **500 Internal Server Error**: Idempotency check or store save failed

## License

MIT
