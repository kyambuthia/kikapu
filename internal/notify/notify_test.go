package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/kyambuthia/kikapu/internal/events"
)

type recordingSender struct {
	messages []Message
	err      error
}

func (r *recordingSender) Send(_ context.Context, msg Message) error {
	if r.err != nil {
		return r.err
	}
	r.messages = append(r.messages, msg)
	return nil
}

func TestConsumer_HandleEvent(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		payload   orderNotificationPayload
		wantCount int
		wantErr   error
	}{
		{
			name:      "order created email and sms",
			eventType: events.EventTypeOrderCreated,
			payload:   orderNotificationPayload{OrderID: "ord-1", Email: "alice@example.com", Phone: "+254712345678"},
			wantCount: 2,
		},
		{
			name:      "order paid email only",
			eventType: events.EventTypeOrderPaid,
			payload:   orderNotificationPayload{OrderID: "ord-2", Email: "bob@example.com"},
			wantCount: 1,
		},
		{
			name:      "order fulfilled",
			eventType: events.EventTypeOrderFulfilled,
			payload:   orderNotificationPayload{OrderID: "ord-3", Phone: "+254712345678"},
			wantCount: 1,
		},
		{
			name:      "order refunded",
			eventType: events.EventTypeOrderRefunded,
			payload:   orderNotificationPayload{OrderID: "ord-4", Email: "carol@example.com"},
			wantCount: 1,
		},
		{
			name:      "unsupported event",
			eventType: "unknown.event",
			payload:   orderNotificationPayload{OrderID: "ord-5"},
			wantErr:   ErrUnsupportedEvent,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sender := &recordingSender{}
			consumer := NewConsumer(sender)

			raw, err := json.Marshal(tc.payload)
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}

			err = consumer.HandleEvent(context.Background(), events.Event{
				ID:          "evt-1",
				Type:        tc.eventType,
				AggregateID: tc.payload.OrderID,
				Data:        raw,
			})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("HandleEvent() error = %v, want %v", err, tc.wantErr)
			}
			if len(sender.messages) != tc.wantCount {
				t.Fatalf("sent %d messages, want %d", len(sender.messages), tc.wantCount)
			}
		})
	}
}

func TestLogSender(t *testing.T) {
	sender := NewLogSender(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := sender.Send(context.Background(), Message{}); !errors.Is(err, ErrInvalidRecipient) {
		t.Fatalf("Send(empty) error = %v, want %v", err, ErrInvalidRecipient)
	}
	if err := sender.Send(context.Background(), Message{
		To:      "alice@example.com",
		Channel: ChannelEmail,
		Subject: "Test",
		Body:    "Hello",
	}); err != nil {
		t.Fatalf("Send(valid) error = %v", err)
	}
}
