// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTerraformCLIProfileKnownInvalidInputsAreNotHiddenByUnknowns(t *testing.T) {
	for _, tc := range []struct{ name, fields, diagnostic string }{
		{"unknown-name-invalid-xml", `name=terraform_data.later.output
profile="synthetic-private-invalid-profile"
runs_on_mac=true`, "Invalid profile XML"},
		{"unknown-profile-invalid-name", `name=" "
profile=terraform_data.later.output
runs_on_mac=true`, "Invalid profile name"},
		{"unknown-name-no-platform", `name=terraform_data.later.output
profile="<plist><dict/></plist>"
runs_on_mac=false
runs_on_iphone=false
runs_on_ipad=false
runs_on_tv=false
runs_on_vision=false`, "Profile platform required"},
		{"unknown-profile-deferred", `name="Synthetic"
profile=terraform_data.later.output
runs_on_mac=true`, ""},
		{"unknown-platform-deferred", `name="Synthetic"
profile="<plist><dict/></plist>"
runs_on_mac=terraform_data.later.output`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int64
			run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(http.StatusMethodNotAllowed)
			}))
			write(fmt.Sprintf("resource \"terraform_data\" \"later\" { input=false }\nresource \"iru_custom_profile\" \"example\" {\n%s\n}\n", tc.fields))
			if tc.diagnostic == "" {
				run(2, "plan", "-no-color", "-detailed-exitcode")
				// These deferred expressions resolve to invalid inputs. The local
				// dependency may be created, but apply must still stop before Iru.
				run(1, "apply", "-auto-approve", "-no-color")
				if strings.TrimSpace(run(0, "state", "list")) != "terraform_data.later" {
					t.Fatal("invalid resolved profile acquired a resource mapping")
				}
			} else {
				if output := run(1, "plan", "-no-color"); !strings.Contains(output, tc.diagnostic) {
					t.Fatal("known invalid input was not diagnosed independently")
				}
			}
			if calls.Load() != 0 {
				t.Fatal("configuration validation or unknown plan reached the API")
			}
		})
	}
}
