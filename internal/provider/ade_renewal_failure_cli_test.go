// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// The Apple token and all remote effects are synthetic loopback fixtures. Never
// replay an ambiguous renewal or claim its local version proves remote success.
func TestTerraformCLIADEAmbiguousRenewalRetainsStateAndExcludesToken(t *testing.T) {
	const token = "synthetic-private-apple-renewal-token"
	encodedToken := base64.StdEncoding.EncodeToString([]byte(token))
	for _, fault := range []string{"503", "transport", "malformed-200"} {
		t.Run(fault, func(t *testing.T) {
			var mu sync.Mutex
			posts := 0
			expiry := "2027-10-29T00:00:00Z"
			endpoint := "/api/v1/integrations/apple/ade/" + syntheticID
			run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet && r.URL.Path == endpoint {
					_ = json.NewEncoder(w).Encode(map[string]any{"id": syntheticID, "blueprint_id": syntheticCategory, "use_blueprint_routing": false, "defaults": map[string]string{"email": "synthetic@example.invalid", "phone": "1234567890"}, "server_name": "Synthetic", "access_token_expiry": expiry})
					return
				}
				if r.Method != http.MethodPost || r.URL.Path != endpoint+"/renew" {
					t.Error("unexpected mutation or endpoint")
					w.WriteHeader(405)
					return
				}
				posts++
				if r.ParseMultipartForm(1024*1024) != nil {
					t.Error("invalid renewal multipart")
					w.WriteHeader(400)
					return
				}
				defer r.MultipartForm.RemoveAll()
				file, _, err := r.FormFile("file")
				if err != nil {
					t.Error("missing synthetic token file")
					w.WriteHeader(400)
					return
				}
				data, err := io.ReadAll(file)
				file.Close()
				if err != nil || string(data) != token {
					t.Error("incorrect synthetic renewal token")
					w.WriteHeader(400)
					return
				}
				// The fixture changes before losing/invalidating its response.
				expiry = "2028-10-29T00:00:00Z"
				switch fault {
				case "503":
					w.WriteHeader(503)
					fmt.Fprint(w, `{"private":"synthetic-private-response"}`)
				case "malformed-200":
					fmt.Fprintf(w, `{"id":%q,"private":"synthetic-private-response"}`, syntheticID)
				case "transport":
					h, ok := w.(http.Hijacker)
					if !ok {
						t.Error("fixture cannot drop response")
						w.WriteHeader(500)
						return
					}
					conn, _, err := h.Hijack()
					if err != nil {
						t.Error("fixture cannot hijack connection")
						return
					}
					conn.Close()
				}
			}))
			config := func(version int) {
				write(fmt.Sprintf("variable \"renewal_token\" {\n type=string\n ephemeral=true\n default=null\n}\nresource \"iru_ade_integration\" \"example\" {\n blueprint_id=%q\n email=\"synthetic@example.invalid\"\n phone=\"1234567890\"\n server_token_version=%d\n server_token=var.renewal_token\n}\n", syntheticCategory, version))
			}
			pull := func() map[string]any {
				var state struct {
					Resources []struct {
						Instances []struct{ Attributes map[string]any }
					}
				}
				if json.Unmarshal([]byte(run(0, "state", "pull")), &state) != nil || len(state.Resources) != 1 || len(state.Resources[0].Instances) != 1 {
					t.Fatal("ADE locator missing from persisted state")
				}
				return state.Resources[0].Instances[0].Attributes
			}
			config(0)
			run(0, "import", "-no-color", "iru_ade_integration.example", syntheticID)
			config(1)
			// Command arguments contain only this hardcoded synthetic fixture. Real
			// credentials are never read; production handoff uses a private environment.
			variable := "-var=renewal_token=" + encodedToken
			run(0, "plan", "-no-color", variable, "-out=renewal.tfplan")
			plan := run(0, "show", "-json", "renewal.tfplan")
			if strings.Contains(plan, token) || strings.Contains(plan, encodedToken) {
				t.Fatal("write-only ephemeral renewal value reached saved plan JSON")
			}
			mu.Lock()
			if posts != 0 {
				t.Error("import/plan renewed a token")
			}
			mu.Unlock()
			out := run(1, "apply", "-auto-approve", "-no-color", variable)
			if strings.Contains(out, token) || strings.Contains(out, encodedToken) || strings.Contains(out, "synthetic-private-response") {
				t.Fatal("private renewal data reached diagnostics")
			}
			state := pull()
			if state["id"] != syntheticID || state["server_token_version"] != float64(0) || state["access_token_expiry"] != "2027-10-29T00:00:00Z" {
				t.Fatal("ambiguous renewal lost identity or invented confirmed renewal state")
			}
			raw := run(0, "state", "pull")
			if strings.Contains(raw, token) || strings.Contains(raw, encodedToken) {
				t.Fatal("write-only renewal token persisted")
			}
			mu.Lock()
			if posts != 1 {
				t.Error("ambiguous renewal was replayed")
			}
			mu.Unlock()
			run(0, "apply", "-refresh-only", "-auto-approve", "-no-color")
			state = pull()
			if state["id"] != syntheticID || state["server_token_version"] != float64(0) || state["access_token_expiry"] != "2028-10-29T00:00:00Z" {
				t.Fatal("independent refresh did not preserve local-version uncertainty and observed remote metadata")
			}
			mu.Lock()
			defer mu.Unlock()
			if posts != 1 {
				t.Fatal("read reconciliation renewed a token")
			}
		})
	}
}

// Keep the initial 1.11 release's upstream marks-serialization limitation visible
// instead of skipping ephemeral+sensitive inputs or blaming the provider.
func TestTerraformCLIADEEphemeralSensitiveInputCompatibility(t *testing.T) {
	const token = "synthetic-private-combined-marks-token"
	encodedToken := base64.StdEncoding.EncodeToString([]byte(token))
	var mu sync.Mutex
	writes := 0
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/integrations/apple/ade/"+syntheticID {
			writes++
			w.WriteHeader(405)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": syntheticID, "blueprint_id": syntheticCategory, "use_blueprint_routing": false, "defaults": map[string]string{"email": "synthetic@example.invalid", "phone": "1234567890"}, "server_name": "Synthetic", "access_token_expiry": "2027-10-29T00:00:00Z"})
	}))
	write(fmt.Sprintf("variable \"renewal_token\" {\n type=string\n sensitive=true\n ephemeral=true\n default=null\n}\nresource \"iru_ade_integration\" \"example\" {\n blueprint_id=%q\n email=\"synthetic@example.invalid\"\n phone=\"1234567890\"\n server_token_version=1\n server_token=var.renewal_token\n}\n", syntheticCategory))
	run(0, "import", "-no-color", "iru_ade_integration.example", syntheticID)
	before := run(0, "state", "pull")
	var version struct {
		TerraformVersion string `json:"terraform_version"`
	}
	if json.Unmarshal([]byte(run(0, "version", "-json")), &version) != nil {
		t.Fatal("Terraform version unavailable")
	}
	want := 0
	if version.TerraformVersion == "1.11.0" {
		want = 1
	}
	output := run(want, "plan", "-no-color", "-var=renewal_token="+encodedToken, "-out=combined-marks.tfplan")
	if strings.Contains(output, token) || strings.Contains(output, encodedToken) {
		t.Fatal("combined-marks input reached human diagnostics")
	}
	if want == 1 {
		if !strings.Contains(output, "cannot serialize value marked") || !strings.Contains(output, "Ephemeral") || !strings.Contains(output, "Sensitive") {
			t.Fatal("unexpected initial-1.11 failure; do not hide another defect")
		}
		t.Log("Terraform 1.11.0 reproduces upstream issue36603; combining ephemeral+sensitive requires patched Terraform. No API writes occurred.")
	} else {
		plan := run(0, "show", "-json", "combined-marks.tfplan")
		if strings.Contains(plan, token) || strings.Contains(plan, encodedToken) {
			t.Fatal("combined-marks input persisted in saved plan JSON")
		}
	}
	if run(0, "state", "pull") != before {
		t.Fatal("plan changed imported ADE state")
	}
	mu.Lock()
	defer mu.Unlock()
	if writes != 0 {
		t.Fatal("combined-marks plan attempted a mutation")
	}
}
