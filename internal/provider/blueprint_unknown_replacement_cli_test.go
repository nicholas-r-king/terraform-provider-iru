// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

func TestTerraformCLIBlueprintUnknownReplacementInputsBlocked(t *testing.T) {
	for _, field := range []string{"type", "source_type", "source_id"} {
		for _, createBeforeDestroy := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/create_before_destroy=%t", field, createBeforeDestroy), func(t *testing.T) {
				var mu sync.Mutex
				posts, otherWrites := 0, 0
				object := client.ManagedBlueprint{ID: syntheticID, Name: "Synthetic cloned map", Type: "map", Color: "aqua-800", Icon: "ss-files"}
				code, active := "000123", false
				object.EnrollmentCode.Code, object.EnrollmentCode.Active = &code, &active
				run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					defer mu.Unlock()
					if r.Header.Get("Authorization") != "Bearer "+cliToken {
						t.Error("unexpected mock credential")
						w.WriteHeader(401)
						return
					}
					if r.Method == http.MethodPost && r.URL.Path == "/api/v1/blueprints" && posts == 0 {
						if r.ParseForm() != nil || r.Form.Get("type") != "map" || r.Form.Get("source.type") != "blueprint" || r.Form.Get("source.id") != syntheticCategory || r.Form.Get("enrollment_code.is_active") != "false" {
							t.Error("unexpected synthetic clone inputs")
							w.WriteHeader(400)
							return
						}
						posts++
						w.WriteHeader(201)
					} else if r.Method != http.MethodGet || r.URL.Path != "/api/v1/blueprints/"+syntheticID {
						otherWrites++
						w.WriteHeader(405)
						return
					}
					_ = json.NewEncoder(w).Encode(object)
				}))
				values := map[string]string{"type": "map", "source_type": "blueprint", "source_id": syntheticCategory}
				config := func(unknown bool) {
					extra := ""
					fields := map[string]string{}
					for key, value := range values {
						fields[key] = fmt.Sprintf("%q", value)
					}
					if unknown {
						extra = fmt.Sprintf("resource \"terraform_data\" \"pending\" { input=%q }\n", values[field])
						fields[field] = "tostring(terraform_data.pending.output)"
					}
					lifecycle := ""
					if createBeforeDestroy {
						lifecycle = " lifecycle { create_before_destroy=true }\n"
					}
					write(fmt.Sprintf("resource \"iru_blueprint\" \"example\" {\n name=\"Synthetic cloned map\"\n type=%s\n source_type=%s\n source_id=%s\n enrollment_active=false\n%s}\n%s", fields["type"], fields["source_type"], fields["source_id"], lifecycle, extra))
				}
				config(false)
				run(0, "apply", "-auto-approve", "-no-color")
				run(0, "plan", "-no-color", "-detailed-exitcode")
				before := run(0, "state", "pull")
				config(true)
				out := run(1, "plan", "-no-color", "-detailed-exitcode")
				if !strings.Contains(out, "Blueprint replacement blocked") {
					t.Fatal("unknown immutable input bypassed the planning guard")
				}
				if after := run(0, "state", "pull"); after != before {
					t.Fatal("failed unknown-input planning changed stored Blueprint state")
				}
				config(false)
				run(0, "plan", "-no-color", "-detailed-exitcode")
				mu.Lock()
				defer mu.Unlock()
				if posts != 1 || otherWrites != 0 {
					t.Fatal("unknown-input planning attempted a clone, update, delete or device route")
				}
			})
		}
	}
}

func TestTerraformCLIBlueprintUnknownCreateInputsDeferred(t *testing.T) {
	for _, field := range []string{"type", "source_type", "source_id"} {
		t.Run(field, func(t *testing.T) {
			var calls atomic.Int32
			run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(405)
			}))
			values := map[string]string{"type": "map", "source_type": "blueprint", "source_id": syntheticCategory}
			fields := map[string]string{}
			for key, value := range values {
				fields[key] = fmt.Sprintf("%q", value)
			}
			fields[field] = "tostring(terraform_data.pending.output)"
			write(fmt.Sprintf("resource \"iru_blueprint\" \"example\" {\n name=\"Synthetic new map\"\n type=%s\n source_type=%s\n source_id=%s\n enrollment_active=false\n}\nresource \"terraform_data\" \"pending\" { input=%q }\n", fields["type"], fields["source_type"], fields["source_id"], values[field]))
			run(2, "plan", "-no-color", "-detailed-exitcode", "-out=new-map.tfplan")
			var plan struct {
				Changes []struct {
					Address string
					Change  struct{ Actions []string }
				} `json:"resource_changes"`
			}
			if json.Unmarshal([]byte(run(0, "show", "-json", "new-map.tfplan")), &plan) != nil || len(plan.Changes) != 2 {
				t.Fatal("unexpected new-map deferral plan")
			}
			for _, change := range plan.Changes {
				if strings.Join(change.Change.Actions, ",") != "create" {
					t.Fatal("new-resource unknown input incorrectly requested replacement")
				}
			}
			if calls.Load() != 0 {
				t.Fatal("planning a new deferred map reached the API")
			}
		})
	}
}
