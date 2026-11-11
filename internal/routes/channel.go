package routes

import (
	"net/http"

	"github.com/kyambuthia/kikapu/internal/channel"
	"github.com/kyambuthia/kikapu/internal/platform/httpx"
)

type ChannelHandler struct {
	Repo channel.Repository
}

func NewChannelHandler(repo channel.Repository) *ChannelHandler {
	return &ChannelHandler{Repo: repo}
}

func (h *ChannelHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/channels", h.listChannels)
}

type ChannelResponse struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Slug    string   `json:"slug"`
	Regions []string `json:"regions"`
}

func (h *ChannelHandler) listChannels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", httpx.RequestID(r))
		return
	}

	ctx := r.Context()
	channels, err := h.Repo.List(ctx)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "internal server error", httpx.RequestID(r))
		return
	}

	data := make([]ChannelResponse, 0, len(channels))
	for _, ch := range channels {
		data = append(data, ChannelResponse{
			ID:      ch.ID,
			Name:    ch.Name,
			Slug:    ch.Slug,
			Regions: ch.Regions,
		})
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": data})
}
