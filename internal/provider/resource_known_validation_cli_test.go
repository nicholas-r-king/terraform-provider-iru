// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
)

// Check known constraints with unrelated unresolved expressions. None of these
// plans adopts state or contacts an API; unknown-dependent valid choices defer.
func TestTerraformCLIRemainingResourceKnownValidation(t *testing.T) {
	u := "terraform_data.pending.output"
	quoted := func(v string) string { return fmt.Sprintf("%q", v) }
	for _, tc := range []struct {
		kind, name string
		fields     map[string]string
		deferPlan  bool
	}{
		{"custom_app", "type", map[string]string{"name": u, "install_type": `"invalid"`}, false},
		{"custom_app", "name", map[string]string{"name": `" "`, "preinstall_script": u}, false},
		{"custom_app", "key", map[string]string{"name": u, "file_key": `" "`}, false},
		{"custom_app", "source-path", map[string]string{"file_key": "null", "source_file": `"bad\npath"`, "source_sha256": quoted(strings.Repeat("a", 64))}, false},
		{"custom_app", "zip-path", map[string]string{"name": u, "install_type": `"zip"`}, false},
		{"custom_app", "enforcement", map[string]string{"name": u, "install_enforcement": `"invalid"`}, false},
		{"custom_app", "missing-audit", map[string]string{"name": u, "install_enforcement": `"continuously_enforce"`}, false},
		{"custom_app", "unsupported-audit", map[string]string{"name": u, "audit_script": `"audit"`}, false},
		{"custom_app", "no-enforcement-hidden", map[string]string{"name": u, "install_enforcement": `"no_enforcement"`}, false},
		{"custom_app", "invalid-category", map[string]string{"name": u, "show_in_self_service": u, "self_service_category_id": `"invalid"`}, false},
		{"custom_app", "hidden-category", map[string]string{"name": u, "self_service_category_id": quoted(syntheticCategory)}, false},
		{"custom_app", "hidden-recommendation", map[string]string{"name": u, "self_service_recommended": "true"}, false},
		{"custom_app", "unknown-enforcement", map[string]string{"install_enforcement": u}, true},
		{"custom_app", "unknown-visibility", map[string]string{"show_in_self_service": u, "install_enforcement": `"no_enforcement"`, "self_service_category_id": quoted(syntheticCategory)}, true},
		{"in_house_app", "name", map[string]string{"name": `" "`, "active": u}, false},
		{"in_house_app", "key", map[string]string{"name": u, "file_key": `" "`}, false},
		{"in_house_app", "platforms", map[string]string{"name": u, "runs_on_iphone": "false"}, false},
		{"in_house_app", "source-path", map[string]string{"file_key": "null", "source_file": `"bad\npath"`, "source_sha256": quoted(strings.Repeat("a", 64))}, false},
		{"in_house_app", "unknown-platform", map[string]string{"runs_on_iphone": u}, true},
		{"blueprint", "name", map[string]string{"name": `" "`, "enrollment_code": u}, false},
		{"blueprint", "type", map[string]string{"name": u, "type": `"invalid"`}, false},
		{"blueprint", "source-type", map[string]string{"name": u, "source_type": `"invalid"`, "source_id": quoted(syntheticCategory)}, false},
		{"blueprint", "source-type-with-unknown-id", map[string]string{"source_type": `"invalid"`, "source_id": u}, false},
		{"blueprint", "source-id", map[string]string{"name": u, "source_type": `"blueprint"`, "source_id": `"invalid"`}, false},
		{"blueprint", "source-pair", map[string]string{"name": u, "source_type": `"blueprint"`}, false},
		{"blueprint", "code", map[string]string{"name": u, "enrollment_code": `"123"`}, false},
		{"blueprint", "unknown-source", map[string]string{"source_type": `"blueprint"`, "source_id": u}, true},
		{"ade_integration", "email", map[string]string{"email": `"invalid"`, "phone": u}, false},
		{"ade_integration", "phone", map[string]string{"email": u, "phone": `" "`}, false},
		{"ade_integration", "blueprint", map[string]string{"email": u, "blueprint_id": `"invalid"`}, false},
		{"ade_integration", "conflict", map[string]string{"email": u, "use_blueprint_routing": "true"}, false},
		{"ade_integration", "missing-blueprint", map[string]string{"phone": u, "blueprint_id": "null"}, false},
		{"ade_integration", "unknown-route", map[string]string{"use_blueprint_routing": u, "blueprint_id": "null"}, true},
		{"ade_integration", "unknown-blueprint", map[string]string{"blueprint_id": u}, true},
		{"device_settings", "known-bad-tag", map[string]string{"tags": `[%s, " "]`}, false},
		{"ade_device_settings", "known-bad-tag", map[string]string{"tags": `[%s, " "]`}, false},
		{"device_settings", "known-null-tag", map[string]string{"tags": `[%s, null]`}, false},
		{"ade_device_settings", "known-null-tag", map[string]string{"tags": `[%s, null]`}, false},
		{"custom_script", "known-bad-frequency", map[string]string{"name": u, "execution_frequency": `"invalid"`}, false},
		{"device_note", "known-bad-content", map[string]string{"device_id": u, "content": `" "`}, false},
		{"blueprint_routing", "known-bad-code", map[string]string{"enrollment_code": `" "`, "enrollment_active": u}, false},
		{"blueprint_library_item_assignment", "known-bad-id", map[string]string{"blueprint_id": u, "library_item_id": `"invalid"`}, false},
		{"tag", "known-bad-name", map[string]string{"name": `" "`}, false},
		{"tag", "unknown-name", map[string]string{"name": u}, true},
	} {
		t.Run(tc.kind+"/"+tc.name, func(t *testing.T) {
			var calls atomic.Int64
			run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(405) }))
			values := map[string]string{}
			switch tc.kind {
			case "custom_app":
				values = map[string]string{"name": `"Synthetic"`, "file_key": `"synthetic/pkg"`, "install_type": `"package"`}
			case "in_house_app":
				values = map[string]string{"name": `"Synthetic"`, "file_key": `"synthetic/ipa"`, "runs_on_iphone": "true"}
			case "blueprint":
				values = map[string]string{"name": `"Synthetic"`, "type": `"map"`}
			case "ade_integration":
				values = map[string]string{"blueprint_id": quoted(syntheticCategory), "email": `"synthetic@example.invalid"`, "phone": `"555"`}
			case "device_settings", "ade_device_settings":
				values = map[string]string{"device_id": quoted(syntheticID)}
			case "custom_script":
				values = map[string]string{"name": `"Synthetic"`, "script": `"exit 0"`}
			}
			for key, value := range tc.fields {
				values[key] = strings.ReplaceAll(value, "%s", u)
			}
			keys := make([]string, 0, len(values))
			for key := range values {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			var config strings.Builder
			fmt.Fprintf(&config, "resource \"terraform_data\" \"pending\" { input=true }\nresource %q \"example\" {\n", "iru_"+tc.kind)
			for _, key := range keys {
				fmt.Fprintf(&config, "%s=%s\n", key, values[key])
			}
			config.WriteString("}\n")
			write(config.String())
			want := 1
			if tc.deferPlan {
				want = 2
			}
			run(want, "plan", "-no-color", "-detailed-exitcode")
			if calls.Load() != 0 {
				t.Fatal("validation/unknown planning reached the API")
			}
		})
	}
}
