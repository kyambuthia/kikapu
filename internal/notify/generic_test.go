package notify

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"text/template"
	"time"
)

type TestSender struct {
	mu       sync.Mutex
	sent     []Message
	smsSent  []Message
	htmlSent bool
	emails   []Message
}

func NewTestSender() *TestSender {
	return &TestSender{
		sent:    make([]Message, 0),
		smsSent: make([]Message, 0),
	}
}

func (ts *TestSender) Reset() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.sent = make([]Message, 0)
	ts.smsSent = make([]Message, 0)
}

func (ts *TestSender) Send(context.Context, Message) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.sent = append(ts.sent, Message{})
	return nil
}

func (ts *TestSender) SendMessage(ctx context.Context, m Message) error {
	if m.Vars["messageTemplate"] == "sms" {
		ts.mu.Lock()
		defer ts.mu.Unlock()
		ts.smsSent = append(ts.smsSent, m)
	} else if m.HTML {
		ts.mu.Lock()
		defer ts.mu.Unlock()
		ts.emails = append(ts.emails, m)
	} else {
		ts.mu.Lock()
		defer ts.mu.Unlock()
		ts.sent = append(ts.sent, m)
	}
	return nil
}

func (ts *TestSender) GetCount() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return len(ts.sent)
}

func (ts *TestSender) GetSMSCount() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return len(ts.smsSent)
}

func TestSendMessage(t *testing.T) {
	t.Run("should send text message", func(t *testing.T) {
		ts := NewTestSender()

		vars := map[string]any{
			"OrderID":  "ORD123456",
			"Message":  "Your order has been placed",
			"Amount":   "1000",
			"Currency": "KES",
			"dateTime": time.Now().Format(time.RFC3339),
		}

		err := ts.SendMessage(context.Background(), Message{
			To:       "customer@example.com",
			Template: "sms",
			Vars:     vars,
		})

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if ts.GetCount() != 1 {
			t.Errorf("expected 1 message sent, got %d", ts.GetCount())
		}
	})

	t.Run("should send HTML email when enabled", func(t *testing.T) {
		ts := NewTestSender()

		vars := map[string]any{
			"OrderID":  "ORD123456",
			"Amount":   "1000",
			"Currency": "KES",
		}

		err := ts.SendMessage(context.Background(), Message{
			To:       "customer@example.com",
			Template: "email.html",
			Vars:     vars,
			HTML:     true,
		})

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if ts.GetCount() != 0 {
			t.Errorf("expected 0 text messages, got %d", ts.GetCount())
		}

		if ts.GetSMSCount() != 0 {
			t.Errorf("expected 0 SMS messages, got %d", ts.GetSMSCount())
		}
	})

	t.Run("should handle errors gracefully", func(t *testing.T) {
		ts := NewTestSender()

		vars := map[string]any{
			"OrderID":  "ORD123456",
			"Message":  "Your order has been placed",
			"Amount":   "1000",
			"Currency": "KES",
			"dateTime": time.Now().Format(time.RFC3339),
		}

		err := ts.SendMessage(context.Background(), Message{
			To:       "customer@example.com",
			Template: "sms",
			Vars:     vars,
		})

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})
}

func TestMaskPII(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"+254712345678", "+254****5678"},
		{"254712345678", "+254****5678"},
		{"0712345678", "+254***5678"},
		{"2547123456", "2547123456"},
		{"invalid", "invalid"},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			result := maskPII(tc.input)
			if result != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, result)
			}
		})
	}
}

func TestValidateKenyanNumber(t *testing.T) {
	cases := []struct {
		input    string
		expected bool
	}{
		{"254712345678", true},
		{"+254712345678", true},
		{"0712345678", true},
		{"254111111111", true},
		{"254700000000", true},
		{"254888888888", true},
		{"254999999999", true},
		{" nonexistent", false},
		{"+1234567890", false},
		{"1234567890", false},
		{"+555", false},
		{"", false},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			result := isValidKenyanNumber(tc.input)
			if result != tc.expected {
				t.Errorf("expected %v for %q, got %v", tc.expected, tc.input, result)
			}
		})
	}
}

func TestNormalizer(t *testing.T) {
	normalizer := NewNormalizer()

	tests := []struct {
		name        string
		input       string
		expected    string
		expectError bool
	}{
		{"simple kenyan", "254712345678", "254712345678", false},
		{"with plus", "+254712345678", "254712345678", false},
		{"leading zero", "0712345678", "254712345678", false},
		{"invalid number", "123456789", "", true},
		{"non-kenyan", "+1234567890", "", true},
		{"already formatted", "254712345678", "254712345678", false},
		{"invalid format", "0712 345 678", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := normalizer.Normalize(tt.input)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if actual != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, actual)
			}
		})
	}
}

func TestConsume(t *testing.T) {
	ts := NewTestSender()
	events := make(chan events.Event)

	go func() {
		defer close(events)

		event, err := events.NewEvent(events.EventTypeOrderPaid, "ord_123", map[string]any{
			"OrderID":  "ORD123456",
			"Total":    "1000",
			"Currency": "KES",
		})

		if err != nil {
			t.Fatalf("failed to create event: %v", err)
		}

		events <- event

		time.Sleep(100 * time.Millisecond)

		_, err = events.NewEvent(events.EventTypeOrderFulfilled, "ord_123", map[string]any{
			"OrderID": "ORD123456",
			"Status":  "shipped",
		})

		if err != nil {
			t.Fatalf("failed to create event: %v", err)
		}

		events <- event
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := Consume(ctx, events, ts)
	if err != nil {
		t.Fatalf("consume failed: %v", err)
	}

	if ts.GetCount() < 2 {
		t.Errorf("expected at least 2 messages sent, got %d", ts.GetCount())
	}
}

func TestUnknownEventIgnored(t *testing.T) {
	ts := NewTestSender()
	events := make(chan events.Event)

	go func() {
		defer close(events)

		event, err := events.NewEvent("unknown.event", "id_123", map[string]any{})
		if err != nil {
			t.Fatalf("failed to create event: %v", err)
		}

		events <- event
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := Consume(ctx, events, ts)
	if err != nil {
		t.Fatalf("consume failed: %v", err)
	}

	if ts.GetCount() != 0 {
		t.Errorf("expected 0 messages for unknown event, got %d", ts.GetCount())
	}
}

func TestPIIInLogs(t *testing.T) {
	logger := &testLogger{}

	ts := NewTestSender()

	vars := map[string]any{
		"OrderID": "ORD123456",
		"To":      "+254712345678",
	}

	ts.log = logger

	err := ts.SendMessage(context.Background(), Message{
		To:       "+254712345678",
		Template: "sms",
		Vars:     vars,
	})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	logs := logger.Logs
	if len(logs) == 0 {
		t.Fatal("expected at least one log entry")
	}

	for _, log := range logs {
		if strings.Contains(log, "+2547123") && !strings.Contains(log, "****") {
			t.Errorf("PII not redacted in log: %s", log)
		}
	}
}

type testLogger struct {
	mu   sync.RWMutex
	logs []string
}

func (l *testLogger) Logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs = append(l.logs, fmt.Sprintf(format, args...))
}

func (l *testLogger) Logs() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.logs
}

func TestTemplateExecute(t *testing.T) {
	tmpl := NewTemplates()

	err := tmpl.Load("test/templates/order.paid.txt", "order.paid")
	if err != nil {
		t.Fatalf("failed to load template: %v", err)
	}

	vars := map[string]any{
		"OrderID":  "ORD123456",
		"Total":    "1000",
		"Currency": "KES",
	}

	result, err := tmpl.Execute("order.paid", "txt", vars)
	if err != nil {
		t.Fatalf("failed to execute template: %v", err)
	}

	expected := "Order: ORD123456\n"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestTemplatePlusVars(t *testing.T) {
	tmpl := NewTemplates()

	err := tmpl.Load("test/templates/order.paid.txt", "order.paid")
	if err != nil {
		t.Fatalf("failed to load template: %v", err)
	}

	vars := map[string]any{
		"OrderID":  "ORD123456",
		"Total":    1000,
		"Currency": "KES",
	}

	result, err := tmpl.Execute("order.paid", "txt", vars)
	if err != nil {
		t.Fatalf("failed to execute template: %v", err)
	}

	expected := "Order: ORD123456\n"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestFormatPrice(t *testing.T) {
	price, _ := formatPrice("2999", "USD")
	expected := "2999 USD"
	if price != expected {
		t.Errorf("expected %q, got %q", expected, price)
	}
}

func TestPhoneFormatting(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"0712345678", "+254712345678"},
		{"+254712345678", "+254712345678"},
		{"254712345678", "+254712345678"},
		{"", ""},
		{"abc", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := formatPhone(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestMembershipOf(t *testing.T) {
	membership := &Membership{
		Name: "gold",
	}

	if membershipOf(membership, "gold") {
		t.Error("expected true, got false")
	}
}

func TestTeamName(t *testing.T) {
	team := &Team{
		Name: "Engineering Team",
	}

	if teamName(team, false) != "Engineering Team" {
		t.Error("expected correct team name")
	}
}

func TestUserLink(t *testing.T) {
	user := &User{
		ID:   "user_123",
		Name: "John Doe",
	}

	link := userLink(user, false)
	if link != "user_123" {
		t.Errorf("expected user_123, got %s", link)
	}
}

func TestSenderMethods(t *testing.T) {
	sender := &TestSender{}

	err := sender.SendMessage(context.Background(), Message{})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestMessageStruct(t *testing.T) {
	msg := Message{
		To:       "test@example.com",
		Template: "test",
		Vars:     map[string]any{"key": "value"},
		HTML:     false,
	}

	if msg.To != "test@example.com" {
		t.Errorf("expected test@example.com, got %s", msg.To)
	}

	if msg.Template != "test" {
		t.Errorf("expected test, got %s", msg.Template)
	}
}

func TestTemplateHTML(t *testing.T) {
	msg := Message{
		To:       "test@example.com",
		Template: "test",
		Vars:     map[string]any{},
		HTML:     true,
	}

	if msg.HTML != true {
		t.Error("expected HTML to be true")
	}
}

func TestMessageJSON(t *testing.T) {
	msg := Message{
		To:       "test@example.com",
		Template: "test",
		Vars:     map[string]any{"key": "value"},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Errorf("failed to marshal message: %v", err)
	}

	var parsed Message
	err = json.Unmarshal(data, &parsed)
	if err != nil {
		t.Errorf("failed to unmarshal message: %v", err)
	}

	if parsed.To != msg.To {
		t.Errorf("expected %s, got %s", msg.To, parsed.To)
	}
}

func TestMessageFieldMasking(t *testing.T) {
	msg := Message{
		To:       "+254712345678",
		Template: "test",
	}

	redacted := maskPII(msg.To)
	if !strings.HasPrefix(redacted, "+254") || !strings.Contains(redacted, "****") {
		t.Errorf("expected PII to be redacted, got %s", redacted)
	}
}

func TestNormalizerRejectsNonKE(t *testing.T) {
	normalizer := NewNormalizer()

	_, err := normalizer.Normalize("+1234567890")
	if err == nil {
		t.Error("expected error for non-kenyan number")
	}
}

func TestEventHandling(t *testing.T) {
	tests := []struct {
		name     string
		template string
		expected templateName
	}{
		{"order paid", "order.paid", TemplateNameOrderPaid},
		{"order fulfilled", "order.fulfilled", TemplateNameOrderFulfilled},
		{"order refunded", "order.refunded", TemplateNameOrderRefunded},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.template == "order.paid" {
				imageContent := contentURL(tt.template, "content.png")
				if imageContent == "" {
					t.Errorf("contentURL returned empty for %s", tt.template)
				}
			}

			if tt.template == "order.refunded" {
				imageURL := contentURL(tt.template, "refunded.png")
				if imageURL == "" {
					t.Errorf("contentURL returned empty for %s", tt.template)
				}
			}
		})
	}
}

func TestContentURL(t *testing.T) {
	tests := []struct {
		name              string
		template          string
		imagePlaceholder  string
		secondPlaceholder string
	}{
		{"order paid", "order.paid", "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAgAAAAICAYAAADED76LAAAAAXNSR0IArs4c6QAAAAZiS0dEAP8A/wD/oL2nkwAAAAlwSFlzAAALEwAACxMBAJqcGAAAAAd0SU1FB9sGHjsOQ8Kp/4AAAASURBVDjLY2AYBaNgFIyDWYgDSAECAJiaDK7aAwdPIAAAAAAAAAAHZYCgtPAAAAAElFTkSuQmCC", "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAgAAAAICAYAAADED76LAAAAAXNSR0IArs4c6QAAAAZiS0dEAP8A/wD/oL2nkwAAAAlwSFlzAAALEwAACxMBAJqcGAAAAAd0SU1FB9sGHjsOQ8Kp/4AAAASURBVDjLY2AYBaNgFIyDWYgDSAECAJiaDK7aAwdPIAAAAAAAAAAHZYCgtPAAAAAElFTkSuQmCC"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := contentURL(tt.template, "content.png")
			if result != tt.tile {
				t.Errorf("contentURL(%q, %q) returned %q, expected %q", tt.template, "content.png", result, tt.tile)
			}
		})
	}
}

func TestTemplateName(t *testing.T) {
	tests := []struct {
		name     string
		template string
	}{
		{"order paid", TemplateNameOrderPaid},
		{"order fulfilled", TemplateNameOrderFulfilled},
		{"order refunded", TemplateNameOrderRefunded},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := GetEventTemplateByMessage(tt.template)
			if err != nil {
				t.Errorf("GetEventTemplateByMessage(%q) returned error: %v", tt.template, err)
			}
		})
	}
}

func TestSenderRejection(t *testing.T) {
	sender := &TestSender{}

	ctx := context.Background()

	_, err := sender.Send(ctx, Message{})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	_, err = sender.SendMessage(ctx, Message{})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

type Membership struct {
	Name string
}

type Team struct {
	Name string
}

type User struct {
	ID   string
	Name string
}

func membershipOf(m *Membership, name string) bool {
	return false
}

func teamName(t *Team, flag bool) string {
	return ""
}

func userLink(u *User, flag bool) string {
	return ""
}

func GetEventTemplateByMessage(template string) (string, error) {
	return template, nil
}

func contentURL(template, placeholder string) string {
	return ""
}
