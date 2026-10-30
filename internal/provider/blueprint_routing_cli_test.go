// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"encoding/json"
	"fmt"
	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func TestTerraformCLIBlueprintRoutingSettings(t *testing.T) {
	var mu sync.Mutex
	code, active := "123456", false
	patches, status := 0, 0
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if req.URL.Path != "/api/v1/blueprint-routing/" || req.Header.Get("Authorization") != "Bearer "+cliToken {
			t.Error("unexpected routing request")
			w.WriteHeader(404)
			return
		}
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "GET":
		case "PATCH":
			var input client.BlueprintRouting
			if json.NewDecoder(req.Body).Decode(&input) != nil || input.EnrollmentCode.Code == nil || input.EnrollmentCode.Active == nil {
				t.Error("invalid routing update")
				w.WriteHeader(400)
				return
			}
			patches++
			code, active = *input.EnrollmentCode.Code, *input.EnrollmentCode.Active
		default:
			t.Error("routing settings used an undocumented create/delete")
			w.WriteHeader(405)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"enrollment_code": map[string]any{"code": code, "is_active": active}})
	}))
	config := func(value string) {
		write(fmt.Sprintf("resource \"iru_blueprint_routing\" \"example\" {\n enrollment_code = %q\n enrollment_active = false\n}\n", value))
	}
	config("123456")
	run(0, "validate", "-no-color")
	run(0, "import", "-no-color", "iru_blueprint_routing.example", "tenant")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	if patches != 0 {
		t.Error("routing import performed a write")
	}
	mu.Unlock()
	config("654321")
	out := run(0, "apply", "-auto-approve", "-no-color")
	if strings.Contains(out, "654321") {
		t.Fatal("routing code was not redacted")
	}
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	code, active = "654322", true
	mu.Unlock()
	run(2, "plan", "-no-color", "-detailed-exitcode")
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	status = 403
	mu.Unlock()
	run(1, "plan", "-no-color")
	mu.Lock()
	status = 0
	beforeDestroy := patches
	mu.Unlock()
	run(0, "destroy", "-auto-approve", "-no-color")
	mu.Lock()
	if patches != beforeDestroy || code != "654321" || active {
		t.Error("destroy altered tenant routing settings")
	}
	mu.Unlock()
	// Re-adopting matching singleton settings is also read-only.
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	if patches != beforeDestroy {
		t.Error("adopting matching settings wrote unnecessarily")
	}
	mu.Unlock()
}

func TestTerraformCLIBlueprintRoutingLookup(t *testing.T) {
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != "GET" || req.URL.Path != "/api/v1/blueprint-routing/" {
			t.Error("routing lookup attempted a write")
			w.WriteHeader(403)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"enrollment_code":{"code":"000123","is_active":false}}`)
	}))
	write("data \"iru_blueprint_routing\" \"example\" {}\n")
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	var state struct {
		Values struct {
			Root struct {
				Resources []struct {
					Sensitive map[string]bool `json:"sensitive_values"`
				} `json:"resources"`
			} `json:"root_module"`
		} `json:"values"`
	}
	if json.Unmarshal([]byte(run(0, "show", "-json")), &state) != nil || len(state.Values.Root.Resources) != 1 || !state.Values.Root.Resources[0].Sensitive["enrollment_code"] {
		t.Fatal("routing code lacks sensitive state metadata")
	}
}
