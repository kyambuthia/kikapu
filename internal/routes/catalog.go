package routes

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/kyambuthia/kikapu/internal/catalog"
	"github.com/kyambuthia/kikapu/internal/platform/httpx"
)

type CatalogHandler struct {
	Repo catalog.Repository
}

func NewCatalogHandler(repo catalog.Repository) *CatalogHandler {
	return &CatalogHandler{Repo: repo}
}

func (h *CatalogHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/products", h.listProducts)
	mux.HandleFunc("/v1/products/", h.getProduct)
}

type ProductResponse struct {
	ID       string            `json:"id,omitempty"`
	Slug     string            `json:"slug,omitempty"`
	Name     string            `json:"name,omitempty"`
	Status   string            `json:"status,omitempty"`
	Variants []VariantResponse `json:"variants,omitempty"`
}

type VariantResponse struct {
	ID       string      `json:"id,omitempty"`
	SKU      string      `json:"sku,omitempty"`
	Price    interface{} `json:"price,omitempty"`
	Currency string      `json:"currency,omitempty"`
}

type ProductListResponse struct {
	Data       []ProductResponse `json:"data"`
	NextCursor string            `json:"next_cursor,omitempty"`
}

func parseFields(fieldsParam string) map[string]bool {
	if fieldsParam == "" {
		return nil
	}
	fields := make(map[string]bool)
	for _, f := range strings.Split(fieldsParam, ",") {
		fields[strings.TrimSpace(f)] = true
	}
	return fields
}

func parseVariantFields(fieldsParam string) map[string]bool {
	if fieldsParam == "" {
		return nil
	}
	variantFields := make(map[string]bool)
	for _, f := range strings.Split(fieldsParam, ",") {
		f = strings.TrimSpace(f)
		if strings.HasPrefix(f, "variants.") {
			field := strings.TrimPrefix(f, "variants.")
			variantFields[field] = true
		}
	}
	return variantFields
}

func applySparseFieldset(product catalog.Product, fields map[string]bool) ProductResponse {
	return applySparseFieldsetWithVariant(product, fields, nil)
}

func applySparseFieldsetWithVariant(product catalog.Product, fields map[string]bool, variantFields map[string]bool) ProductResponse {
	if fields == nil && variantFields == nil {
		return ProductResponse{
			ID:       product.ID,
			Slug:     product.Slug,
			Name:     product.Name,
			Status:   product.Status,
			Variants: convertVariants(product.Variants, nil),
		}
	}

	var resp ProductResponse

	validProductFields := map[string]bool{
		"id": true, "slug": true, "name": true, "status": true,
	}
	validVariantFields := map[string]bool{
		"id": true, "sku": true, "price": true, "currency": true,
	}

	if fields != nil {
		for field := range fields {
			if !validProductFields[field] && !validVariantFields[field] && !strings.HasPrefix(field, "variants.") {
				return ProductResponse{}
			}
		}

		if fields["id"] {
			resp.ID = product.ID
		}
		if fields["slug"] {
			resp.Slug = product.Slug
		}
		if fields["name"] {
			resp.Name = product.Name
		}
		if fields["status"] {
			resp.Status = product.Status
		}
	}

	if variantFields != nil && len(variantFields) > 0 && len(product.Variants) > 0 {
		variants := convertVariants(product.Variants, variantFields)
		resp.Variants = variants
	}

	return resp
}

func convertVariants(variants []catalog.Variant, fields map[string]bool) []VariantResponse {
	result := make([]VariantResponse, 0, len(variants))
	for _, v := range variants {
		var vr VariantResponse
		if fields == nil {
			vr = VariantResponse{
				ID:       v.ID,
				SKU:      v.SKU,
				Price:    v.Price,
				Currency: v.Currency,
			}
		} else {
			if fields["id"] {
				vr.ID = v.ID
			}
			if fields["sku"] {
				vr.SKU = v.SKU
			}
			if fields["price"] {
				vr.Price = v.Price
			}
			if fields["currency"] {
				vr.Currency = v.Currency
			}
		}
		result = append(result, vr)
	}
	return result
}

func (h *CatalogHandler) listProducts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	ctx := r.Context()

	limitStr := r.URL.Query().Get("limit")
	limit := catalog.DefaultLimit
	if limitStr != "" {
		parsedLimit, err := strconv.Atoi(limitStr)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_limit", "limit must be a valid integer")
			return
		}
		limit = parsedLimit
	}

	if limit < 1 || limit > catalog.MaxLimit {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_limit", "limit must be between 1 and 100")
		return
	}

	cursor := r.URL.Query().Get("cursor")
	fieldsParam := r.URL.Query().Get("fields")
	fields := parseFields(fieldsParam)
	variantFields := parseVariantFields(fieldsParam)

	if fieldsParam != "" {
		validProductFields := map[string]bool{
			"id": true, "slug": true, "name": true, "status": true,
		}
		validVariantFields := map[string]bool{
			"id": true, "sku": true, "price": true, "currency": true,
		}
		for field := range fields {
			baseField := field
			if strings.HasPrefix(field, "variants.") {
				baseField = strings.TrimPrefix(field, "variants.")
			}
			if !validProductFields[baseField] && !validVariantFields[baseField] {
				httpx.WriteError(w, http.StatusBadRequest, "invalid_field", "unknown field: "+field)
				return
			}
		}
	}

	opts := catalog.ListOptions{
		Limit:  limit,
		Cursor: cursor,
	}

	products, nextCursor, err := h.Repo.List(ctx, opts)
	if err != nil {
		if errors.Is(err, catalog.ErrInvalidCursor) {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_cursor", "invalid cursor")
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	data := make([]ProductResponse, 0, len(products))
	for _, p := range products {
		resp := applySparseFieldsetWithVariant(p, fields, variantFields)
		if resp.ID == "" && fields != nil {
			continue
		}
		data = append(data, resp)
	}

	response := ProductListResponse{
		Data:       data,
		NextCursor: nextCursor,
	}

	httpx.WriteJSON(w, http.StatusOK, response)
}

func (h *CatalogHandler) getProduct(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	ctx := r.Context()
	path := r.URL.Path
	if !strings.HasPrefix(path, "/v1/products/") {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "not found")
		return
	}

	slug := strings.TrimPrefix(path, "/v1/products/")
	if slug == "" {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "not found")
		return
	}

	fieldsParam := r.URL.Query().Get("fields")
	fields := parseFields(fieldsParam)
	variantFields := parseVariantFields(fieldsParam)

	if fieldsParam != "" {
		validProductFields := map[string]bool{
			"id": true, "slug": true, "name": true, "status": true,
		}
		validVariantFields := map[string]bool{
			"id": true, "sku": true, "price": true, "currency": true,
		}
		for field := range fields {
			baseField := field
			if strings.HasPrefix(field, "variants.") {
				baseField = strings.TrimPrefix(field, "variants.")
			}
			if !validProductFields[baseField] && !validVariantFields[baseField] {
				httpx.WriteError(w, http.StatusBadRequest, "invalid_field", "unknown field: "+field)
				return
			}
		}
	}

	product, err := h.Repo.Get(ctx, slug)
	if err != nil {
		if errors.Is(err, catalog.ErrProductNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "product_not_found", "product not found")
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	resp := applySparseFieldsetWithVariant(*product, fields, variantFields)
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func WriteJSON(w http.ResponseWriter, status int, data interface{}) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(data)
}
