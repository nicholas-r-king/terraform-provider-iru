// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

func TestTerraformCLIScriptImportNormalizedContent(t *testing.T) {
	model := syntheticModel()
	object := client.Script{ID: syntheticID, ScriptInput: model.input()}
	object.Script = strings.TrimSpace(object.Script)
	var writes atomic.Int32
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet {
			writes.Add(1)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if req.URL.Path != "/api/v1/library/custom-scripts/"+syntheticID {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(object)
	}))
	config := func(script string) {
		write(fmt.Sprintf("resource \"iru_custom_script\" \"example\" {\n name = %q\n script = %s\n active = false\n}\n", object.Name, script))
	}
	// Import must preserve actual remote content; it cannot reconstruct whitespace
	// stripped by Iru from an earlier creation on another Terraform root.
	config(fmt.Sprintf("%q", object.Script+"\n"))
	run(0, "import", "-no-color", "iru_custom_script.example", syntheticID)
	run(2, "plan", "-no-color", "-detailed-exitcode")
	config(fmt.Sprintf("trimspace(%q)", object.Script+"\n"))
	run(0, "plan", "-no-color", "-detailed-exitcode")
	config(fmt.Sprintf("trimspace(%q)", object.Script+"\necho substantive-change\n"))
	run(2, "plan", "-no-color", "-detailed-exitcode")
	if writes.Load() != 0 {
		t.Fatal("import and read-only plans attempted API writes")
	}
}
