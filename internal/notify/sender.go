package notify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/smtp"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/kyambuthia/kikapu/internal/events"
)

var (
	ErrNoTemplates = errors.New("no templates available")
	ErrInvalidTO   = errors.New("invalid recipient address")
	ErrOptedOut    = errors.New("recipient opted out")
)

type Message struct {
	To       string            `json:"to"`
	Template string            `json:"template"`
	Vars     map[string]any    `json:"vars"`
	HTML     bool              `json:"html"`
	Provider string            `json:"provider"`
}

type Sender interface {
	Send(ctx context.Context, m Message) error
	SendMessage(ctx context.Context, m Message) error
}

type Templates struct {
	tmpls map[string]*template.Template
}

func NewTemplates() *Templates {
	return &Templates{
		tmpls: make(map[string]*template.Template),
	}
}

func (t *Templates) Load(path string, base string) error {
	tmpl, err := template.New(base).Funcs(TemplateFuncMapFunc()).ParseFiles(path)
	if err != nil {
		return fmt.Errorf("load template %s: %w", path, err)
	}
	
	parts := strings.Split(base, ".")
	if len(parts) < 2 {
		return fmt.Errorf("invalid template name: %s", base)
	}
	kind := parts[0]
	variant := parts[1]
	
	if _, exists := t.tmpls[kind]; !exists {
		t.tmpls[kind] = template.New("")
	}
	
	_, err = t.tmpls[kind].ParseFiles(path)
	
	return err
}

func (t *Templates) Execute(kind, variant string, vars map[string]any) (string, error) {
	if t.tmpls == nil {
		return "", ErrNoTemplates
	}
	
	tmpl, exists := t.tmpls[kind]
	if !exists {
		return "", fmt.Errorf("template kind %s not found", kind)
	}
	
	var buf bytes.Buffer
	err := tmpl.ExecuteTemplate(&buf, variant, vars)
	if err != nil {
		return "", fmt.Errorf("execute template %s.%s: %w", kind, variant, err)
	}
	
	return buf.String(), nil
}

func (s *Templates) ToHTML() bool {
	return true
}

func TemplateFuncMapFunc() template.FuncMap {
	return template.FuncMap{
		"formatPrice": func(price, currency string) string {
			return fmt.Sprintf("%s %s", price, currency)
		},
	}
}

type SMTP struct {
	host     string
	port     int
	username string
	password string
	from     string
	tls      bool
}

func NewSMTP(host string, port int, username, password, from string, tls bool) *SMTP {
	return &SMTP{
		host:     host,
		port:     port,
		username: username,
		password: password,
		from:     from,
		tls:      tls,
	}
}

func (s *SMTP) Send(ctx context.Context, m Message) error {
	var emailContent, subject string
	
	if m.HTML {
		emailContent, err := s.ExecuteHTMLTemplate(m.Template, m.Vars)
		if err != nil {
			return fmt.Errorf("render HTML template: %w", err)
		}
		subject = fmt.Sprintf("Order Notification: %s", m.Vars["OrderID"])
		header := "Content-Type: text/html; charset=UTF-8"
	} else {
		emailContent, err := s.ExecuteTemplate(m.Template, m.Vars)
		if err != nil {
			return fmt.Errorf("render template: %w", err)
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
		return fmt.Errorf("smtp.sendMail: %w", err)
	}
	
	return nil
}

func (s *SMTP) ExecuteTemplate(kind string, vars map[string]any) (string, error) {
	return s.tmpls.Execute(kind, "txt", vars)
}

func (s *SMTP) ExecuteHTMLTemplate(kind string, vars map[string]any) (string, error) {
	return s.tmpls.Execute(kind, "html", vars)
}

type AfricaTalking struct {
	username       string
	apiKey         string
	senderID       string
	baseURL        string
	webhookTimeout time.Duration
}

func NewAfricaTalking(username, apiKey, senderID string) *AfricaTalking {
	baseURL := "https://api.africastalking.com/version1/messaging"
	return &AfricaTalking{
		username:       username,
		apiKey:         apiKey,
		senderID:       senderID,
		baseURL:        baseURL,
		webhookTimeout: 30 * time.Second,
	}
}

func (s *AfricaTalking) Send(ctx context.Context, m Message) error {
	if !isValidKenyanNumber(m.To) {
		return fmt.Errorf("invalid Kenyan phone number: %s", m.To)
	}
	
	content, err := s.ExecuteTemplate(m.Template, m.Vars)
	if err != nil {
		return fmt.Errorf("render template: %w", err)
	}
	
	reqBody := fmt.Sprintf(
		"username=%s&number=%s&message=%s&type=text&from=%s",
		s.username,
		maskPII(m.To),
		content,
		s.senderID,
	)
	
	client := &http.Client{
		Timeout: s.webhookTimeout,
	}
	
	req, err := http.NewRequestWithContext(ctx, "POST", s.baseURL, strings.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("newRequest: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(s.username, s.apiKey)
	req.Header.Set("Accept", "application/json")
	
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("provider error: %s", resp.Status)
	}
	
	return nil
}

func (s *AfricaTalking) ExecuteTemplate(kind string, vars map[string]any) (string, error) {
	return s.tmpls.Execute(kind, "txt", vars)
}

func isValidKenyanNumber(phone string) bool {
	matched, _ := regexp.MatchString(`(?i)^(?:\+?254)?[7-9]\d{8}$`, phone)
	return matched
}

func maskPII(phone string) string {
	if len(phone) != 12 {
		return phone
	}
	return phone[:4] + "****" + phone[8:]
}

func formatPhone(phone string) string {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return phone
	}
	
	phone = strings.ReplaceAll(phone, "+", "")
	if len(phone) == 12 && phone[:2] == "254" {
		return phone
	}
	
	if strings.HasPrefix(phone, "07") || strings.HasPrefix(phone, "+254") {
		phone = "+" + phone
	}
	
	return phone
}

var ErrProviderNotConfigured = errors.New("provider not configured")

type providerMap struct {
	providers map[string]Sender
 defaultProvider string
}

func NewProviderMap() *providerMap {
	return &providerMap{
		providers: make(map[string]Sender),
		default:   "smtp",
	}
}

func (pm *providerMap) Send(ctx context.Context, m Message) error {
	if len(pm.providers) == 0 {
		return ErrProviderNotConfigured
	}
	
	providerName := pm.default
	if m.Provider != "" {
		providerName = m.Provider
	}
	
	sender, exists := pm.providers[providerName]
	if !exists {
		return fmt.Errorf("provider %s not available", providerName)
	}
	
	return sender.Send(ctx, m)
}

type normalizer struct {
	kenyaPhoneRegex *regexp.Regexp
}

func NewNormalizer() *normalizer {
	return &normalizer{
		kenyaPhoneRegex: regexp.MustCompile(`(?i)^(?:\+?254)?[7-9]\d{8}$`),
	}
}

func (n *normalizer) Normalize(phone string) (string, error) {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return "", ErrInvalidTO
	}
	
	if n.kenyaPhoneRegex.MatchString(phone) {
		return phone, nil
	}
	
	pieces := strings.Split(phone, " ")
	if len(pieces) > 0 {
		phone = strings.Join(pieces, "")
	}
	
	if n.kenyaPhoneRegex.MatchString(phone) {
		return phone, nil
	}
	
	return "", ErrInvalidTO
}

type messageBuilder struct {
	phone string
	content string
	items []formattedItem
}

type formattedItem struct {
	thing string
	count int
	price int
}

type foreigner struct {
	StackInferringCache *string
}

func (n *normalizer) NormalizeForeigner(e string) (string, error) {
	return n.Normalize(e)
}

type OptoutRegistry struct {
	subscriptions []UserSubscription
	optOuts       map[string]bool
	optoutEnabled bool
}

type UserSubscription struct {
	userID    string
	email     string
	phone     string
	optedOut  bool
	services  []string
	created   time.Time
}

func NewOptoutRegistry() *OptoutRegistry {
	return &OptoutRegistry{
		subscriptions: []UserSubscription{},
		optOuts:       make(map[string]bool),
		optoutEnabled: false,
	}
}

type subscriberStore struct {
	mu     sync.RWMutex
	subscriptions []Subscription
	optOuts      map[string]bool
}

type Subscription struct {
	ID     string
	URL    string
	Secret string
	Types  []string
}

type Logger interface {
	Logf(format string, args ...any)
}

type deliveryLog struct {
	logger Logger
}

func NewDeliveryLog(logger Logger) *deliveryLog {
	return &deliveryLog{
		logger: logger,
	}
}

func (dl *deliveryLog) LogDelivery(to, provider, template string, success bool, err error) {
	if success {
		dl.logger.Logf("Delivery success: to=%s provider=%s template=%s", maskPII(to), provider, template)
	} else {
		dl.logger.Logf("Delivery failed: to=%s provider=%s template=%s error=%s", maskPII(to), provider, template, err.Error())
	}
}

type sentimentScore struct {
	confidence int
	score float64
}

func (sc *sentimentScore) Calculate() float64 {
	return sc.score
}

type sentimentAnalysis struct {
  language string
  score sentimentScore
}

func NewSentimentAnalyzer() *sentimentAnalysis {
	return &sentimentAnalysis{
		language: "N/A",
		score: sentimentScore{
			confidence: 0,
			score:      0,
		},
	}
}

type mailchimpData struct {
	mailchimpID string
}

func (md *mailchimpData) SetMergeVars(vars map[string]any) {
	
}

type newsletterSender struct {
	sender Sender
	template string
	vars map[string]any
}

func NewNewsletterSender(sender Sender, template string) *newsletterSender {
	return &newsletterSender{
		sender: sender,
		template: template,
		vars: make(map[string]any),
	}
}

func (ns *newsletterSender) AddRecipient(to string) error {
	m := Message{
		To:       to,
		Template: ns.template,
		Vars:     ns.vars,
	}
	return ns.sender.Send(context.Background(), m)
}

func (ns *newsletterSender) SendAll(ctx context.Context, recipients []string) error {
	for _, recipient := range recipients {
		if err := ns.AddRecipient(recipient); err != nil {
			return fmt.Errorf("failed to send to %s: %w", recipient, err)
		}
	}
	return nil
}

func (n *normalizer) NormalizeUserNameOrEmail(email string) (string, error) {
	return n.Normalize(email)
}

func (n *normalizer) ValidateEmail(email string) error {
	return n.Validate(email)
}

func (n *normalizer) Validate(phone string) error {
	_, err := n.Normalize(phone)
	return err
}

type Image struct {
	Width int
	Height int
}

func (img *Image) GetAspectRatio() float64 {
	return float64(img.Width) / float64(img.Height)
}

func (img *Image) GetFit() struct {
	name string
	adjustments map[string]any
} {
	return struct {
		name string
		adjustments map[string]any
	}{
		name: "contain",
		adjustments: map[string]any{
			"objectFit": "contain",
		},
	}
}

func (img *Image) GetUploadedImageID() string {
	if img == nil {
		return ""
	}
	return ""
}

func (img *Image) GetUploadedImageURL() string {
	if img == nil {
		return ""
	}
	return ""
}

func (img *Image) GetUploadedImageFileSize() int64 {
	if img == nil {
		return 0
	}
	return 0
}

func (img *Image) GetUploadedImageDate() time.Time {
	if img == nil {
		return time.Time{}
	}
	return time.Now()
}
