// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestTerraformCLIInventory(t *testing.T) {
	calls := 0
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls++
		if req.Method != "GET" || req.URL.Path != "/api/v1/library/custom-scripts" {
			t.Error("inventory made a write or used wrong endpoint")
			w.WriteHeader(403)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"next":null,"results":[{"id":"`+syntheticID+`","name":"Synthetic script","script":"synthetic-private-content","enrollment_code":"654321"}]}`)
	}))
	write("data \"iru_inventory\" \"example\" {\n kind = \"custom_scripts\"\n}\n")
	run(0, "apply", "-auto-approve", "-no-color")
	state := run(0, "show", "-json")
	if strings.Contains(state, "synthetic-private-content") || strings.Contains(state, "654321") || !strings.Contains(state, "Synthetic script") {
		t.Fatal("inventory state projection was incorrect")
	}
	run(0, "plan", "-no-color", "-detailed-exitcode")
	if calls != 2 {
		t.Fatal("unexpected inventory reads")
	}
}
