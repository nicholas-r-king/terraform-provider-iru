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

// Extends package-only resource coverage. The keys below model pre-uploaded
// fixtures; this test neither uploads nor claims ZIP/DMG contents are installable.
func TestTerraformCLIAppZipAndImageReferenceLifecycle(t *testing.T) {
	for _, kind := range []string{"zip", "image"} {
		t.Run(kind, func(t *testing.T) {
			var mu sync.Mutex
			var object *client.App
			posts, patches, deletes := 0, 0, 0
			fault := ""
			invalidZIPRead := false
			key := "synthetic/reference/fixture." + map[string]string{"zip": "zip", "image": "dmg"}[kind]
			name, location := "Synthetic "+kind, ""
			if kind == "zip" {
				location = "/var/tmp/IruProviderValidation"
			}
			run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if r.Header.Get("Authorization") != "Bearer "+cliToken || (r.URL.Path != "/api/v1/library/custom-apps" && r.URL.Path != "/api/v1/library/custom-apps/"+syntheticID) {
					t.Error("unexpected route, upload, device operation or credential")
					w.WriteHeader(405)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.Method {
				case "GET":
					if object == nil {
						w.WriteHeader(404)
						return
					}
				case "POST", "PATCH":
					if r.ParseForm() != nil || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
						t.Error("invalid form")
						w.WriteHeader(400)
						return
					}
					if r.Method == "POST" {
						posts++
						if object != nil || r.Form.Get("file_key") != key || r.Form.Get("install_type") != kind || r.Form.Get("active") != "false" || r.Form.Get("show_in_self_service") != "false" || r.Form.Get("restart") != "false" || r.Form.Get("install_enforcement") != "install_once" {
							t.Error("reference creation changed kind, key, safety flags or duplicated object")
						}
						if (kind == "zip" && r.Form.Get("unzip_location") != location) || (kind == "image" && r.Form.Has("unzip_location")) {
							t.Error("installer-specific extraction field was wrong")
						}
						object = &client.App{ID: syntheticID, AppInput: client.AppInput{Name: r.Form.Get("name"), FileKey: key, InstallType: kind, InstallEnforcement: "install_once", UnzipLocation: r.Form.Get("unzip_location")}}
						w.WriteHeader(201)
					} else {
						patches++
						if object == nil {
							w.WriteHeader(404)
							return
						}
						// Metadata edits must not resend an adopted key, upload data,
						// alter type/enforcement or accidentally enable delivery.
						for field := range r.Form {
							if field != "name" && !(kind == "zip" && field == "unzip_location") {
								t.Error("metadata PATCH resent unrelated installer fields")
							}
						}
						if fault == "rejected" {
							w.WriteHeader(503)
							fmt.Fprint(w, `{"private":"synthetic-private-failure"}`)
							return
						}
						if r.Form.Has("name") {
							object.Name = r.Form.Get("name")
						}
						if r.Form.Has("unzip_location") {
							object.UnzipLocation = r.Form.Get("unzip_location")
						}
						if fault == "committed" {
							w.WriteHeader(503)
							fmt.Fprint(w, `{"private":"synthetic-private-failure"}`)
							return
						}
					}
				case "DELETE":
					deletes++
					object = nil
					w.WriteHeader(204)
					return
				default:
					t.Error("unexpected method")
					w.WriteHeader(405)
					return
				}
				returned := *object
				if r.Method == "GET" && invalidZIPRead {
					returned.UnzipLocation = ""
				}
				_ = json.NewEncoder(w).Encode(returned)
			}))
			configuredKey := true
			config := func() {
				source := ""
				if configuredKey {
					source = fmt.Sprintf("file_key=%q\n", key)
				}
				write(fmt.Sprintf("resource \"iru_custom_app\" \"example\" {\n name=%q\n %s install_type=%q\n unzip_location=%q\n active=false\n restart=false\n show_in_self_service=false\n}\n", name, source, kind, location))
			}
			state := func() map[string]any {
				var value struct {
					Resources []struct {
						Instances []struct{ Attributes map[string]any }
					}
				}
				if json.Unmarshal([]byte(run(0, "state", "pull")), &value) != nil || len(value.Resources) != 1 || len(value.Resources[0].Instances) != 1 {
					t.Fatal("resource locator missing")
				}
				return value.Resources[0].Instances[0].Attributes
			}
			assertState := func(wantName, wantLocation string) {
				value := state()
				if value["id"] != syntheticID || value["file_key"] != key || value["install_type"] != kind || value["name"] != wantName || value["unzip_location"] != wantLocation || value["active"] != false || value["show_in_self_service"] != false {
					t.Fatal("installer metadata/key/safety state changed unexpectedly")
				}
			}
			config()
			run(0, "apply", "-auto-approve", "-no-color")
			run(0, "plan", "-no-color", "-detailed-exitcode")
			assertState(name, location)
			if kind == "zip" {
				mu.Lock()
				invalidZIPRead = true
				before := patches
				mu.Unlock()
				run(1, "plan", "-no-color")
				assertState(name, location)
				mu.Lock()
				invalidZIPRead = false
				if patches != before {
					t.Error("invalid ZIP read changed the remote record")
				}
				mu.Unlock()
			}
			// Fresh import may adopt the uploaded key rather than require the user
			// to reconstruct it in configuration. Import must not create/upload.
			run(0, "state", "rm", "iru_custom_app.example")
			configuredKey = false
			config()
			run(0, "import", "-no-color", "iru_custom_app.example", strings.ToUpper(syntheticID))
			run(0, "plan", "-no-color", "-detailed-exitcode")
			assertState(name, location)
			change := func(suffix string) {
				if kind == "zip" {
					location = "/Library/Application Support/IruProviderValidation/" + suffix + " λ"
				} else {
					name = "Synthetic image " + suffix
				}
				config()
			}
			change("changed")
			run(2, "plan", "-no-color", "-detailed-exitcode")
			run(0, "apply", "-auto-approve", "-no-color")
			run(0, "plan", "-no-color", "-detailed-exitcode")
			assertState(name, location)
			mu.Lock()
			if kind == "zip" {
				object.UnzipLocation = "/var/tmp/SyntheticExternalDrift"
			} else {
				object.Name = "Synthetic external image drift"
			}
			mu.Unlock()
			run(2, "plan", "-no-color", "-detailed-exitcode")
			run(0, "apply", "-auto-approve", "-no-color")
			run(0, "plan", "-no-color", "-detailed-exitcode")
			assertState(name, location)
			for _, failure := range []string{"rejected", "committed"} {
				oldName, oldLocation := name, location
				change(failure)
				mu.Lock()
				fault = failure
				previous := patches
				mu.Unlock()
				out := run(1, "apply", "-auto-approve", "-no-color")
				if strings.Contains(out, "synthetic-private-failure") {
					t.Fatal("raw service body leaked")
				}
				assertState(oldName, oldLocation)
				mu.Lock()
				if patches != previous+1 {
					t.Error("failed metadata PATCH replayed")
				}
				fault = ""
				mu.Unlock()
				if failure == "rejected" {
					run(2, "plan", "-no-color", "-detailed-exitcode")
					// Explicit apply only after GET/plan showed unchanged remote
					// state. The client did not replay the failed request itself.
					run(0, "apply", "-auto-approve", "-no-color")
				} else {
					run(0, "plan", "-no-color", "-detailed-exitcode")
					run(0, "apply", "-refresh-only", "-auto-approve", "-no-color")
					mu.Lock()
					if patches != previous+1 {
						t.Error("read-only reconciliation repeated a committed update")
					}
					mu.Unlock()
				}
				assertState(name, location)
				run(0, "plan", "-no-color", "-detailed-exitcode")
			}
			if kind == "zip" {
				preserved := location
				location = ""
				config()
				mu.Lock()
				before := patches
				mu.Unlock()
				run(1, "plan", "-no-color")
				mu.Lock()
				if patches != before {
					t.Error("invalid ZIP destination clear reached transport")
				}
				mu.Unlock()
				location = preserved
				config()
			}
			run(0, "destroy", "-auto-approve", "-no-color")
			if strings.TrimSpace(run(0, "state", "list")) != "" {
				t.Fatal("destroy left a managed resource")
			}
			run(1, "import", "-no-color", "iru_custom_app.example", syntheticID)
			if strings.TrimSpace(run(0, "state", "list")) != "" {
				t.Fatal("failed missing-object import invented managed state")
			}
			mu.Lock()
			defer mu.Unlock()
			if posts != 1 || deletes != 1 || object != nil {
				t.Fatal("reference lifecycle created duplicates or retained deleted record")
			}
		})
	}
}
