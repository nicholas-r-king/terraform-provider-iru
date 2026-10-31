// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// Exercise Terraform's persisted state, not just client error classification.
// Every request is GET-only against a loopback fixture; no live API is used.
func TestTerraformCLIRefreshFailuresPreserveImportedState(t *testing.T) {
	id, bp := syntheticID, syntheticCategory
	cases := []struct{ kind, config, endpoint, body, importID string }{
		{"custom_script", "name=\"Synthetic\"\nscript=\"#!/bin/sh\\nexit 0\"", "/api/v1/library/custom-scripts/" + id, `{"id":"` + id + `","name":"Synthetic","script":"#!/bin/sh\nexit 0","remediation_script":"","execution_frequency":"once","active":false,"restart":false,"show_in_self_service":false,"self_service_recommended":false}`, id},
		{"custom_profile", "name=\"Synthetic\"\nprofile=\"<plist><dict/></plist>\"\nruns_on_mac=true", "/api/v1/library/custom-profiles/" + id, `{"id":"` + id + `","mdm_identifier":"synthetic-mdm","name":"Synthetic","profile":"<plist><dict/></plist>","active":false,"runs_on_mac":true,"runs_on_iphone":false,"runs_on_ipad":false,"runs_on_tv":false,"runs_on_vision":false}`, id},
		{"custom_app", "name=\"Synthetic\"\nfile_key=\"synthetic/pkg\"\ninstall_type=\"package\"", "/api/v1/library/custom-apps/" + id, `{"id":"` + id + `","name":"Synthetic","file_key":"synthetic/pkg","install_type":"package","install_enforcement":"install_once","audit_script":"","preinstall_script":"","postinstall_script":"","unzip_location":"","active":false,"restart":false,"show_in_self_service":false,"self_service_recommended":false}`, id},
		{"in_house_app", "name=\"Synthetic\"\nfile_key=\"synthetic/ipa\"\nruns_on_iphone=true", "/api/v1/library/ipa-apps/" + id, `{"id":"` + id + `","name":"Synthetic","file_key":"synthetic/ipa","active":false,"runs_on_iphone":true,"runs_on_ipad":false,"runs_on_tv":false}`, id},
		{"blueprint", "name=\"Synthetic\"", "/api/v1/blueprints/" + id, `{"id":"` + id + `","name":"Synthetic","type":"classic","description":"","color":"aqua-800","icon":"ss-files","enrollment_code":{"code":"123456","is_active":false}}`, id},
		{"ade_integration", "blueprint_id=\"" + bp + "\"\nemail=\"synthetic@example.invalid\"\nphone=\"1234567890\"", "/api/v1/integrations/apple/ade/" + id, `{"id":"` + id + `","blueprint_id":"` + bp + `","use_blueprint_routing":false,"defaults":{"email":"synthetic@example.invalid","phone":"1234567890"},"server_name":"Synthetic","access_token_expiry":"2027-10-30T00:00:00Z"}`, id},
		{"device_settings", "device_id=\"" + id + "\"", "/api/v1/devices/" + id, `{"device_id":"` + id + `","blueprint_id":"` + bp + `","user":null,"asset_tag":null,"tags":[]}`, id},
		{"ade_device_settings", "device_id=\"" + id + "\"", "/api/v1/integrations/apple/ade/devices/" + id, `{"id":"` + id + `","blueprint_id":"` + bp + `","user_id":null,"asset_tag":null,"use_blueprint_routing":false,"override_dep_profile_id":null}`, id},
		{"device_note", "device_id=\"" + bp + "\"\ncontent=\"Synthetic\"", "/api/v1/devices/" + bp + "/notes/" + id, `{"note_id":"` + id + `","content":"Synthetic"}`, bp + "/" + id},
		{"tag", "name=\"Synthetic\"", "/api/v1/tags", `{"results":[{"id":"` + id + `","name":"Synthetic"}],"next":null}`, id},
		{"blueprint_routing", "enrollment_code=\"123456\"\nenrollment_active=false", "/api/v1/blueprint-routing/", `{"enrollment_code":{"code":"123456","is_active":false}}`, "tenant"},
	}
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			var mu sync.Mutex
			status, malformed, absent, writes := 200, false, false, 0
			run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if r.Method != http.MethodGet {
					writes++
					w.WriteHeader(405)
					return
				}
				if r.URL.Path != c.endpoint {
					t.Errorf("unexpected fixture path %s", r.URL.Path)
					w.WriteHeader(400)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if status != 200 {
					w.WriteHeader(status)
					return
				}
				if malformed {
					_, _ = w.Write([]byte(`{"private":"synthetic-private-response"}`))
					return
				}
				if absent {
					_, _ = w.Write([]byte(`{"results":[],"next":null}`))
					return
				}
				_, _ = w.Write([]byte(c.body))
			}))
			address := "iru_" + c.kind + ".example"
			write(fmt.Sprintf("resource %q \"example\" {\n%s\n}\n", "iru_"+c.kind, c.config))
			run(0, "import", "-no-color", address, c.importID)
			before := run(0, "state", "pull")
			for _, code := range []int{401, 403, 200} {
				mu.Lock()
				status, malformed = code, code == 200
				mu.Unlock()
				out := run(1, "plan", "-no-color")
				if strings.Contains(out, "synthetic-private-response") {
					t.Fatal("error response leaked")
				}
				if run(0, "state", "pull") != before {
					t.Fatal("failed refresh changed persisted imported state")
				}
			}
			mu.Lock()
			status, malformed = 404, false
			mu.Unlock()
			if c.kind == "tag" || c.kind == "blueprint_routing" {
				run(1, "apply", "-refresh-only", "-auto-approve", "-no-color")
				if run(0, "state", "pull") != before {
					t.Fatal("collection/singleton 404 erased imported state")
				}
				if c.kind == "tag" {
					mu.Lock()
					status, absent = 200, true
					mu.Unlock()
					run(0, "apply", "-refresh-only", "-auto-approve", "-no-color")
					if strings.TrimSpace(run(0, "state", "list")) != "" {
						t.Fatal("complete absent Tag listing retained state")
					}
				}
			} else {
				run(0, "apply", "-refresh-only", "-auto-approve", "-no-color")
				if strings.TrimSpace(run(0, "state", "list")) != "" {
					t.Fatal("exact-record 404 retained absent object")
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if writes != 0 {
				t.Fatalf("import/refresh sent %d writes", writes)
			}
		})
	}
}
