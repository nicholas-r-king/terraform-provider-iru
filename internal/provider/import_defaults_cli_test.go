// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

// Import must preserve non-default remote values. Omitted configuration defaults
// must remain visible as proposed changes, rather than silently adopting them or
// mutating the remote during import/plan. Matching explicit config is a no-op.
func TestTerraformCLIImportDefaultsRemainReviewable(t *testing.T) {
	cases := []struct {
		name, kind, endpoint, matching, omitted string
		remote                                  any
		before, after                           map[string]any
	}{
		{
			name: "script", kind: "iru_custom_script", endpoint: "/api/v1/library/custom-scripts/",
			remote:   client.Script{ID: syntheticID, ScriptInput: client.ScriptInput{Name: "Synthetic import", Script: "#!/bin/sh\nexit 0", ExecutionFrequency: "every_day", Active: true, Restart: true}},
			matching: "name=\"Synthetic import\"\nscript=\"#!/bin/sh\\nexit 0\"\nactive=true\nrestart=true\nexecution_frequency=\"every_day\"",
			omitted:  "name=\"Synthetic import\"\nscript=\"#!/bin/sh\\nexit 0\"",
			before:   map[string]any{"active": true, "restart": true, "execution_frequency": "every_day"},
			after:    map[string]any{"active": false, "restart": false, "execution_frequency": "once"},
		},
		{
			name: "profile", kind: "iru_custom_profile", endpoint: "/api/v1/library/custom-profiles/",
			remote:   client.Profile{ID: syntheticID, MDMIdentifier: "com.example.synthetic", ProfileInput: client.ProfileInput{Name: "Synthetic import", Profile: syntheticProfileXML, Active: true, RunsOnMac: true, RunsOnIphone: true}},
			matching: fmt.Sprintf("name=\"Synthetic import\"\nprofile=%q\nactive=true\nruns_on_mac=true\nruns_on_iphone=true", syntheticProfileXML),
			omitted:  fmt.Sprintf("name=\"Synthetic import\"\nprofile=%q\nruns_on_mac=true", syntheticProfileXML),
			before:   map[string]any{"active": true, "runs_on_iphone": true},
			after:    map[string]any{"active": false, "runs_on_iphone": false},
		},
		{
			name: "app", kind: "iru_custom_app", endpoint: "/api/v1/library/custom-apps/",
			remote:   client.App{ID: syntheticID, AppInput: client.AppInput{Name: "Synthetic import", FileKey: "companies/synthetic/library/custom_apps/example.pkg", InstallType: "package", InstallEnforcement: "continuously_enforce", AuditScript: "exit 0", PreinstallScript: "exit 0", PostinstallScript: "exit 0", Active: true, Restart: true}},
			matching: "name=\"Synthetic import\"\nfile_key=\"companies/synthetic/library/custom_apps/example.pkg\"\ninstall_type=\"package\"\ninstall_enforcement=\"continuously_enforce\"\naudit_script=\"exit 0\"\npreinstall_script=\"exit 0\"\npostinstall_script=\"exit 0\"\nactive=true\nrestart=true",
			omitted:  "name=\"Synthetic import\"\nfile_key=\"companies/synthetic/library/custom_apps/example.pkg\"\ninstall_type=\"package\"",
			before:   map[string]any{"active": true, "restart": true, "install_enforcement": "continuously_enforce", "audit_script": "exit 0", "preinstall_script": "exit 0", "postinstall_script": "exit 0"},
			after:    map[string]any{"active": false, "restart": false, "install_enforcement": "install_once", "audit_script": "", "preinstall_script": "", "postinstall_script": ""},
		},
		{
			name: "in-house", kind: "iru_in_house_app", endpoint: "/api/v1/library/ipa-apps/",
			remote:   client.InHouseApp{ID: syntheticID, InHouseAppInput: client.InHouseAppInput{Name: "Synthetic import", FileKey: "synthetic/import.ipa", Active: true, RunsOnIphone: true, RunsOnIpad: true, RunsOnTV: true}},
			matching: "name=\"Synthetic import\"\nfile_key=\"synthetic/import.ipa\"\nactive=true\nruns_on_iphone=true\nruns_on_ipad=true\nruns_on_tv=true",
			omitted:  "name=\"Synthetic import\"\nfile_key=\"synthetic/import.ipa\"\nruns_on_iphone=true",
			before:   map[string]any{"active": true, "runs_on_ipad": true, "runs_on_tv": true}, after: map[string]any{"active": false, "runs_on_ipad": false, "runs_on_tv": false},
		},
		{
			name: "blueprint", kind: "iru_blueprint", endpoint: "/api/v1/blueprints/",
			remote: func() client.ManagedBlueprint {
				v := client.ManagedBlueprint{ID: syntheticID, Name: "Synthetic import", Type: "map", Description: "Synthetic imported metadata"}
				code, active := "123456", true
				v.EnrollmentCode.Code = &code
				v.EnrollmentCode.Active = &active
				return v
			}(),
			matching: "name=\"Synthetic import\"\ndescription=\"Synthetic imported metadata\"\nenrollment_active=true",
			omitted:  "name=\"Synthetic import\"",
			before:   map[string]any{"description": "Synthetic imported metadata", "enrollment_active": true}, after: map[string]any{"description": "", "enrollment_active": false},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var writes atomic.Int32
			run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Method != http.MethodGet {
					writes.Add(1)
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				if req.URL.Path != tc.endpoint+syntheticID {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(tc.remote)
			}))
			config := func(body string) { write(fmt.Sprintf("resource %q \"example\" {\n%s\n}\n", tc.kind, body)) }
			config(tc.matching)
			run(0, "import", "-no-color", tc.kind+".example", syntheticID)
			run(0, "plan", "-no-color", "-detailed-exitcode")
			config(tc.omitted)
			run(2, "plan", "-no-color", "-detailed-exitcode", "-out=import.tfplan")
			var plan struct {
				ResourceChanges []struct {
					Address string
					Change  struct {
						Actions       []string
						Before, After map[string]any
					}
				} `json:"resource_changes"`
			}
			if json.Unmarshal([]byte(run(0, "show", "-json", "import.tfplan")), &plan) != nil || len(plan.ResourceChanges) != 1 {
				t.Fatal("expected one reviewable imported-resource change")
			}
			change := plan.ResourceChanges[0]
			if change.Address != tc.kind+".example" || !reflect.DeepEqual(change.Change.Actions, []string{"update"}) {
				t.Fatal("import defaults unexpectedly replaced or hid the resource change")
			}
			for key, want := range tc.before {
				if !reflect.DeepEqual(change.Change.Before[key], want) || !reflect.DeepEqual(change.Change.After[key], tc.after[key]) {
					t.Errorf("non-default import change hidden or misrepresented for %s", key)
				}
			}
			config(tc.matching)
			run(0, "plan", "-no-color", "-detailed-exitcode")
			if writes.Load() != 0 {
				t.Fatal("import or plans attempted API writes")
			}
		})
	}
}
