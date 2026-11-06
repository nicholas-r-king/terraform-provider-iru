// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
)

// Only loopback fixtures are deleted. A failed DELETE must retain its exact
// locator, never replay a mutation, and allow independent read reconciliation.
func TestTerraformCLIDeleteFailuresRetainLocatorsWithoutReplay(t *testing.T) {
	id, bp := syntheticID, syntheticCategory
	cases := []struct{ kind, config, endpoint, body, importID string }{
		{"custom_script", "name=\"Synthetic\"\nscript=\"#!/bin/sh\\nexit 0\"", "/api/v1/library/custom-scripts/" + id, `{"id":"` + id + `","name":"Synthetic","script":"#!/bin/sh\nexit 0","remediation_script":"","execution_frequency":"once","active":false,"restart":false,"show_in_self_service":false,"self_service_recommended":false}`, id},
		{"custom_profile", "name=\"Synthetic\"\nprofile=\"<plist><dict/></plist>\"\nruns_on_mac=true", "/api/v1/library/custom-profiles/" + id, `{"id":"` + id + `","mdm_identifier":"synthetic-mdm","name":"Synthetic","profile":"<plist><dict/></plist>","active":false,"runs_on_mac":true,"runs_on_iphone":false,"runs_on_ipad":false,"runs_on_tv":false,"runs_on_vision":false}`, id},
		{"custom_app", "name=\"Synthetic\"\nfile_key=\"synthetic/pkg\"\ninstall_type=\"package\"", "/api/v1/library/custom-apps/" + id, `{"id":"` + id + `","name":"Synthetic","file_key":"synthetic/pkg","install_type":"package","install_enforcement":"install_once","audit_script":"","preinstall_script":"","postinstall_script":"","unzip_location":"","active":false,"restart":false,"show_in_self_service":false,"self_service_recommended":false}`, id},
		{"in_house_app", "name=\"Synthetic\"\nfile_key=\"synthetic/ipa\"\nruns_on_iphone=true", "/api/v1/library/ipa-apps/" + id, `{"id":"` + id + `","name":"Synthetic","file_key":"synthetic/ipa","active":false,"runs_on_iphone":true,"runs_on_ipad":false,"runs_on_tv":false}`, id},
		{"device_note", "device_id=\"" + bp + "\"\ncontent=\"Synthetic\"", "/api/v1/devices/" + bp + "/notes/" + id, `{"note_id":"` + id + `","content":"Synthetic"}`, bp + "/" + id},
		{"tag", "name=\"Synthetic\"", "/api/v1/tags/" + id, `{"count":1,"results":[{"id":"` + id + `","name":"Synthetic"}],"next":null}`, id},
	}
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			var mu sync.Mutex
			exists := true
			status := 403
			drop := false
			deletes, otherWrites := 0, 0
			run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				readEndpoint := c.endpoint
				if c.kind == "tag" {
					readEndpoint = "/api/v1/tags"
				}
				if r.Method == http.MethodGet && r.URL.Path == readEndpoint {
					if !exists {
						if c.kind == "tag" {
							fmt.Fprint(w, `{"count":0,"results":[],"next":null}`)
						} else {
							w.WriteHeader(404)
						}
						return
					}
					fmt.Fprint(w, c.body)
					return
				}
				if r.Method != http.MethodDelete || r.URL.Path != c.endpoint {
					otherWrites++
					t.Error("unexpected mutation or path")
					w.WriteHeader(405)
					return
				}
				deletes++
				if drop {
					// Simulate a real deletion with a lost response: never infer success or
					// resend DELETE solely because the transport failed.
					exists = false
					h, ok := w.(http.Hijacker)
					if !ok {
						t.Error("fixture cannot drop connection")
						w.WriteHeader(500)
						return
					}
					connection, _, err := h.Hijack()
					if err != nil {
						t.Error("fixture cannot hijack connection")
						return
					}
					connection.Close()
					return
				}
				if status == 404 {
					exists = false
				}
				w.WriteHeader(status)
				fmt.Fprint(w, `{"private":"synthetic-private-delete-response"}`)
			}))
			address := "iru_" + c.kind + ".example"
			write(fmt.Sprintf("resource %q \"example\" {\n%s\n}\n", "iru_"+c.kind, c.config))
			pull := func() any {
				var state map[string]any
				if json.Unmarshal([]byte(run(0, "state", "pull")), &state) != nil {
					t.Fatal("invalid persisted state")
				}
				return canonicalDeleteState(t, state["resources"])
			}
			run(0, "import", "-no-color", address, c.importID)
			before := pull()
			for _, failure := range []int{401, 403, 429, 503} {
				mu.Lock()
				status = failure
				count := deletes
				mu.Unlock()
				output := run(1, "destroy", "-auto-approve", "-no-color")
				if strings.Contains(output, "synthetic-private-delete-response") {
					t.Fatal("private response leaked")
				}
				if after := pull(); !reflect.DeepEqual(after, before) {
					t.Logf("Changed state paths (values withheld): %v", deleteStateDifferencePaths(before, after, "resources"))
					t.Fatalf("DELETE HTTP%d changed resource locator or attributes", failure)
				}
				mu.Lock()
				if deletes != count+1 || !exists || otherWrites != 0 {
					t.Error("failed DELETE replayed, changed fixture or performed another mutation")
				}
				mu.Unlock()
			}
			mu.Lock()
			drop = true
			count := deletes
			mu.Unlock()
			run(1, "destroy", "-auto-approve", "-no-color")
			if !reflect.DeepEqual(pull(), before) {
				t.Fatal("ambiguous DELETE discarded its locator")
			}
			mu.Lock()
			if deletes != count+1 || exists {
				t.Error("ambiguous deletion did not occur exactly once")
			}
			mu.Unlock()
			run(0, "apply", "-refresh-only", "-auto-approve", "-no-color")
			if strings.TrimSpace(run(0, "state", "list")) != "" {
				t.Fatal("independently confirmed absence retained resource state")
			}
			mu.Lock()
			if deletes != count+1 {
				t.Error("read reconciliation replayed DELETE")
			}
			drop = false
			exists = true
			status = 404
			count = deletes
			mu.Unlock()
			run(0, "import", "-no-color", address, c.importID)
			run(0, "destroy", "-auto-approve", "-no-color")
			if strings.TrimSpace(run(0, "state", "list")) != "" {
				t.Fatal("exact DELETE404 did not complete idempotently")
			}
			mu.Lock()
			defer mu.Unlock()
			if deletes != count+1 || exists || otherWrites != 0 {
				t.Fatal("DELETE404 was replayed or changed unrelated objects")
			}
		})
	}
}

// Diagnostics expose state field paths only, never attribute values.
func deleteStateDifferencePaths(left, right any, prefix string) []string {
	if reflect.DeepEqual(left, right) {
		return nil
	}
	switch a := left.(type) {
	case map[string]any:
		b, ok := right.(map[string]any)
		if !ok {
			return []string{prefix}
		}
		keys := map[string]bool{}
		for k := range a {
			keys[k] = true
		}
		for k := range b {
			keys[k] = true
		}
		result := []string{}
		for k := range keys {
			result = append(result, deleteStateDifferencePaths(a[k], b[k], prefix+"."+k)...)
		}
		sort.Strings(result)
		return result
	case []any:
		b, ok := right.([]any)
		if !ok || len(a) != len(b) {
			return []string{prefix}
		}
		result := []string{}
		for i := range a {
			result = append(result, deleteStateDifferencePaths(a[i], b[i], fmt.Sprintf("%s[%d]", prefix, i))...)
		}
		return result
	}
	return []string{prefix}
}

// Terraform 1.11 can serialize this set of sensitivity paths in a different
// order after saving otherwise identical state. Canonicalize only that outer
// path list; never sort attribute values, nested path steps or resource arrays.
func canonicalDeleteState(t *testing.T, value any) any {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal("invalid comparison fixture")
	}
	var copied any
	if json.Unmarshal(encoded, &copied) != nil {
		t.Fatal("invalid comparison fixture")
	}
	resources, ok := copied.([]any)
	if !ok {
		return copied
	}
	for _, raw := range resources {
		resource, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		instances, _ := resource["instances"].([]any)
		for _, rawInstance := range instances {
			instance, ok := rawInstance.(map[string]any)
			if !ok {
				continue
			}
			paths, ok := instance["sensitive_attributes"].([]any)
			if !ok {
				continue
			}
			sort.SliceStable(paths, func(i, j int) bool {
				left, _ := json.Marshal(paths[i])
				right, _ := json.Marshal(paths[j])
				return string(left) < string(right)
			})
		}
	}
	return copied
}

func TestDeleteStateComparisonCanonicalizesOnlySensitivePathOrder(t *testing.T) {
	fixture := func(paths []any, id, script string) any {
		return []any{map[string]any{"type": "iru_custom_script", "name": "example", "instances": []any{map[string]any{"attributes": map[string]any{"id": id, "script": script, "ordered_values": []string{"first", "second"}}, "sensitive_attributes": paths}}}}
	}
	scriptPath := []any{map[string]any{"type": "get_attr", "value": "script"}}
	remediationPath := []any{map[string]any{"type": "get_attr", "value": "remediation_script"}}
	original := fixture([]any{scriptPath, remediationPath}, syntheticID, "synthetic-private-script")
	before := canonicalDeleteState(t, original)
	reordered := fixture([]any{remediationPath, scriptPath}, syntheticID, "synthetic-private-script")
	if !reflect.DeepEqual(before, canonicalDeleteState(t, reordered)) {
		t.Fatal("sensitivity-path permutation changed semantic state")
	}
	for _, changed := range []any{
		fixture([]any{scriptPath}, syntheticID, "synthetic-private-script"),
		fixture([]any{scriptPath, scriptPath}, syntheticID, "synthetic-private-script"),
		fixture([]any{scriptPath, remediationPath}, syntheticCategory, "synthetic-private-script"),
		fixture([]any{scriptPath, remediationPath}, syntheticID, "changed-private-script"),
	} {
		if reflect.DeepEqual(before, canonicalDeleteState(t, changed)) {
			t.Fatal("canonical comparison hid a meaningful state change")
		}
	}
	ordered := canonicalDeleteState(t, original).([]any)
	attrs := ordered[0].(map[string]any)["instances"].([]any)[0].(map[string]any)["attributes"].(map[string]any)
	attrs["ordered_values"] = []any{"second", "first"}
	if reflect.DeepEqual(before, canonicalDeleteState(t, ordered)) {
		t.Fatal("canonical comparison sorted an attribute value")
	}
}
