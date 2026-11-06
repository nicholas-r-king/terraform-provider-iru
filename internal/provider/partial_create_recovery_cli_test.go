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

// A successful HTTP status can still describe an incompletely observed create.
// Check actual Core persistence/reconciliation, not just CreateResponse.State.
// All writes, objects and credentials here belong to a disposable mock service.
type partialCreateFixture struct{ kind, config, endpoint, body, identity, invalid string }

func partialCreateFixtures() []partialCreateFixture {
	return []partialCreateFixture{
		{"custom_script", "name=\"Synthetic\"\nscript=\"#!/bin/sh\\nexit 0\"", "/api/v1/library/custom-scripts", `{"id":"` + syntheticID + `","name":"Synthetic","script":"#!/bin/sh\nexit 0","remediation_script":"","execution_frequency":"once","active":false,"restart":false,"show_in_self_service":false,"self_service_recommended":false}`, "id", "name"},
		{"custom_profile", "name=\"Synthetic\"\nprofile=\"<plist><dict/></plist>\"\nruns_on_mac=true", "/api/v1/library/custom-profiles", `{"id":"` + syntheticID + `","mdm_identifier":"synthetic-mdm","name":"Synthetic","profile":"<plist><dict/></plist>","active":false,"runs_on_mac":true,"runs_on_iphone":false,"runs_on_ipad":false,"runs_on_tv":false,"runs_on_vision":false}`, "id", "name"},
		{"custom_app", "name=\"Synthetic\"\nfile_key=\"synthetic/pkg\"\ninstall_type=\"package\"", "/api/v1/library/custom-apps", `{"id":"` + syntheticID + `","name":"Synthetic","file_key":"synthetic/pkg","install_type":"package","install_enforcement":"install_once","audit_script":"","preinstall_script":"","postinstall_script":"","unzip_location":"","active":false,"restart":false,"show_in_self_service":false,"self_service_recommended":false}`, "id", "name"},
		{"in_house_app", "name=\"Synthetic\"\nfile_key=\"synthetic/ipa\"\nruns_on_iphone=true", "/api/v1/library/ipa-apps", `{"id":"` + syntheticID + `","name":"Synthetic","file_key":"synthetic/ipa","active":false,"runs_on_iphone":true,"runs_on_ipad":false,"runs_on_tv":false}`, "id", "name"},
		{"tag", "name=\"Synthetic\"", "/api/v1/tags", `{"id":"` + syntheticID + `","name":"Synthetic"}`, "id", "name"},
		{"device_note", "device_id=\"" + syntheticNoteDevice + "\"\ncontent=\"Synthetic\"", "/api/v1/devices/" + syntheticNoteDevice + "/notes", `{"note_id":"` + syntheticID + `","content":"Synthetic"}`, "note_id", "content"},
	}
}

func TestTerraformCLIPartialCreateRetainsRecoverableIdentity(t *testing.T) {
	for _, c := range partialCreateFixtures() {
		for _, fault := range []string{"missing-fields", "invalid-field-type"} {
			t.Run(c.kind+"/"+fault, func(t *testing.T) {
				var mu sync.Mutex
				posts, otherWrites, reads := 0, 0, 0
				created := false
				run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					defer mu.Unlock()
					w.Header().Set("Content-Type", "application/json")
					if r.Header.Get("Authorization") != "Bearer "+cliToken {
						t.Error("unexpected synthetic credential")
						w.WriteHeader(401)
						return
					}
					if r.Method == http.MethodPost && r.URL.Path == c.endpoint {
						posts++
						if created {
							t.Error("ambiguous creation was replayed")
							w.WriteHeader(409)
							return
						}
						created = true // Mock commit occurs before the malformed response.
						partial := map[string]any{c.identity: syntheticID, "private_response": "synthetic-private-response"}
						if fault == "invalid-field-type" {
							partial[c.invalid] = map[string]string{"private": "synthetic-private-response"}
						}
						w.WriteHeader(201)
						_ = json.NewEncoder(w).Encode(partial)
						return
					}
					if r.Method != http.MethodGet {
						otherWrites++
						w.WriteHeader(405)
						return
					}
					reads++
					path := c.endpoint + "/" + syntheticID
					if c.kind == "tag" {
						path = c.endpoint // Tags require a complete listing.
					}
					if !created || r.URL.Path != path {
						t.Error("read did not target the captured synthetic object")
						w.WriteHeader(404)
						return
					}
					if c.kind == "tag" {
						_, _ = fmt.Fprintf(w, `{"count":1,"results":[%s],"next":null}`, c.body)
					} else {
						_, _ = w.Write([]byte(c.body))
					}
				}))
				write(fmt.Sprintf("resource %q \"example\" {\n%s\n}\n", "iru_"+c.kind, c.config))
				out := run(1, "apply", "-auto-approve", "-no-color")
				if !strings.Contains(out, "Unable to create Iru") || strings.Contains(out, "synthetic-private-response") || strings.Contains(out, "unknown value") {
					t.Fatal("partial creation lacked a sanitized error or exposed invalid unknown state")
				}
				state := func() map[string]any {
					t.Helper()
					var parsed struct {
						Resources []struct {
							Type      string
							Instances []struct {
								Status     string
								Attributes map[string]any
							}
						}
					}
					if json.Unmarshal([]byte(run(0, "state", "pull")), &parsed) != nil || len(parsed.Resources) != 1 || len(parsed.Resources[0].Instances) != 1 {
						t.Fatal("Core did not persist exactly one recoverable resource")
					}
					r := parsed.Resources[0]
					i := r.Instances[0]
					if r.Type != "iru_"+c.kind || i.Status != "tainted" || i.Attributes["id"] != syntheticID {
						t.Fatal("failed create lost its identity/tainted status")
					}
					if c.kind == "device_note" && i.Attributes["device_id"] != syntheticNoteDevice {
						t.Fatal("failed Note creation lost its device selector")
					}
					return i.Attributes
				}
				initial := state()
				if c.kind == "custom_profile" && initial["mdm_identifier"] != nil {
					t.Fatal("partial profile invented unobserved MDM identity")
				}
				if c.kind == "custom_app" && (initial["file_size"] != nil || initial["sha256"] != nil) {
					t.Fatal("partial App invented unobserved installer metadata")
				}
				// A read-only refresh observes the committed mock object, but must
				// not silently untaint it or claim that the failed creation passed.
				run(0, "apply", "-refresh-only", "-auto-approve", "-no-color")
				after := state()
				if c.kind == "custom_profile" && after["mdm_identifier"] != "synthetic-mdm" {
					t.Fatal("independent refresh failed to observe the profile's identity")
				}
				if !strings.Contains(run(2, "plan", "-no-color", "-detailed-exitcode"), "tainted") {
					t.Fatal("reconciliation silently certified or replaced a failed create")
				}
				mu.Lock()
				defer mu.Unlock()
				if posts != 1 || otherWrites != 0 || reads == 0 || !created {
					t.Fatal("recovery replayed creation, mutated/deleted the object, or skipped observation")
				}
			})
		}
	}
}

// When the response cannot establish an identity, an empty Terraform state is
// not proof of remote absence. Do not guess a UUID, search by name, or replay a
// write. Deliberately leave the resulting new-create plan unapplied.
func TestTerraformCLIAmbiguousCreateWithoutIdentityDoesNotInventRecovery(t *testing.T) {
	for _, c := range partialCreateFixtures() {
		for _, fault := range []string{"missing-id", "invalid-id", "truncated-json", "error-status-with-id"} {
			t.Run(c.kind+"/"+fault, func(t *testing.T) {
				var mu sync.Mutex
				posts, unexpected := 0, 0
				created := false
				run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					defer mu.Unlock()
					if r.Method != http.MethodPost || r.URL.Path != c.endpoint || r.Header.Get("Authorization") != "Bearer "+cliToken {
						unexpected++
						w.WriteHeader(405)
						return
					}
					posts++
					if created {
						t.Error("unknown-identity creation was replayed")
						w.WriteHeader(409)
						return
					}
					created = true // Simulate a committed write even on the HTTP503.
					w.Header().Set("Content-Type", "application/json")
					status := 201
					if fault == "error-status-with-id" {
						status = 503
					}
					w.WriteHeader(status)
					if fault == "truncated-json" {
						_, _ = fmt.Fprintf(w, `{"%s":%q,"private_response":"synthetic-private-response"`, c.identity, syntheticID)
						return
					}
					body := map[string]any{"private_response": "synthetic-private-response"}
					if fault == "invalid-id" {
						body[c.identity] = "synthetic-private-response"
					} else if fault == "error-status-with-id" {
						body[c.identity] = syntheticID // Error bodies are not identity evidence.
					}
					_ = json.NewEncoder(w).Encode(body)
				}))
				write(fmt.Sprintf("resource %q \"example\" {\n%s\n}\n", "iru_"+c.kind, c.config))
				out := run(1, "apply", "-auto-approve", "-no-color")
				if !strings.Contains(out, "Unable to create Iru") || !strings.Contains(strings.ToLower(out), "inspect/import") || strings.Contains(out, "synthetic-private-response") {
					t.Fatal("ambiguous create lacked safe reconciliation guidance or exposed response data")
				}
				var state struct {
					Values struct {
						RootModule struct{ Resources []json.RawMessage } `json:"root_module"`
					}
				}
				if json.Unmarshal([]byte(run(0, "show", "-json")), &state) != nil || len(state.Values.RootModule.Resources) != 0 {
					t.Fatal("provider invented a recoverable resource without a trusted UUID")
				}
				run(2, "plan", "-no-color", "-detailed-exitcode", "-out=unapplied.plan")
				var plan struct {
					ResourceChanges []struct {
						Type   string
						Change struct{ Actions []string }
					} `json:"resource_changes"`
				}
				if json.Unmarshal([]byte(run(0, "show", "-json", "unapplied.plan")), &plan) != nil || len(plan.ResourceChanges) != 1 {
					t.Fatal("unexpected next plan after an identity-less create failure")
				}
				change := plan.ResourceChanges[0]
				if change.Type != "iru_"+c.kind || len(change.Change.Actions) != 1 || change.Change.Actions[0] != "create" {
					t.Fatal("empty state did not expose its unapplied duplicate-create risk")
				}
				mu.Lock()
				defer mu.Unlock()
				if posts != 1 || unexpected != 0 || !created {
					t.Fatal("ambiguous creation replayed, discovered an object by guess, or mutated again")
				}
			})
		}
	}
}
