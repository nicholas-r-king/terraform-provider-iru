// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

func TestTerraformCLIApp(t *testing.T) {
	model := syntheticApp()
	object := &client.App{ID: syntheticID, AppInput: model.input()}
	var mu sync.Mutex
	methods := map[string]int{}
	replacements := 0
	malformedCreate := false
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if req.URL.Path != "/api/v1/library/custom-apps" && req.URL.Path != "/api/v1/library/custom-apps/"+syntheticID {
			t.Error("unexpected app endpoint, including an unrequested upload")
			w.WriteHeader(404)
			return
		}
		if req.Header.Get("Authorization") != "Bearer "+cliToken {
			t.Error("missing mock bearer header")
			w.WriteHeader(401)
			return
		}
		methods[req.Method]++
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "GET":
			if object == nil {
				w.WriteHeader(404)
				return
			}
		case "POST", "PATCH":
			if req.Method == "PATCH" && object == nil {
				w.WriteHeader(404)
				return
			}
			if req.ParseForm() != nil || req.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
				t.Error("invalid app form")
				w.WriteHeader(400)
				return
			}
			input := model.input()
			input.Name, input.InstallType, input.InstallEnforcement = req.Form.Get("name"), req.Form.Get("install_type"), req.Form.Get("install_enforcement")
			input.AuditScript, input.PreinstallScript, input.PostinstallScript, input.UnzipLocation = req.Form.Get("audit_script"), req.Form.Get("preinstall_script"), req.Form.Get("postinstall_script"), req.Form.Get("unzip_location")
			input.Active, input.Restart = req.Form.Get("active") == "true", req.Form.Get("restart") == "true"
			if object != nil {
				input.FileKey = object.FileKey
			}
			if key, ok := req.Form["file_key"]; ok {
				input.FileKey = key[0]
				if req.Method == "PATCH" {
					replacements++
				}
			}
			if input.Validate() != nil {
				t.Error("invalid app payload")
				w.WriteHeader(400)
				return
			}
			object = &client.App{ID: syntheticID, AppInput: input}
			if req.Method == "POST" {
				w.WriteHeader(201)
				if malformedCreate {
					malformedCreate = false
					_, _ = fmt.Fprintf(w, `{"id":%q,"name":"Synthetic partial"}`, syntheticID)
					return
				}
			}
		case "DELETE":
			object = nil
			w.WriteHeader(204)
			return
		default:
			w.WriteHeader(405)
			return
		}
		returned := *object
		returned.PreinstallScript = strings.TrimSpace(returned.PreinstallScript)
		if err := json.NewEncoder(w).Encode(returned); err != nil {
			t.Error("app mock encode failed")
		}
	}))
	preinstall := ""
	config := func(name, key, kind string) {
		write(fmt.Sprintf("resource \"iru_custom_app\" \"example\" {\n name = %q\n file_key = %q\n install_type = %q\n preinstall_script = %q\n}\n", name, key, kind, preinstall))
	}
	key := model.FileKey.ValueString()
	config("Synthetic app", key, "package")
	run(0, "validate", "-no-color")
	run(0, "import", "-no-color", "iru_custom_app.example", strings.ToUpper(syntheticID))
	run(0, "plan", "-no-color", "-detailed-exitcode")
	preinstall = "#!/bin/sh\ncat <<'EOF'\nsynthetic\nEOF\n"
	config("Synthetic renamed", key, "package")
	run(2, "plan", "-no-color", "-detailed-exitcode")
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	object.Name = "Synthetic external drift"
	mu.Unlock()
	run(2, "plan", "-no-color", "-detailed-exitcode")
	run(0, "apply", "-auto-approve", "-no-color")
	config("Synthetic renamed", "companies/synthetic/library/custom_apps/replacement.pkg", "package")
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	if replacements != 1 || methods["POST"] != 0 {
		t.Error("metadata updates replaced installer or import created an app")
	}
	mu.Unlock()
	run(0, "destroy", "-auto-approve", "-no-color")
	mu.Lock()
	malformedCreate = true
	mu.Unlock()
	run(1, "apply", "-auto-approve", "-no-color")
	if !strings.Contains(run(0, "state", "show", "-no-color", "iru_custom_app.example"), syntheticID) {
		t.Fatal("Terraform did not persist the partial creation ID")
	}
	run(2, "plan", "-no-color", "-detailed-exitcode")
	// Model explicit recovery after checking the persisted object matches HCL.
	run(0, "untaint", "-no-color", "iru_custom_app.example")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	object = nil
	mu.Unlock()
	run(0, "apply", "-refresh-only", "-auto-approve", "-no-color")
	if strings.TrimSpace(run(0, "state", "list")) != "" {
		t.Fatal("404 app refresh did not remove state")
	}
	config("Synthetic app", key, "unsupported")
	run(1, "validate", "-no-color")
	mu.Lock()
	defer mu.Unlock()
	for _, method := range []string{"POST", "GET", "PATCH", "DELETE"} {
		if methods[method] == 0 {
			t.Errorf("app mock did not exercise %s", method)
		}
	}
}
