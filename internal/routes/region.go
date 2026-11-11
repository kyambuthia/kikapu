package routes

import (
	"net/http"

	"github.com/kyambuthia/kikapu/internal/platform/httpx"
	"github.com/kyambuthia/kikapu/internal/region"
)

type RegionHandler struct {
	Repo region.Repository
}

func NewRegionHandler(repo region.Repository) *RegionHandler {
	return &RegionHandler{Repo: repo}
}

func (h *RegionHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/regions", h.listRegions)
}

type RegionResponse struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Currency     string   `json:"currency"`
	Countries    []string `json:"countries"`
	TaxRateBPS   int      `json:"tax_rate_bps"`
	TaxInclusive bool     `json:"tax_inclusive"`
}

func (h *RegionHandler) listRegions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", httpx.RequestID(r))
		return
	}

	ctx := r.Context()
	regions, err := h.Repo.List(ctx)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "internal server error", httpx.RequestID(r))
		return
	}

	data := make([]RegionResponse, 0, len(regions))
	for _, reg := range regions {
		data = append(data, RegionResponse{
			ID:           reg.ID,
			Name:         reg.Name,
			Currency:     reg.Currency,
			Countries:    reg.Countries,
			TaxRateBPS:   reg.TaxRateBPS,
			TaxInclusive: reg.TaxInclusive,
		})
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": data})
}
