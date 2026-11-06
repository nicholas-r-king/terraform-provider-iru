// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// Synthetic records only. A selector replacement relinquishes a projection; it
// must never create/delete a device or replay the prior device's observed fields.
func TestTerraformCLIDeviceSelectorReplacementReleaseSafety(t *testing.T) {
	const first = "aaaaaaaa-bbbb-4ccc-8ddd-111111111111"
	const second = "bbbbbbbb-cccc-4ddd-8eee-222222222222"
	for _, ade := range []bool{false, true} {
		for _, createBeforeDestroy := range []bool{false, true} {
			name := fmt.Sprintf("ade=%t/create_before_destroy=%t", ade, createBeforeDestroy)
			t.Run(name, func(t *testing.T) {
				var mu sync.Mutex
				writes := 0
				reads := map[string]int{}
				kind, prefix := "iru_device_settings", "/api/v1/devices/"
				if ade {
					kind, prefix = "iru_ade_device_settings", "/api/v1/integrations/apple/ade/devices/"
				}
				objects := map[string]map[string]any{}
				for _, id := range []string{first, second} {
					value := map[string]any{"blueprint_id": id, "asset_tag": "asset-" + id}
					if ade {
						value["id"], value["user_id"], value["use_blueprint_routing"], value["override_dep_profile_id"] = id, id, false, nil
					} else {
						value["device_id"], value["user"], value["tags"] = id, map[string]any{"id": id}, []string{"tag-" + id}
					}
					objects[id] = value
				}
				run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					defer mu.Unlock()
					if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, prefix) || r.Header.Get("Authorization") != "Bearer "+cliToken {
						writes++
						w.WriteHeader(405)
						return
					}
					id := strings.TrimPrefix(r.URL.Path, prefix)
					value, exists := objects[id]
					if !exists {
						w.WriteHeader(404)
						return
					}
					reads[id]++
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(value)
				}))
				config := func(id string) {
					lifecycle := ""
					if createBeforeDestroy {
						lifecycle = " lifecycle { create_before_destroy=true }\n"
					}
					write(fmt.Sprintf("resource %q \"example\" {\n device_id=%q\n%s}\n", kind, id, lifecycle))
				}
				planActions := func() []string {
					var document struct {
						Changes []struct {
							Address string
							Change  struct{ Actions []string }
						} `json:"resource_changes"`
					}
					if json.Unmarshal([]byte(run(0, "show", "-json", "selector.tfplan")), &document) != nil || len(document.Changes) != 1 || document.Changes[0].Address != kind+".example" {
						t.Fatal("unexpected selector plan shape")
					}
					return document.Changes[0].Change.Actions
				}
				assertState := func(id, spelling string) {
					var state struct {
						Values struct {
							Root struct {
								Resources []struct{ Values map[string]any }
							} `json:"root_module"`
						}
					}
					if json.Unmarshal([]byte(run(0, "show", "-json")), &state) != nil || len(state.Values.Root.Resources) != 1 {
						t.Fatal("selector state missing")
					}
					value := state.Values.Root.Resources[0].Values
					if value["id"] != id || value["device_id"] != spelling || value["blueprint_id"] != id || value["user_id"] != id || value["asset_tag"] != "asset-"+id {
						t.Fatal("selector adoption retained another record's locator or unmanaged settings")
					}
					if !ade {
						if tags, ok := value["tags"].([]any); !ok || len(tags) != 1 || tags[0] != "tag-"+id {
							t.Fatal("selector adoption retained another record's tags")
						}
					}
				}
				config(first)
				run(0, "apply", "-auto-approve", "-no-color")
				assertState(first, first)
				config(strings.ToUpper(first))
				run(2, "plan", "-no-color", "-detailed-exitcode", "-out=selector.tfplan")
				if strings.Join(planActions(), ",") != "update" {
					t.Fatal("UUID casing alone unnecessarily released and readopted the same device")
				}
				run(0, "apply", "-auto-approve", "-no-color")
				run(0, "plan", "-no-color", "-detailed-exitcode")
				assertState(first, strings.ToUpper(first))
				config(second)
				run(2, "plan", "-no-color", "-detailed-exitcode", "-out=selector.tfplan")
				wanted := "delete,create"
				if createBeforeDestroy {
					wanted = "create,delete"
				}
				if strings.Join(planActions(), ",") != wanted {
					t.Fatal("a different device UUID no longer required the configured replacement ordering")
				}
				run(0, "apply", "-auto-approve", "-no-color")
				run(0, "plan", "-no-color", "-detailed-exitcode")
				assertState(second, second)
				run(0, "destroy", "-auto-approve", "-no-color")
				if strings.TrimSpace(run(0, "state", "list")) != "" {
					t.Fatal("settings release retained a mapping")
				}
				mu.Lock()
				defer mu.Unlock()
				if writes != 0 || reads[first] == 0 || reads[second] == 0 || len(objects) != 2 {
					t.Fatal("selector transition mutated a device, invoked commands or failed to observe both records")
				}
			})
		}
	}
}

func TestTerraformCLINoteSelectorCaseAndRealReplacement(t *testing.T) {
	const first = "aaaaaaaa-bbbb-4ccc-8ddd-111111111111"
	const second = "bbbbbbbb-cccc-4ddd-8eee-222222222222"
	for _, createBeforeDestroy := range []bool{false, true} {
		t.Run(fmt.Sprintf("create_before_destroy=%t", createBeforeDestroy), func(t *testing.T) {
			var mu sync.Mutex
			content := "<p>Synthetic note</p>"
			objects := map[string]map[string]any{first: {"note_id": syntheticID, "content": content}, second: nil}
			writes := []string{}
			run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				device, suffix := "", ""
				for _, id := range []string{first, second} {
					prefix := "/api/v1/devices/" + id + "/notes"
					if strings.HasPrefix(r.URL.Path, prefix) {
						device, suffix = id, strings.TrimPrefix(r.URL.Path, prefix)
					}
				}
				if device == "" || r.Header.Get("Authorization") != "Bearer "+cliToken {
					t.Error("unexpected device operation or credential")
					w.WriteHeader(405)
					return
				}
				value := objects[device]
				if r.Method != http.MethodPost && (value == nil || suffix != "/"+value["note_id"].(string)) {
					w.WriteHeader(404)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.Method {
				case http.MethodGet:
				case http.MethodPatch, http.MethodPost:
					if r.Method == http.MethodPost && (suffix != "" || value != nil || device != second) {
						t.Error("unexpected Note creation")
						w.WriteHeader(409)
						return
					}
					var body map[string]string
					if json.NewDecoder(r.Body).Decode(&body) != nil || len(body) != 1 || body["content"] == "" {
						t.Error("unexpected Note update fields")
						w.WriteHeader(400)
						return
					}
					if r.Method == http.MethodPost {
						value = map[string]any{"note_id": syntheticCategory}
						objects[device] = value
						w.WriteHeader(201)
					}
					writes = append(writes, r.Method+" "+device)
					value["content"] = body["content"]
				case http.MethodDelete:
					writes = append(writes, r.Method+" "+device)
					objects[device] = nil
					_, _ = w.Write([]byte(`{"deleted_at":"2026-11-06T00:00:00Z"}`))
					return
				default:
					t.Error("unexpected Note method")
					w.WriteHeader(405)
					return
				}
				_ = json.NewEncoder(w).Encode(value)
			}))
			config := func(id string) {
				lifecycle := ""
				if createBeforeDestroy {
					lifecycle = " lifecycle { create_before_destroy=true }\n"
				}
				write(fmt.Sprintf("resource \"iru_device_note\" \"example\" {\n device_id=%q\n content=%q\n%s}\n", id, content, lifecycle))
			}
			actions := func() string {
				var plan struct {
					Changes []struct{ Change struct{ Actions []string } } `json:"resource_changes"`
				}
				if json.Unmarshal([]byte(run(0, "show", "-json", "note-selector.tfplan")), &plan) != nil || len(plan.Changes) != 1 {
					t.Fatal("unexpected Note selector plan")
				}
				return strings.Join(plan.Changes[0].Change.Actions, ",")
			}
			assertState := func(id, noteID string) {
				var raw struct {
					Resources []struct {
						Instances []struct{ Attributes map[string]any }
					}
				}
				if json.Unmarshal([]byte(run(0, "state", "pull")), &raw) != nil || len(raw.Resources) != 1 || len(raw.Resources[0].Instances) != 1 {
					t.Fatal("Note state missing")
				}
				value := raw.Resources[0].Instances[0].Attributes
				if value["device_id"] != id || value["id"] != noteID || value["content"] != content {
					t.Fatal("Note selector transition changed identity/content unexpectedly")
				}
			}
			config(first)
			run(0, "import", "-no-color", "iru_device_note.example", first+"/"+syntheticID)
			config(strings.ToUpper(first))
			run(2, "plan", "-no-color", "-detailed-exitcode", "-out=note-selector.tfplan")
			if actions() != "update" {
				t.Fatal("UUID casing alone planned destructive Note replacement")
			}
			run(0, "apply", "-auto-approve", "-no-color")
			run(0, "plan", "-no-color", "-detailed-exitcode")
			assertState(strings.ToUpper(first), syntheticID)
			mu.Lock()
			count := len(writes)
			mu.Unlock()
			if count != 0 {
				t.Fatal("case-only Note selector change wrote to the API")
			}
			content = "<p>Synthetic changed note</p>"
			config(strings.ToUpper(first))
			run(0, "apply", "-auto-approve", "-no-color")
			assertState(strings.ToUpper(first), syntheticID)
			config(second)
			run(2, "plan", "-no-color", "-detailed-exitcode", "-out=note-selector.tfplan")
			wanted := "delete,create"
			if createBeforeDestroy {
				wanted = "create,delete"
			}
			if actions() != wanted {
				t.Fatal("a genuinely different Note device no longer requires replacement")
			}
			run(0, "apply", "-auto-approve", "-no-color")
			run(0, "plan", "-no-color", "-detailed-exitcode")
			assertState(second, syntheticCategory)
			run(0, "destroy", "-auto-approve", "-no-color")
			mu.Lock()
			defer mu.Unlock()
			middle := []string{"DELETE " + first, "POST " + second}
			if createBeforeDestroy {
				middle[0], middle[1] = middle[1], middle[0]
			}
			expected := strings.Join(append(append([]string{"PATCH " + first}, middle...), "DELETE "+second), ",")
			if strings.Join(writes, ",") != expected || objects[first] != nil || objects[second] != nil {
				t.Fatal("Note writes escaped the exact requested content/relocation/deletion scope")
			}
		})
	}
}
