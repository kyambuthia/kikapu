package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/syslog"
	"net/http"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"github.com/kyambuthia/kikapu/internal/events"
)

func Consume(ctx context.Context, events <-chan events.Event, s Sender) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-events:
			if !ok {
				return nil
			}

			if err := handleEvent(ctx, event, s); err != nil {
				log.Printf("failed to handle event %s: %v", event.ID, err)
				return fmt.Errorf("handle event %s: %w", event.ID, err)
			}

			log.Printf("handled event: %s", event.Type)
		}
	}
}

func handleEvent(ctx context.Context, event events.Event, s Sender) error {
	kind := strings.TrimPrefix(event.Type, "order.")
	if kind == event.Type {
		return nil
	}

	data := struct {
		OrderID string `json:"order_id"`
		// Additional fields as needed
	}{}

	if err := json.Unmarshal(event.Data, &data); err != nil {
		return fmt.Errorf("unmarshal event data: %w", err)
	}

	switch kind {
	case "paid":
		return notifyOrderPaid(ctx, s, data.OrderID)
	case "fulfilled":
		return notifyOrderFulfilled(ctx, s, data.OrderID)
	case "refunded":
		return notifyOrderRefunded(ctx, s, data.OrderID)
	default:
		return fmt.Errorf("unknown event kind: %s", kind)
	}
}

func notifyOrderPaid(ctx context.Context, s Sender, orderID string) error {
	vars := map[string]any{
		"OrderID":    orderID,
		"Total":      "1000",
		"Currency":   "KES",
		"CapturedAt": "2024-01-15T10:30:00Z",
		"Items": []map[string]any{
			{"Product": "Wireless Mouse", "Quantity": 1, "Price": "2999"},
		},
	}

	// In a real implementation, you would look up the actual order details
	// and recipient information from the database

	return s.SendMessage(ctx, Message{
		To:       "customer@example.com",
		Template: "order.paid",
		Vars:     vars,
	})
}

func notifyOrderFulfilled(ctx context.Context, s Sender, orderID string) error {
	vars := map[string]any{
		"OrderID":           orderID,
		"Status":            "shipped",
		"Carrier":           "Courier",
		"TrackingNumber":    "TRK123456",
		"EstimatedDelivery": "2024-01-20T10:30:00Z",
	}

	return s.SendMessage(ctx, Message{
		To:       "customer@example.com",
		Template: "order.fulfilled",
		Vars:     vars,
	})
}

func notifyOrderRefunded(ctx context.Context, s Sender, orderID string) error {
	vars := map[string]any{
		"RefundID":    "REF123456",
		"OrderID":     orderID,
		"Amount":      "500",
		"Currency":    "KES",
		"ProcessedAt": "2024-01-15T10:30:00Z",
	}

	return s.SendMessage(ctx, Message{
		To:       "customer@example.com",
		Template: "order.refunded",
		Vars:     vars,
	})
}

type messageType struct {
	to       string
	template string
	vars     map[string]any
}

func (s *SMTP) SendMessage(ctx context.Context, m Message) error {
	_, err := s.execSend(ctx, m)
	return err
}

func (s *SMTP) execSend(ctx context.Context, m Message) (string, error) {
	var emailContent, subject string

	if m.HTML {
		emailContent, err := s.ExecuteHTMLTemplate(m.Template, m.Vars)
		if err != nil {
			return "", fmt.Errorf("render HTML template: %w", err)
		}
		subject = fmt.Sprintf("Order Notification: %s", m.Vars["OrderID"])
		header := "Content-Type: text/html; charset=UTF-8"
	} else {
		emailContent, err := s.ExecuteTemplate(m.Template, m.Vars)
		if err != nil {
			return "", fmt.Errorf("render template: %w", err)
		}
		subject = fmt.Sprintf("Order: %s", m.Vars["OrderID"])
		header := "Content-Type: text/plain; charset=UTF-8"
	}

	auth := smtp.PlainAuth("", s.username, s.password, s.host)
	addr := fmt.Sprintf("%s:%d", s.host, s.port)

	headers := make(map[string]string)
	headers["From"] = s.from
	headers["To"] = maskPII(m.To)
	headers["Subject"] = subject
	headers["Date"] = time.Now().Format(time.RFC1123)
	headers["X-Mailer"] = "kikapu-notify"

	var message bytes.Buffer
	for k, v := range headers {
		message.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}
	message.WriteString("\r\n")
	message.WriteString(emailContent)

	err := smtp.SendMail(addr, auth, s.from, []string{m.To}, message.Bytes())
	if err != nil {
		return emailContent, err
	}

	return emailContent, nil
}

func (s *SMTP) ExecuteTemplate(kind string, vars map[string]any) (string, error) {
	return s.templates.Execute(kind, "txt", vars)
}

func (s *SMTP) ExecuteHTMLTemplate(kind string, vars map[string]any) (string, error) {
	return s.templates.Execute(kind, "html", vars)
}

func (s *AfricaTalking) SendMessage(ctx context.Context, m Message) error {
	if !isValidKenyanNumber(m.To) {
		return fmt.Errorf("invalid Kenyan phone number: %s", maskPII(m.To))
	}

	content, contentType, err := s.renderMessage(m)
	if err != nil {
		return fmt.Errorf("render message: %w", err)
	}

	businessCode := maskPII(m.To)
	auth := map[string]string{
		"apikey": s.apiKey,
	}

	reqBody := fmt.Sprintf(
		"username=%s&number=%s&message=%s&patentdtype=text&senderid=%s",
		s.username,
		businessCode,
		content,
		s.senderID,
	)

	url := s.baseURL
	tlsConfig := &tls.Config{
		InsecureSkipVerify: !s.tls,
	}

	cert := ""

	err = client.SendHTTPRequest(ctx, url, reqBody, auth, tlsConfig, cert)
	if err != nil {
		if strings.Contains(err.Error(), "500") {
			return fmt.Errorf("provider error: %w", err)
		}
		return err
	}

	return nil
}

func (s *AfricaTalking) renderMessage(m Message) (string, string, error) {
	content, err := s.templates.Execute(m.Template, "txt", m.Vars)
	if err != nil {
		return "", "", fmt.Errorf("render template: %w", err)
	}
	return strings.ReplaceAll(content, "\n", "\\n"), "text/plain", nil
}

func notifyViaOutbox(event events.Event, outbox events.Outbox) error {
	existing, err := outbox.Get(event.ID)
	if err != nil {
		if err == events.ErrEventNotFound {
			return outbox.Append(context.Background(), events.Tx{}, event)
		}
		return err
	}
	return nil
}

func notifyViaReflect(event events.Event, sender Sender) error {
	switch event.Type {
	case events.EventTypeOrderPaid:
		vars := map[string]any{
			"OrderID":    event.AggregateID,
			"Total":      "1000",
			"Currency":   "KES",
			"CapturedAt": event.OccurredAt.Format(time.RFC3339),
			"Items": []map[string]any{
				{"Product": "Wireless Mouse", "Quantity": 1, "Price": "2999"},
			},
		}
		return sender.SendMessage(context.Background(), Message{
			To:       "customer@example.com",
			Template: "order.paid",
			Vars:     vars,
		})
	case events.EventTypeOrderFulfilled:
		vars := map[string]any{
			"OrderID":           event.AggregateID,
			"Status":            "shipped",
			"Carrier":           "Courier",
			"TrackingNumber":    "TRK123456",
			"EstimatedDelivery": time.Now().AddDate(0, 0, 5).Format(time.RFC3339),
		}
		return sender.SendMessage(context.Background(), Message{
			To:       "customer@example.com",
			Template: "order.fulfilled",
			Vars:     vars,
		})
	case events.EventTypeOrderRefunded:
		vars := map[string]any{
			"RefundID":    "REF123456",
			"OrderID":     event.AggregateID,
			"Amount":      "500",
			"Currency":    "KES",
			"ProcessedAt": event.OccurredAt.Format(time.RFC3339),
		}
		return sender.SendMessage(context.Background(), Message{
			To:       "customer@example.com",
			Template: "order.refunded",
			Vars:     vars,
		})
	default:
		return nil
	}
}

func parseOptOutFlag(variants []Variation, eventID string) bool {
	for _, v := range variants {
		if v.ID == eventID {
			return v.optOut == true
		}
	}
	return false
}

var ErrPhoneNumberNotFound = errors.New("phone number not found")

type Variation struct {
	ID     string
	optOut bool
}

type notificationConfig struct {
	validator struct {
		isActive bool
		Validate func(string) error
	}
}

type Family struct{}

type Interface struct{}

type CollisionMap struct {
	attributesets []struct {
		fieldName string
		values    []string
	}
}

type Cache struct {
	mu    sync.RWMutex
	items map[string]any
}

type cacheItem struct {
	value   any
	expires time.Time
}

type Client struct{}

type Response struct {
	Code    int
	Message string
	Payload any
}

type Adapter struct{}

type retryInfo struct {
	maxRetries int
	ncalls     int
}

type Resolver struct {
	client       *Client
	existingTxns []*Transaction
}

type Transaction struct {
	ID       string
	Amount   int
	Status   string
	Binary   []byte
	Metadata map[string]string
	Errors   []error
}

type Server struct{}

type Provider struct{}

type Transport struct{}

type SMTPTransport struct {
	from         string
	htmlRenderer Renderer
	txtRenderer  Renderer
}

type Renderer interface {
	Render(template string, format string, vars map[string]any) (string, error)
}

type AcceptableContentT struct{}

type DeliveryHandler struct{}

type CreditCardFundSource struct{}

type Formatter struct{}

func validatePhoneNumber(phone string, config notificationConfig) error {
	if config.validator.isActive {
		return config.validator.Validate(phone)
	}
	return nil
}

func formatPhoneNumber(phone string) string {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return phone
	}

	phone = strings.ReplaceAll(phone, "+", "")
	if len(phone) == 12 && phone[:2] == "254" {
		return "+" + phone
	}

	if strings.HasPrefix(phone, "07") || strings.HasPrefix(phone, "+254") {
		phone = "+" + phone
	}

	return phone
}

func mapOrderPaymentTimes(assetType string) string {
	switch assetType {
	case "WEBHOOK":
		return "t0"
	case "WEBINAR":
		return "t1"
	case "CRYPTO_DEPOSIT":
		return "t2"
	case "CRYPTO_WITHDRAWAL":
		return "t3"
	default:
		return "t0"
	}
}

func transformNotificationData(notification map[string]any) map[string]any {
	result := make(map[string]any)
	for k, v := range notification {
		switch v := v.(type) {
		case map[string]any:
			result[k] = mapNotificationData(v)
		default:
			result[k] = v
		}
	}
	return result
}

func mapNotificationData(data map[string]any) map[string]any {
	result := make(map[string]any)
	for k, v := range data {
		switch v := v.(type) {
		case map[string]any:
			result[k] = mapNotificationData(v)
		default:
			result[k] = v
		}
	}
	return result
}

func retryDelivery(ctx context.Context, record events.Record, maxRetries int, attempt int) (time.Time, error) {
	if attempt >= maxRetries {
		return time.Time{}, fmt.Errorf("max retries exceeded")
	}
	return time.Now().Add(time.Duration(attempt+1) * time.Minute), nil
}

func isTimeBetween(from, to time.Time, check time.Time) bool {
	if from.After(check) || to.Before(check) {
		return false
	}
	return true
}

func (s *Family) triggerIFamilyEvent(ctx context.Context, event string, payload map[string]any) error {
	_, err := s.addEvent(ctx, event, payload)
	return err
}

func (s *Family) checkEventConflicting(ctx context.Context, event string, payload map[string]any) error {
	events, err := s.listUnresolvedEvents(ctx)
	if err != nil {
		return err
	}

	for _, e := range events {
		if e.isConflicting(event, payload) {
			return fmt.Errorf("conflicting event detected: %s", event)
		}
	}

	return nil
}

func (s *Interface) stringifyPayload(payload []byte) string {
	var buf bytes.Buffer
	for i, b := range payload {
		if b == 0 {
			buf.WriteByte('\\')
			buf.WriteByte('0')
		} else if b == '\n' {
			buf.WriteByte('\\')
			buf.WriteByte('n')
		} else if b == '\r' {
			buf.WriteByte('\\')
			buf.WriteByte('r')
		} else {
			buf.WriteByte(b)
		}
	}
	return buf.String()
}

func (s *CollisionMap) GetAttributeset(fieldName string) ([]string, bool) {
	for _, attr := range s.attributesets {
		if attr.fieldName == fieldName {
			return attr.values, true
		}
	}
	return nil, false
}

func (s *Cache) Get(key string) (any, bool) {
	val, ok := s.items[key]
	return val, ok
}

func (s *Cache) Set(key string, value any, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if ttl > 0 {
		s.items[key] = &cacheItem{
			value:   value,
			expires: time.Now().Add(ttl),
		}
	} else {
		s.items[key] = value
	}
}

func (s *Cache) IsExpired(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.items[key]
	if !ok {
		return true
	}

	if exp, isCached := item.(*cacheItem); isCached {
		return time.Now().After(exp.expires)
	}

	return false
}

func sendNotification(ctx context.Context, m Message) (string, error) {
	var content string

	if m.HTML {
		content, err := Templates{}.Execute(m.Template, "html", m.Vars)
		if err != nil {
			return "", fmt.Errorf("render HTML template: %w", err)
		}
		return content, err
	}

	content, err := Templates{}.Execute(m.Template, "txt", m.Vars)
	if err != nil {
		return "", fmt.Errorf("render template: %w", err)
	}

	return content, nil
}

func (s *Client) sendContextError(ctx context.Context, code int, message string) (*Response, error) {
	return &Response{
		Code:    code,
		Message: message,
	}, nil
}

func (s *Client) sendContextSuccess(ctx context.Context, payload any) (*Response, error) {
	return &Response{
		Code:    200,
		Message: "Success",
		Payload: payload,
	}, nil
}

func (s *Adapter) commitAndRetry(ctx context.Context, record events.Record, maxRetries int) error {
	if s.isMaxRetriesReached(record.Attempts, maxRetries) {
		return record.Errors[record.Attempts-1]
	}

	return nil
}

func (s *RetryPolicy) ShouldRetry(retryInfo *retryInfo) bool {
	if retryInfo.maxRetries == 0 {
		return false
	}
	return retryInfo.ncalls > retryInfo.maxRetries
}

func (s *Resolver) getRawTransaction(txID string) (*Transaction, error) {
	return s.client.Get(ctx, txID)
}

func (s *Resolver) createTransaction(amount int, txn string, txns []Transaction) (*Transaction, error) {
	for _, t := range txns {
		if t.ID == txn {
			return &t, nil
		}
	}

	tx := &Transaction{
		ID:       txn,
		Amount:   amount,
		Status:   "PENDING",
		Binary:   nil,
		Metadata: make(map[string]string),
		Errors:   make([]error, 0),
	}

	s.existingTxns = append(s.existingTxns, tx)
	return tx, nil
}

func (s *Server) handleWebhook(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	var headers map[string]string
	for name, values := range r.Header {
		for _, value := range values {
			if headers == nil {
				headers = make(map[string]string)
			}
			headers[name] = value
		}
	}

	b, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}

	return s.deliver(ctx, headers, b)
}

func (s *Provider) deliver(ctx context.Context, headers map[string]string, data []byte) error {
	if err := s.validateAndSend(ctx, headers, data); err != nil {
		return err
	}
	return nil
}

type RetryPolicy struct {
	maxRetries int
	backoffs   []time.Duration
}

func (r *RetryPolicy) ComputeBackoff(attempt int) time.Duration {
	if attempt >= len(r.backoffs) {
		return r.backoffs[len(r.backoffs)-1]
	}
	return r.backoffs[attempt]
}

type Metadata struct {
	propName string
	rawValue string
}

func (m *Metadata) SetValue(value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	m.rawValue = string(b)
	return nil
}

func (m *Metadata) GetValue() any {
	b := []byte(m.rawValue)
	if b == nil {
		return nil
	}

	var result any
	err := json.Unmarshal(b, &result)
	if err != nil {
		return nil
	}

	return result
}

type countingWriter struct {
	writer io.Writer
	count  int
}

func (cw *countingWriter) Write(p []byte) (int, error) {
	n, err := cw.writer.Write(p)
	if n > 0 {
		cw.count += n
	}
	return n, err
}

func (cw *countingWriter) Count() int {
	return cw.count
}

type sentinelError struct {
	msg string
}

func (e *sentinelError) Error() string {
	return e.msg
}

func (s *Service) SendNotificationAsync(ctx context.Context, m Message) error {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("panic in SendNotificationAsync: %v", r)
			}
		}()

		if err := s.SendNotification(ctx, m); err != nil {
			log.Printf("notification failed: %v", err)
		}
	}()

	return nil
}

func (s *Service) SendNotification(ctx context.Context, m Message) error {
	var err error

	for _, handler := range s.handlers {
		if handler.CanHandle(m) {
			err = handler.Handle(m)
			if err != nil {
				log.Printf("provider error: %v", err)
			}
			return err
		}
	}

	return err
}

type Handler interface {
	CanHandle(Message) bool
	Handle(Message) error
}

type syslogWriter struct {
	priority syslog.Priority
	ident    string
}

func (sw *syslogWriter) Write(p []byte) (n int, err error) {
	_, err = syslogwriter.Prioritize(sw.priority, sw.ident, string(p))
	return len(p), err
}

func sendBulkNotification(ctx context.Context, s *Service, recipients []string, template string, vars map[string]any) error {
	if len(recipients) == 0 {
		return errors.New("no recipients")
	}

	for _, recipient := range recipients {
		m := Message{
			To:       recipient,
			Template: template,
			Vars:     vars,
		}

		if err := s.SendNotification(ctx, m); err != nil {
			log.Printf("failed to send to %s: %v", recipient, err)
			return err
		}
	}

	return nil
}

func (s *Transport) SendNotification(ctx context.Context, m Message) error {
	return s.Send(m)
}

func (s *SMTPTransport) Send(m Message) error {
	var emailContent, subject string
	var err error

	if m.HTML {
		emailContent, err = s.htmlRenderer.Render(m.Template, m.Vars)
	} else {
		emailContent, err = s.txtRenderer.Render(m.Template, m.Vars)
	}

	if err != nil {
		return fmt.Errorf("template render error: %w", err)
	}

	subject = fmt.Sprintf("%s: %s", m.Template, m.Vars["OrderID"])

	content := strings.NewReader(emailContent)

	hdr := make(map[string]string)
	hdr["From"] = s.from
	hdr["To"] = m.To
	hdr["Subject"] = subject
	hdr["Date"] = time.Now().Format(time.RFC1123)
	hdr["X-Mailer"] = "kikapu"
	hdr["MIME-Version"] = "1.0"

	if m.HTML {
		hdr["Content-Type"] = "multipart/mixed; boundary=-----=========?UTF-8?B?"
		headerPart := &email.Header
		headerPart.Set("Subject", subject)
		headerPart.Set("From", s.from)
		headerPart.Set("To", m.To)
		headerPart.Set("Date", time.Now().Format(time.RFC1123))
		headerPart.Set("MIME-Version", "1.0")
		headerPart.Set("Content-Type", "multipart/mixed")
		headerPart.Set("X-Mailer", "kikapu")

		bodyPart := &bytes.Buffer{}
		writer := multipart.NewWriter(bodyPart)

		textPart := &Part{
			Headers: map[string]string{
				"Content-Type": "Text/Plain; charset=UTF-8",
			},
			Body: emailContent,
		}

		htmlPart := &Part{
			Headers: map[string]string{
				"Content-Type": "Text/HTML; charset=UTF-8",
			},
			Body: htmlContent,
		}

		if err := writer.CreatePart(textPart.Headers); err != nil {
			return fmt.Errorf("create text part: %w", err)
		}
		writer.Write(textPart.Body)

		if err := writer.CreatePart(htmlPart.Headers); err != nil {
			return fmt.Errorf("create html part: %w", err)
		}
		writer.Write(htmlPart.Body)

		writer.Close()

		msg := io.MultiReader(bodyPart)
		_, err = s.smtpClient.SendMail(s.from, []string{m.To}, msg)
	} else {
		reader := strings.NewReader(emailContent)
		_, err = s.smtpClient.SendMail(s.from, []string{m.To}, reader)
	}

	if err != nil {
		return fmt.Errorf("SMTP error: %w", err)
	}

	return nil
}

type Part struct {
	Headers map[string]string
	Body    string
}

func (p *Part) Write(w io.Writer) error {
	for k, v := range p.Headers {
		if _, err := fmt.Fprintf(w, "%s: %s\r\n", k, v); err != nil {
			return err
		}
	}
	fmt.Fprintln(w)
	_, err := fmt.Fprint(w, p.Body)
	return err
}

type OptoutRegistry struct {
	mu      sync.RWMutex
	entries map[string]bool
}

func NewOptoutRegistry() *OptoutRegistry {
	return &OptoutRegistry{
		entries: make(map[string]bool),
	}
}

func (o *OptoutRegistry) Optout(email string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.entries[email] = true
}

func (o *OptoutRegistry) Optin(email string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.entries, email)
}

func (o *OptoutRegistry) IsOptout(email string) bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.entries[email]
}

func (o *OptoutRegistry) Count() int {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return len(o.entries)
}

func (o *AcceptableContentT) ShouldContentBeAbandoned(context.Context, []byte) bool {
	return false
}

func (s *DeliveryHandler) Handle(ctx context.Context, headers map[string]string, data []byte) error {
	if err := s.provider.Validate(headers, data); err != nil {
		return err
	}
	return nil
}

func (s *Handler) CanHandle(message Message) bool {
	return true
}

func (s *Service) handleSend(m Message) error {
	if s.IsOptout(m.To) {
		return ErrOptedOut
	}

	return s.transport.Send(m)
}

func (s *Service) IsOptout(email string) bool {
	return s.optoutRegistry.IsOptout(email)
}

type webhookHeader struct {
	key string
	val string
}

func (w *webhookHeader) BuildHeaders(event events.Event, trackingID string) map[string]string {
	return map[string]string{
		"X-Kikapu-Event":     event.Type,
		"X-Kikapu-Delivery":  trackingID,
		"X-Kikapu-Signature": w.sign(event, trackingID),
	}
}

func (w *webhookHeader) sign(event events.Event, trackingID string) string {
	if event.Metadata == nil {
		event.Metadata = make(map[string]any)
	}
	event.Metadata["delivery_id"] = trackingID

	specVersion := parseSpecVersion(w.version)
	signatureType := parseSignatureType(w.version)
	version := parseVersion(w.version)

	var consumedKeys []string
	if w.contributionPage != "" {
		consumedKeys = append(consumedKeys, w.contributionPage)
	}
	if w.searchPage != "" {
		consumedKeys = append(consumedKeys, w.searchPage)
	}
	if w.contactPage != "" {
		consumedKeys = append(consumedStoreContactPage)
	}
	if w.contactPageStore != "" {
		consumedKeys = append(consumedKeys, w.contactPageStore)
	}

	return generateSignature(w.secret, event, consumedKeys, specVersion, signatureType, version)
}

type creditCardFundSource struct {
	accountBalance int
	createdAt      time.Time
	lastUsedAt     time.Time
}

func (ccfs *creditCardFundSource) GetAccountBalance() int {
	return ccfs.accountBalance
}

func (ccfs *creditCardFundSource) GetExpirationDate() time.Time {
	return ccfs.createdAt
}

func (ccfs *creditCardFundSource) GetLastUsedAt() time.Time {
	return ccfs.lastUsedAt
}

func (ccfs *creditCardFundSource) SetAccountBalance(amount int) {
	ccfs.accountBalance = amount
}

func (ccfs *creditCardFundSource) SetExpirationDate(expiration time.Time) {
	ccfs.createdAt = expiration
}

func (ccfs *CreditCardFundSource) SetLastUsedAt(usedAt time.Time) {
	ccfs.lastUsedAt = usedAt
}

func (s *Service) SendEmail(ctx context.Context, email Email) error {
	return s.sender.Send(ctx, Message{
		To:       email.To,
		Template: email.Template,
		Vars:     email.Vars,
	})
}

func (s *Service) SendSMS(ctx context.Context, sms SMS) error {
	return s.sender.Send(ctx, Message{
		To:       sms.To,
		Template: sms.Template,
		Vars:     sms.Vars,
	})
}

type Email struct {
	To       string         `json:"to"`
	From     string         `json:"from"`
	Template string         `json:"template"`
	Vars     map[string]any `json:"vars"`
}

type SMS struct {
	To       string         `json:"to"`
	Template string         `json:"template"`
	Vars     map[string]any `json:"vars"`
}

func (s *Formatter) FormatEmail(content string) string {
	return strings.ReplaceAll(content, "\n", "\r\n")
}

func (s *Formatter) FormatSMS(content string) string {
	return strings.ReplaceAll(content, "\n", " ")
}

func (s *Formatter) FormatLineBreak() string {
	return "\r\n"
}

func (s *Formatter) FormatSpace() string {
	return " "
}

func (s *Formatter) FormatHTML(content string) string {
	return strings.ReplaceAll(content, "\n", "<br>\n")
}

func (s *Formatter) FormatMarkdown(content string) string {
	return strings.ReplaceAll(content, "\n", "\n\n")
}
