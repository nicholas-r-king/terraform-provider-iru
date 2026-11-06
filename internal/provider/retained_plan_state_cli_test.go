// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func TestTerraformCLIRetainedPlanMetadataPreservesNormalLifecycle(t *testing.T) {
	for _, family := range []string{"blueprint", "ade_integration"} {
		t.Run(family, func(t *testing.T) {
			var mu sync.Mutex
			var object map[string]any
			creates, patches, deletes := 0, 0, 0
			fault := ""
			kind, endpoint, createPath, success := "iru_blueprint", "/api/v1/blueprints", "/api/v1/blueprints", 201
			if family == "ade_integration" {
				kind, endpoint, createPath, success = "iru_ade_integration", "/api/v1/integrations/apple/ade", "/api/v1/integrations/apple/ade/", 200
			}
			run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet && fault != "" {
					if fault == "permission" {
						w.WriteHeader(403)
					} else {
						_, _ = w.Write([]byte("synthetic-malformed"))
					}
					return
				}
				if r.Method == http.MethodPost && r.URL.Path == createPath {
					if object != nil {
						t.Error("ordinary lifecycle attempted a duplicate create")
						w.WriteHeader(409)
						return
					}
					creates++
					id := syntheticID
					if creates == 2 {
						id = syntheticCategory
					}
					object = map[string]any{"id": id}
					if family == "blueprint" {
						_ = r.ParseForm()
						object["name"], object["type"], object["description"], object["color"], object["icon"] = r.FormValue("name"), "map", "", "aqua-800", "ss-files"
						object["enrollment_code"] = map[string]any{"code": "000123", "is_active": false}
					} else {
						if r.ParseMultipartForm(1024*1024) != nil {
							t.Error("invalid synthetic token form")
							w.WriteHeader(400)
							return
						}
						defer r.MultipartForm.RemoveAll()
						object["blueprint_id"], object["use_blueprint_routing"] = syntheticNoteDevice, false
						object["defaults"] = map[string]string{"email": r.FormValue("email"), "phone": r.FormValue("phone")}
						object["server_name"], object["access_token_expiry"] = "Synthetic", "2027-11-06T00:00:00Z"
					}
					w.WriteHeader(success)
				} else {
					if object == nil {
						w.WriteHeader(404)
						return
					}
					if r.URL.Path != endpoint+"/"+object["id"].(string) {
						t.Error("unexpected retained-object route")
						w.WriteHeader(405)
						return
					}
					if r.Method == http.MethodPatch {
						patches++
						if family == "blueprint" {
							_ = r.ParseForm()
							object["name"] = r.FormValue("name")
						} else {
							var fields map[string]any
							_ = json.NewDecoder(r.Body).Decode(&fields)
							object["defaults"] = map[string]any{"email": fields["email"], "phone": fields["phone"]}
						}
					} else if r.Method != http.MethodGet {
						deletes++
						w.WriteHeader(405)
						return
					}
				}
				_ = json.NewEncoder(w).Encode(object)
			}))
			config := func(changed bool) {
				fields := " name=\"Synthetic original\"\n type=\"map\"\n"
				if changed {
					fields = strings.Replace(fields, "original", "changed", 1)
				}
				if family == "ade_integration" {
					email := "synthetic@example.invalid"
					if changed {
						email = "synthetic-changed@example.invalid"
					}
					fields = fmt.Sprintf(" blueprint_id=%q\n email=%q\n phone=\"synthetic phone\"\n server_token=%q\n", syntheticNoteDevice, email, base64.StdEncoding.EncodeToString([]byte("synthetic Apple token")))
				}
				write(fmt.Sprintf("resource %q \"example\" {\n%s}\n", kind, fields))
			}
			config(false)
			run(0, "apply", "-auto-approve", "-no-color")
			config(true)
			run(0, "apply", "-auto-approve", "-no-color")
			run(0, "plan", "-no-color", "-detailed-exitcode")
			var state struct {
				Resources []struct{ Instances []struct{ Private string } }
			}
			if json.Unmarshal([]byte(run(0, "state", "pull")), &state) != nil || len(state.Resources) != 1 || len(state.Resources[0].Instances) != 1 {
				t.Fatal("normal update lost state")
			}
			raw, err := base64.StdEncoding.DecodeString(state.Resources[0].Instances[0].Private)
			var private map[string][]byte
			if err != nil || json.Unmarshal(raw, &private) != nil || len(private) != 1 || string(private[retainedExistingPlanKey]) != "true" {
				t.Fatal("provider-private metadata was not exactly the nonsecret boolean marker")
			}
			before := run(0, "state", "pull")
			for _, failure := range []string{"permission", "malformed"} {
				mu.Lock()
				fault = failure
				mu.Unlock()
				run(1, "plan", "-no-color")
				if run(0, "state", "pull") != before {
					t.Fatal("failed read changed retained-plan metadata or stored attributes")
				}
			}
			mu.Lock()
			fault = ""
			object = nil // Model an independently absent object, not a DELETE.
			mu.Unlock()
			run(2, "plan", "-no-color", "-detailed-exitcode")
			run(0, "apply", "-auto-approve", "-no-color")
			run(0, "plan", "-no-color", "-detailed-exitcode")
			mu.Lock()
			defer mu.Unlock()
			if creates != 2 || patches != 1 || deletes != 0 || object == nil || object["id"] != syntheticCategory {
				t.Fatal("retained-plan marker blocked ordinary creation/update/absence handling or mutated an extra object")
			}
		})
	}
}
