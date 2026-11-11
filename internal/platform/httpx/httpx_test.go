package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type mockLogger struct {
	lastMsg  string
	lastArgs []interface{}
}

func (m *mockLogger) Info(msg string, args ...interface{}) {
	m.lastMsg = msg
	m.lastArgs = args
}

func TestWriteJSON(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		data       interface{}
		wantBody   string
		wantHeader string
	}{
		{
			name:       "success with data",
			status:     http.StatusOK,
			data:       map[string]string{"key": "value"},
			wantBody:   `{"key":"value"}`,
			wantHeader: "application/json; charset=utf-8",
		},
		{
			name:       "204 no content",
			status:     http.StatusNoContent,
			data:       nil,
			wantBody:   "",
			wantHeader: "application/json; charset=utf-8",
		},
		{
			name:       "error response",
			status:     http.StatusBadRequest,
			data:       map[string]int{"code": 400},
			wantBody:   `{"code":400}`,
			wantHeader: "application/json; charset=utf-8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			err := WriteJSON(w, tt.status, tt.data)
			if err != nil {
				t.Errorf("WriteJSON() error = %v", err)
			}
			if w.Code != tt.status {
				t.Errorf("WriteJSON() status = %v, want %v", w.Code, tt.status)
			}
			if ct := w.Header().Get("Content-Type"); ct != tt.wantHeader {
				t.Errorf("WriteJSON() Content-Type = %v, want %v", ct, tt.wantHeader)
			}
			if nosniff := w.Header().Get("X-Content-Type-Options"); nosniff != "nosniff" {
				t.Errorf("WriteJSON() X-Content-Type-Options = %v, want nosniff", nosniff)
			}
			if tt.wantBody != "" {
				if w.Body.String() != tt.wantBody+"\n" {
					t.Errorf("WriteJSON() body = %v, want %v", w.Body.String(), tt.wantBody+"\n")
				}
			}
		})
	}
}

func TestWriteError(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		code      string
		message   string
		requestID string
		want      ErrorEnvelope
	}{
		{
			name:      "error with request id",
			status:    http.StatusBadRequest,
			code:      "invalid_input",
			message:   "The request was invalid",
			requestID: "test-request-123",
			want: ErrorEnvelope{
				Error: ErrorDetail{
					Code:      "invalid_input",
					Message:   "The request was invalid",
					RequestID: "test-request-123",
				},
			},
		},
		{
			name:      "error without request id",
			status:    http.StatusInternalServerError,
			code:      "internal_error",
			message:   "Something went wrong",
			requestID: "",
			want: ErrorEnvelope{
				Error: ErrorDetail{
					Code:      "internal_error",
					Message:   "Something went wrong",
					RequestID: "",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			err := WriteError(w, tt.status, tt.code, tt.message, tt.requestID)
			if err != nil {
				t.Errorf("WriteError() error = %v", err)
			}
			if w.Code != tt.status {
				t.Errorf("WriteError() status = %v, want %v", w.Code, tt.status)
			}
			var got ErrorEnvelope
			json.Unmarshal(w.Body.Bytes(), &got)
			if got.Error.Code != tt.want.Error.Code {
				t.Errorf("WriteError() code = %v, want %v", got.Error.Code, tt.want.Error.Code)
			}
			if got.Error.Message != tt.want.Error.Message {
				t.Errorf("WriteError() message = %v, want %v", got.Error.Message, tt.want.Error.Message)
			}
		})
	}
}

func TestErrorEnvelope(t *testing.T) {
	envelope := ErrorEnvelope{
		Error: ErrorDetail{
			Code:      "test_code",
			Message:   "test message",
			RequestID: "req-123",
		},
	}

	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	want := `{"error":{"code":"test_code","message":"test message","request_id":"req-123"}}`
	if string(data) != want {
		t.Errorf("Marshal() = %s, want %s", data, want)
	}
}

func TestRequestID(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*http.Request)
		want  string
	}{
		{
			name: "from header",
			setup: func(r *http.Request) {
				r.Header.Set("X-Request-ID", "header-id")
			},
			want: "header-id",
		},
		{
			name: "from context",
			setup: func(r *http.Request) {
				ctx := SetRequestID(r.Context(), "ctx-id")
				*r = *r.WithContext(ctx)
			},
			want: "ctx-id",
		},
		{
			name:  "empty when not set",
			setup: func(r *http.Request) {},
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			tt.setup(req)
			if got := RequestID(req); got != tt.want {
				t.Errorf("RequestID() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRequestIDMiddleware(t *testing.T) {
	t.Run("uses existing request id", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := RequestID(r); got != "existing-id" {
				t.Errorf("RequestID() = %v, want existing-id", got)
			}
		})
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Request-ID", "existing-id")
		rec := httptest.NewRecorder()
		RequestIDMiddleware(handler).ServeHTTP(rec, req)
		if rec.Header().Get("X-Request-ID") != "existing-id" {
			t.Errorf("X-Request-ID header = %v, want existing-id", rec.Header().Get("X-Request-ID"))
		}
	})

	t.Run("generates request id when missing", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := RequestID(r); got == "" {
				t.Error("RequestID() empty, want generated id")
			}
			if len(RequestID(r)) != 32 {
				t.Errorf("RequestID() length = %d, want 32", len(RequestID(r)))
			}
		})
		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		RequestIDMiddleware(handler).ServeHTTP(rec, req)
		if rec.Header().Get("X-Request-ID") == "" {
			t.Error("X-Request-ID header empty")
		}
	})
}

func TestLoggingMiddleware(t *testing.T) {
	logger := &mockLogger{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	req := httptest.NewRequest("POST", "/test", nil)
	req.Header.Set("X-Request-ID", "test-id")
	rec := httptest.NewRecorder()

	LoggingMiddleware(logger)(handler).ServeHTTP(rec, req)

	if logger.lastMsg != "request" {
		t.Errorf("logger message = %v, want 'request'", logger.lastMsg)
	}
}
