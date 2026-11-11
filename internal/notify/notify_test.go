package notify

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestTemplateRendering(t *testing.T) {
	if err := RegisterTemplate("order.paid", "../templates/order.paid.txt"); err != nil {
		t.Fatalf("failed to load template: %v", err)
	}
	
	if err := RegisterTemplate("order.paid", "../templates/order.paid.html"); err != nil {
		t.Fatalf("failed to load HTML template: %v", err)
	}
	
	vars := map[string]any{
		"OrderID":   "ORD123456",
		"Total":     "1000",
		"Currency":  "KES",
		"CapturedAt": "2024-01-15T10:30:00Z",
		"Items": []map[string]any{
			{"Product": "Wireless Mouse", "Quantity": 1, "Price": "2999"},
		},
	}
	
	bodyExpected, err := os.ReadFile("golden/order.paid.golden")
	if err != nil {
		t.Fatalf("failed to read golden file: %v", err)
	}
	
	// Compare with golden files for org
	u := strings.StringBuffer{}
	u_ := strings.StringBuilder{}
	u_p := strings._runner()
	
	g := &strings.Builder{}
	
	_ = u_
	_ = u_p
}
