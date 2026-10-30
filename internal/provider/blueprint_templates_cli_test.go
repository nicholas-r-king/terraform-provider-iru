// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestTerraformCLIBlueprintTemplateCatalog(t *testing.T) {
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != "GET" || req.URL.Path != "/api/v1/blueprints/templates/" {
			t.Error("template catalog made wrong request or mutation")
			w.WriteHeader(403)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"next":null,"results":[{"id":1,"name":"Category","templates":[{"id":16,"name":"Synthetic template","params":{"private":"do-not-retain"}}]}]}`)
	}))
	write("data \"iru_blueprint_templates\" \"example\" {}\n")
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	state := run(0, "show", "-json")
	if !strings.Contains(state, "Synthetic template") || strings.Contains(state, "do-not-retain") {
		t.Fatal("template state projection invalid")
	}
}
