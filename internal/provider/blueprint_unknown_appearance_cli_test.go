// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

// An unknown appearance expression is not permission to write unsupported Map
// fields. Core must resolve it and the existing ModifyPlan guard must stop even
// a saved-plan apply before a create/update request reaches the synthetic API.
func TestTerraformCLIMapUnknownAppearanceStopsBeforeWrite(t *testing.T) {
	for _, field := range []string{"color", "icon"} {
		for _, existing := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/existing=%t", field, existing), func(t *testing.T) {
				var writes atomic.Int64
				object := client.ManagedBlueprint{ID: syntheticID, Name: "Synthetic map", Type: "map", Color: "aqua-800", Icon: "ss-files"}
				code, active := "000123", false
				object.EnrollmentCode.Code, object.EnrollmentCode.Active = &code, &active
				run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != "GET" {
						writes.Add(1)
						w.WriteHeader(405)
						return
					}
					if r.URL.Path != "/api/v1/blueprints/"+syntheticID {
						t.Error("unexpected fixture read")
						w.WriteHeader(404)
						return
					}
					_ = json.NewEncoder(w).Encode(object)
				}))
				base := "resource \"iru_blueprint\" \"example\" {\n name=\"Synthetic map\"\n type=\"map\"\n enrollment_active=false\n%s}\n"
				before := ""
				if existing {
					write(fmt.Sprintf(base, ""))
					run(0, "import", "-no-color", "iru_blueprint.example", syntheticID)
					before = run(0, "state", "show", "-no-color", "iru_blueprint.example")
				}
				write("resource \"terraform_data\" \"pending\" { input=\"later\" }\n" + fmt.Sprintf(base, " "+field+"=terraform_data.pending.output\n"))
				plan := filepath.Join(t.TempDir(), "synthetic.tfplan")
				run(2, "plan", "-no-color", "-detailed-exitcode", "-out="+plan)
				output := run(1, "apply", "-auto-approve", "-no-color", plan)
				if writes.Load() != 0 || !strings.Contains(output, "Unsupported map appearance") {
					t.Fatal("unknown appearance bypassed the Map apply guard")
				}
				if existing {
					if run(0, "state", "show", "-no-color", "iru_blueprint.example") != before {
						t.Fatal("blocked appearance update changed imported Map state")
					}
				} else if strings.TrimSpace(run(0, "state", "list")) != "terraform_data.pending" {
					t.Fatal("blocked appearance creation acquired a Map mapping")
				}
			})
		}
	}
}
