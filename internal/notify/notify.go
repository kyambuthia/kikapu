package notify

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/kyambuthia/kikapu/internal/events"
)

type Config struct {
	EmailProvider    string
	SMTPHost         string
	SMTPPort         int
	SMTPUsername     string
	SMTPPassword     string
	SMTPFrom         string
	AfricaTalkingAPIKey string
	AfricaTalkingSenderID string
	AfricaTalkingUsername string
	WebhookURL      string
	WebhookAuthToken string
}

type Service struct {
	config           Config
	sender          Sender
	tmpls            *Templates
	log              Logger
	normalizer       *normalizer
}

func NewService(config Config) *Service {
	sender, err := createSender(config)
	if err != nil {
		log.Fatalf("failed to create sender: %v", err)
	}
	
	return &Service{
		config:     config,
		sender:     sender,
		tmpls:      NewTemplates(),
		log:        &defaultLogger{},
		normalizer: NewNormalizer(),
	}
}

func (s *Service) Dispatcher(ctx context.Context) <-chan events.Event {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-s.eventsChan():
				go s.handleEvent(event)
			}
		}
	}()
	return s.eventsChan()
}

func (s *Service) RegisterTemplate(name, path string) error {
	if err := s.tmpls.Load(path, name); err != nil {
		return fmt.Errorf("register template %s: %w", name, err)
	}
	return nil
}

func (s *Service) Email(ctx context.Context, to, template string, vars map[string]any) error {
	normalized, err := s.normalizer.Normalize(to)
	if err != nil {
		return fmt.Errorf("normalize email: %w", err)
	}
	
	email, ok := s.sender.(*SMTP)
	if !ok {
		return fmt.Errorf("email sender not configured")
	}
	
	if s.config.EmailProvider == "smtp" {
		return email.Send(ctx, Message{
			To:       to,
			Template: template,
			Vars:     vars,
		})
	}
	
	return fmt.Errorf("email provider not available")
}

func (s *Service) SMS(ctx context.Context, to, template string, vars map[string]any) error {
	normalized, err := s.normalizer.Normalize(to)
	if err != nil {
		return fmt.Errorf("normalize phone: %w", err)
	}
	
	sms, ok := s.sender.(*AfricaTalking)
	if !ok {
		return fmt.Errorf("SMS sender not configured")
	}
	
	return sms.Send(ctx, Message{
		To:       normalized,
		Template: template,
		Vars:     vars,
	})
}

func (s *Service) Consume(ctx context.Context, events <-chan events.Event) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-events:
			if !ok {
				return nil
			}
			
			if err := s.handleEvent(ctx, event); err != nil {
				log.Printf("failed to handle event %s: %v", event.ID, err)
				return fmt.Errorf("handle event %s: %w", event.ID, err)
			}
			
			log.Printf("handled event: %s", event.Type)
		}
	}
}

func (s *Service) handleEvent(ctx context.Context, event events.Event) error {
	switch event.Type {
	case events.EventTypeOrderPaid:
		return s.handleOrderPaid(ctx, event)
	case events.EventTypeOrderFulfilled:
		return s.handleOrderFulfilled(ctx, event)
	case events.EventTypeOrderRefunded:
		return s.handleOrderRefunded(ctx, event)
	default:
		log.Printf("ignoring unknown event: %s", event.Type)
		return nil
	}
}

func (s *Service) eventsChan() <-chan events.Event {
	if s.eventChan == nil {
		s.eventChan = make(chan events.Event, 100)
	}
	return s.eventChan
}

func (s *Service) handleOrderPaid(ctx context.Context, event events.Event) error {
	// Extract order data from event
	// In production, you would query the database for order details
	
	// Prepare variables for template
	vars := map[string]any{
		"OrderID":   extractOrderID(event),
		"Total":     "1000",
		"Currency":  "KES",
		"CapturedAt": event.OccurredAt.Format(time.RFC3339),
		"Items": []map[string]any{
			{"Product": "Wireless Mouse", "Quantity": 1, "Price": "2999"},
		},
	}
	
	// Send email notification
	if err := s.Email(ctx, "customer@example.com", "order.paid", vars); err != nil {
		log.Printf("failed to send email: %v", err)
	}
	
	// Send SMS notification (if configured)
	if err := s.SMS(ctx, "+254712345678", "order.paid", vars); err != nil {
		log.Printf("failed to send SMS: %v", err)
	}
	
	return nil
}

func (s *Service) handleOrderFulfilled(ctx context.Context, event events.Event) error {
	// Extract order data from event
	// In production, you would query the database for order details
	
	// Prepare variables for template
	vars := map[string]any{
		"OrderID":      extractOrderID(event),
		"Status":       "shipped",
		"Carrier":      "Courier",
		"TrackingNumber": "TRK123456",
		"EstimatedDelivery": time.Now().AddDate(0, 0, 5).Format(time.RFC3339),
	}
	
	// Send email notification
	if err := s.Email(ctx, "customer@example.com", "order.fulfilled", vars); err != nil {
		log.Printf("failed to send email: %v", err)
	}
	
	// Send SMS notification (if configured)
	if err := s.SMS(ctx, "+254712345678", "order.fulfilled", vars); err != nil {
		log.Printf("failed to send SMS: %v", err)
	}
	
	return nil
}

func (s *Service) handleOrderRefunded(ctx context.Context, event events.Event) error {
	// Extract order data from event
	// In production, you would query the database for order details
	
	// Prepare variables for template
	vars := map[string]any{
		"RefundID":  "REF123456",
		"OrderID":   extractOrderID(event),
		"Amount":    "500",
		"Currency":  "KES",
		"ProcessedAt": event.OccurredAt.Format(time.RFC3339),
	}
	
	// Send email notification
	if err := s.Email(ctx, "customer@example.com", "order.refunded", vars); err != nil {
		log.Printf("failed to send email: %v", err)
	}
	
	// Send SMS notification (if configured)
	if err := s.SMS(ctx, "+254712345678", "order.refunded", vars); err != nil {
		log.Printf("failed to send SMS: %v", err)
	}
	
	return nil
}

func extractOrderID(event events.Event) string {
	// In production, you would parse the event.Data JSON
	// For now, return a placeholder
	if event.AggregateID != "" {
		return event.AggregateID
	}
	return "ORD123456"
}

type defaultLogger struct{}

func (l *defaultLogger) Logf(format string, args ...any) {
	log.Printf(format, args...)
}

func createSender(config Config) (Sender, error) {
	switch config.EmailProvider {
	case "smtp":
		return NewSMTP(
			config.SMTPHost,
			config.SMTPPort,
			config.SMTPUsername,
			config.SMTPPassword,
			config.SMTPFrom,
			true,
		), nil
		
	case "africastalking":
		return NewAfricaTalking(
			config.AfricaTalkingUsername,
			config.AfricaTalkingAPIKey,
			config.AfricaTalkingSenderID,
			"https://api.africastalking.com/version1/messaging",
		), nil
		
	default:
		return nil, fmt.Errorf("unsupported provider: %s", config.EmailProvider)
	}
}

func (s *Service) WithSMTPProvider(config Config) *Service {
	s.config = config
	sender, err := createSender(config)
	if err != nil {
		log.Fatalf("failed to create SMTP sender: %v", err)
	}
	s.sender = sender
	return s
}

func (s *Service) WithAfricaTalkingProvider(config Config) *Service {
	s.config = config
	sender, err := createSender(config)
	if err != nil {
		log.Fatalf("failed to create Africa's Talking sender: %v", err)
	}
	s.sender = sender
	return s
}

func (s *Service) WithTemplates(templates *Templates) *Service {
	s.tmpls = templates
	return s
}

func (s *Service) WithNormalizer(normalizer *normalizer) *Service {
	s.normalizer = normalizer
	return s
}

func (s *Service) WithLogger(logger Logger) *Service {
	s.log = logger
	return s
}

func (s *Service) MustRegisterTemplate(name, path string) {
	if err := s.RegisterTemplate(name, path); err != nil {
		log.Fatalf("failed to register template %s: %v", name, err)
	}
}
