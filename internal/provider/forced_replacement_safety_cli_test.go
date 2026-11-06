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

// All records, Apple-token bytes and API operations below are synthetic. This
// regression records the tainted-state limitation explicitly; protected Core
// plans must never reach a replacement create.
func TestTerraformCLIReplacementGuardsAndTaintLimitation(t *testing.T) {
	for _, family := range []string{"blueprint", "ade_integration"} {
		for _, mode := range []string{"force", "taint", "trigger"} {
			for _, createBeforeDestroy := range []bool{false, true} {
				for _, preventDestroy := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/create_before_destroy=%t/prevent_destroy=%t", family, mode, createBeforeDestroy, preventDestroy), func(t *testing.T) {
						var mu sync.Mutex
						creates, deletes, patches := 0, 0, 0
						objects := map[string]map[string]any{}
						kind, endpoint, createPath, success, diagnostic := "iru_blueprint", "/api/v1/blueprints", "/api/v1/blueprints", 201, "Blueprint deletion blocked"
						if family == "ade_integration" {
							kind, endpoint, createPath, success, diagnostic = "iru_ade_integration", "/api/v1/integrations/apple/ade", "/api/v1/integrations/apple/ade/", 200, "ADE integration deletion blocked"
						}
						run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							mu.Lock()
							defer mu.Unlock()
							if r.Header.Get("Authorization") != "Bearer "+cliToken {
								t.Error("unexpected synthetic auth")
								w.WriteHeader(401)
								return
							}
							w.Header().Set("Content-Type", "application/json")
							if r.Method == http.MethodPost && r.URL.Path == createPath {
								if family == "blueprint" {
									if r.ParseForm() != nil || r.Form.Get("type") != "map" || r.Form.Get("enrollment_code.is_active") != "false" {
										t.Error("unexpected synthetic map create")
										w.WriteHeader(400)
										return
									}
								} else {
									if r.ParseMultipartForm(1024*1024) != nil {
										t.Error("invalid synthetic ADE create")
										w.WriteHeader(400)
										return
									}
									defer r.MultipartForm.RemoveAll()
								}
								creates++
								id := syntheticID
								if creates > 1 {
									id = syntheticCategory
								}
								value := map[string]any{"id": id}
								if family == "blueprint" {
									value["name"], value["type"], value["description"], value["color"], value["icon"] = r.FormValue("name"), "map", "", "aqua-800", "ss-files"
									value["enrollment_code"] = map[string]any{"code": "000123", "is_active": false}
								} else {
									value["blueprint_id"], value["use_blueprint_routing"] = syntheticNoteDevice, false
									value["defaults"] = map[string]string{"email": r.FormValue("email"), "phone": r.FormValue("phone")}
									value["server_name"], value["access_token_expiry"] = "Synthetic server", "2027-11-06T00:00:00Z"
								}
								objects[id] = value
								w.WriteHeader(success)
								_ = json.NewEncoder(w).Encode(value)
								return
							}
							if r.Method == http.MethodDelete {
								deletes++
							} else if r.Method == http.MethodPatch {
								patches++
							}
							id := strings.TrimPrefix(r.URL.Path, endpoint+"/")
							value := objects[id]
							if r.Method != http.MethodGet || value == nil {
								w.WriteHeader(405)
								return
							}
							_ = json.NewEncoder(w).Encode(value)
						}))
						config := func(changed bool) {
							lifecycle, extra := "", ""
							if createBeforeDestroy {
								lifecycle += " create_before_destroy=true\n"
							}
							if preventDestroy {
								lifecycle += " prevent_destroy=true\n"
							}
							if mode == "trigger" {
								lifecycle += " replace_triggered_by=[terraform_data.trigger]\n"
								input := "first"
								if changed {
									input = "next"
								}
								extra = fmt.Sprintf("resource \"terraform_data\" \"trigger\" { input=%q }\n", input)
							}
							fields := " name=\"Synthetic original\"\n type=\"map\"\n enrollment_active=false\n"
							if changed {
								fields = strings.Replace(fields, "original", "replacement", 1)
							}
							if family == "ade_integration" {
								fields = fmt.Sprintf(" blueprint_id=%q\n email=\"synthetic@example.invalid\"\n phone=\"synthetic phone\"\n server_token=%q\n", syntheticNoteDevice, base64.StdEncoding.EncodeToString([]byte("synthetic Apple token")))
							}
							if lifecycle != "" {
								fields += " lifecycle {\n" + lifecycle + " }\n"
							}
							write(fmt.Sprintf("resource %q \"example\" {\n%s}\n%s", kind, fields, extra))
						}
						config(false)
						run(0, "apply", "-auto-approve", "-no-color")
						config(true)
						if mode == "taint" {
							run(0, "taint", kind+".example")
						}
						args := []string{"plan", "-no-color", "-detailed-exitcode", "-out=forced.plan"}
						if mode == "force" {
							args = append(args, "-replace="+kind+".example", "-refresh=false")
						}
						if mode != "taint" || preventDestroy {
							out := run(1, args...)
							if !strings.Contains(out, "replacement blocked") && !strings.Contains(out, "Instance cannot be destroyed") {
								t.Fatal("replacement plan lacked the provider/Core guard")
							}
						} else {
							run(2, args...)
							out := run(1, "apply", "-no-color", "forced.plan")
							if !strings.Contains(out, diagnostic) {
								t.Fatal("tainted replacement did not reach the independent deletion block")
							}
						}
						var state struct {
							Resources []struct {
								Type      string
								Instances []struct{ Attributes map[string]any }
							}
						}
						if json.Unmarshal([]byte(run(0, "state", "pull")), &state) != nil {
							t.Fatal("replacement outcome lost local state")
						}
						ids := map[string]bool{}
						for _, r := range state.Resources {
							if r.Type == kind {
								for _, i := range r.Instances {
									id, ok := i.Attributes["id"].(string)
									if !ok || ids[id] {
										t.Fatal("replacement retained an invalid/duplicate state locator")
									}
									ids[id] = true
								}
							}
						}
						wantInstances := 1
						if mode == "taint" && createBeforeDestroy && !preventDestroy {
							wantInstances = 2
						}
						if len(ids) != wantInstances || !ids[syntheticID] || (wantInstances == 2 && !ids[syntheticCategory]) {
							t.Fatal("replacement failure did not retain every observed object locator")
						}
						mu.Lock()
						defer mu.Unlock()
						t.Logf("synthetic creates=%d deletes=%d patches=%d retained_records=%d", creates, deletes, patches, len(objects))
						if deletes != 0 || patches != 0 || objects[syntheticID] == nil {
							t.Fatal("the destructive guard allowed mutation/removal of the original record")
						}
						wantCreates := 1
						if mode == "taint" && createBeforeDestroy && !preventDestroy {
							wantCreates = 2
						}
						if creates != wantCreates {
							t.Fatal("replacement guard/known taint limitation changed")
						}
					})
				}
			}
		}
	}
}
