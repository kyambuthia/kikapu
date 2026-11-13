package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/kyambuthia/kikapu/internal/events"
	"github.com/kyambuthia/kikapu/internal/platform/obs"
)

var (
	ErrInvalidRecipient = errors.New("notify: invalid recipient")
	ErrUnsupportedEvent = errors.New("notify: unsupported event type")
)

type Channel string

const (
	ChannelEmail Channel = "email"
	ChannelSMS   Channel = "sms"
)

type Message struct {
	To      string
	Channel Channel
	Subject string
	Body    string
}

type Sender interface {
	Send(ctx context.Context, msg Message) error
}

// LogSender records notifications via structured logging with PII redaction.
type LogSender struct {
	logger *slog.Logger
}

func NewLogSender(logger *slog.Logger) *LogSender {
	if logger == nil {
		logger = slog.Default()
	}
	return &LogSender{logger: logger}
}

func (s *LogSender) Send(ctx context.Context, msg Message) error {
	if strings.TrimSpace(msg.To) == "" {
		return ErrInvalidRecipient
	}
	s.logger.InfoContext(ctx, "notification dispatched",
		"channel", msg.Channel,
		"recipient", maskRecipient(msg.Channel, msg.To),
		"subject", msg.Subject,
	)
	return nil
}

type orderNotificationPayload struct {
	OrderID  string `json:"order_id"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	Total    int64  `json:"total"`
	Currency string `json:"currency"`
}

// Consumer translates domain events into outbound notifications.
type Consumer struct {
	sender Sender
}

func NewConsumer(sender Sender) *Consumer {
	return &Consumer{sender: sender}
}

func (c *Consumer) HandleEvent(ctx context.Context, e events.Event) error {
	var payload orderNotificationPayload
	if len(e.Data) > 0 {
		if err := json.Unmarshal(e.Data, &payload); err != nil {
			return fmt.Errorf("notify: decode event %s: %w", e.ID, err)
		}
	}
	if payload.OrderID == "" {
		payload.OrderID = e.AggregateID
	}

	var subject, body string
	switch e.Type {
	case events.EventTypeOrderCreated:
		subject = fmt.Sprintf("Order %s received", payload.OrderID)
		body = fmt.Sprintf("Your order %s has been received.", payload.OrderID)
	case events.EventTypeOrderPaid:
		subject = fmt.Sprintf("Payment confirmed for order %s", payload.OrderID)
		body = fmt.Sprintf("Payment for order %s has been confirmed.", payload.OrderID)
	case events.EventTypeOrderFulfilled:
		subject = fmt.Sprintf("Order %s shipped", payload.OrderID)
		body = fmt.Sprintf("Your order %s has been fulfilled.", payload.OrderID)
	case events.EventTypeOrderRefunded:
		subject = fmt.Sprintf("Refund processed for order %s", payload.OrderID)
		body = fmt.Sprintf("A refund for order %s has been processed.", payload.OrderID)
	default:
		return ErrUnsupportedEvent
	}

	if payload.Email != "" {
		if err := c.sender.Send(ctx, Message{
			To:      payload.Email,
			Channel: ChannelEmail,
			Subject: subject,
			Body:    body,
		}); err != nil {
			return err
		}
	}
	if payload.Phone != "" {
		if err := c.sender.Send(ctx, Message{
			To:      payload.Phone,
			Channel: ChannelSMS,
			Subject: subject,
			Body:    body,
		}); err != nil {
			return err
		}
	}
	return nil
}

func maskRecipient(ch Channel, recipient string) string {
	if ch == ChannelSMS {
		return obs.RedactPhoneNumber(recipient)
	}
	at := strings.Index(recipient, "@")
	if at <= 1 {
		return "***"
	}
	return recipient[:1] + "***" + recipient[at:]
}
