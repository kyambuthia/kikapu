package httpx

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteJSON(t *testing.T) {
	tests := []struct {
		name     string
		data     interface{}
		status   int
		wantBody string
	}{
		{
			name:     "simple object",
			data:     map[string]string{"status": "ok"},
			status:   http.StatusOK,
			wantBody: `{"status":"ok"}`,
		},
		{
			name:     "nil data",
			data:     nil,
			status:   http.StatusOK,
			wantBody: "null",
		},
		{
			name:     "number",
			data:     42,
			status:   http.StatusOK,
			wantBody: "42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			WriteJSON(w, tt.status, tt.data)

			if w.Code != tt.status {
				t.Errorf("WriteJSON() status = %v, want %v", w.Code, tt.status)
			}

			if ct := w.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("WriteJSON() Content-Type = %v, want application/json", ct)
			}

			got := strings.TrimSpace(w.Body.String())
			if got != tt.wantBody {
				t.Errorf("WriteJSON() body = %q, want %q", got, tt.wantBody)
			}
		})
	}
}

func TestWriteError(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, http.StatusBadRequest, "invalid_input", "The request body is invalid")

	if w.Code != http.StatusBadRequest {
		t.Errorf("WriteError() status = %v, want %v", w.Code, http.StatusBadRequest)
	}

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("WriteError() Content-Type = %v, want application/json", ct)
	}

	var envelope ErrorEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if envelope.Error.Code != "invalid_input" {
		t.Errorf("WriteError() code = %v, want invalid_input", envelope.Error.Code)
	}

	if envelope.Error.Message != "The request body is invalid" {
		t.Errorf("WriteError() message = %v, want 'The request body is invalid'", envelope.Error.Message)
	}
}

func TestErrorEnvelope(t *testing.T) {
	envelope := ErrorEnvelope{
		Error: ErrorDetail{
			Code:    "not_found",
			Message: "Resource not found",
		},
	}

	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("Failed to marshal envelope: %v", err)
	}

	want := `{"error":{"code":"not_found","message":"Resource not found"}}`
	var buf bytes.Buffer
	if err := json.Compact(&buf, data); err != nil {
		t.Fatalf("Failed to compact JSON: %v", err)
	}

	if got := buf.String(); got != want {
		t.Errorf("ErrorEnvelope JSON = %v, want %v", got, want)
	}
}

func TestRequestIDHeader(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{
			name:   "with header",
			header: "req-123",
			want:   "req-123",
		},
		{
			name:   "without header",
			header: "",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				req.Header.Set("X-Request-ID", tt.header)
			}

			got := RequestIDHeader(req)
			if got != tt.want {
				t.Errorf("RequestIDHeader() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoggingMiddleware(t *testing.T) {
	var logOutput []byte

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("test"))
	})

	middleware := LoggingMiddleware(handler)

	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Header.Set("X-Request-ID", "test-123")
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	logOutput = w.Body.Bytes()

	if len(logOutput) == 0 {
		t.Error("Expected logged output")
	}
}
