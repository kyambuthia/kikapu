package routes

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/kyambuthia/kikapu/internal/platform/httpx"
	"github.com/kyambuthia/kikapu/internal/search"
)

type SearchHandler struct {
	SearchStore *search.PostgresStore
}

func NewSearchHandler(store *search.PostgresStore) *SearchHandler {
	return &SearchHandler{SearchStore: store}
}

func (h *SearchHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/search", h.search)
}

func (h *SearchHandler) search(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", httpx.RequestID(r))
		return
	}

	q := r.URL.Query().Get("q")
	if q == "" {
		httpx.WriteError(w, http.StatusBadRequest, "validation_error", "query parameter 'q' is required", httpx.RequestID(r))
		return
	}

	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if limitStr != "" {
		parsedLimit, err := strconv.Atoi(limitStr)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_limit", "limit must be a valid integer", httpx.RequestID(r))
			return
		}
		limit = parsedLimit
	}

	if limit < 1 || limit > 100 {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_limit", "limit must be between 1 and 100", httpx.RequestID(r))
		return
	}

	cursor := r.URL.Query().Get("cursor")
	statusFilter := r.URL.Query().Get("status")

	filters := search.Filters{
		Status: statusFilter,
	}

	page := search.Page{
		Limit:  limit,
		Cursor: cursor,
	}

	results, err := h.SearchStore.Query(r.Context(), q, filters, page)
	if err != nil {
		if errors.Is(err, search.ErrInvalidQuery) {
			httpx.WriteError(w, http.StatusBadRequest, "validation_error", "invalid query", httpx.RequestID(r))
			return
		}
		if errors.Is(err, search.ErrInvalidCursor) {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_cursor", "invalid cursor", httpx.RequestID(r))
			return
		}
		if errors.Is(err, search.ErrInvalidLimit) {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_limit", "invalid limit", httpx.RequestID(r))
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "internal server error", httpx.RequestID(r))
		return
	}

	httpx.WriteJSON(w, http.StatusOK, results)
}
