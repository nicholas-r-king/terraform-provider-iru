// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

func TestADEManagedContactsSensitiveSchema(t *testing.T) {
	var response resource.SchemaResponse
	(&ADEIntegrationResource{}).Schema(context.Background(), resource.SchemaRequest{}, &response)
	if response.Schema.Version != 0 || len(response.Schema.Attributes) != 9 {
		t.Fatal("contact redaction changed the state schema shape/version")
	}
	for _, key := range []string{"email", "phone"} {
		attribute, ok := response.Schema.Attributes[key].(schema.StringAttribute)
		if !ok || !attribute.Required || !attribute.Sensitive || attribute.WriteOnly || attribute.Computed || attribute.Optional {
			t.Errorf("%s must remain a required stored string with sensitive display metadata", key)
		}
	}
	if diags := response.Schema.ValidateImplementation(context.Background()); diags.HasError() {
		t.Fatal(diags)
	}
}

// Uses only synthetic local state and GET fixtures. Marking existing attributes
// sensitive must not mutate the API, lose values, renew tokens or need replacement.
func TestTerraformCLIADEContactRedactionAndLegacyState(t *testing.T) {
	const email = "synthetic-private-ade@example.invalid"
	const phone = "+1 555 0199 synthetic-private-ade"
	const changedEmail = "synthetic-private-changed@example.invalid"
	const changedPhone = "+1 555 0188 synthetic-private-changed"
	var writes atomic.Int32
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/integrations/apple/ade/"+syntheticID {
			writes.Add(1)
			w.WriteHeader(405)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": syntheticID, "blueprint_id": syntheticCategory, "use_blueprint_routing": false, "defaults": map[string]string{"email": email, "phone": phone}, "server_name": "Synthetic", "access_token_expiry": "2027-11-06T00:00:00Z"})
	}))
	config := func(mail, number, output string) {
		write(fmt.Sprintf("resource \"iru_ade_integration\" \"example\" {\n blueprint_id=%q\n email=%q\n phone=%q\n server_token_version=0\n}\n%s", syntheticCategory, mail, number, output))
	}
	redacted := func(value string) {
		t.Helper()
		for _, private := range []string{email, phone, changedEmail, changedPhone} {
			if strings.Contains(value, private) {
				t.Fatal("contact appeared in normal human-readable CLI output")
			}
		}
	}
	decodeState := func(raw string) map[string]any {
		t.Helper()
		var state map[string]any
		if json.Unmarshal([]byte(raw), &state) != nil {
			t.Fatal("invalid synthetic state")
		}
		return state
	}
	instance := func(state map[string]any) map[string]any {
		t.Helper()
		return state["resources"].([]any)[0].(map[string]any)["instances"].([]any)[0].(map[string]any)
	}
	config(email, phone, "")
	redacted(run(0, "import", "-no-color", "iru_ade_integration.example", syntheticID))
	original := decodeState(run(0, "state", "pull"))
	originalAttributes := instance(original)["attributes"]
	// Simulate a prior version's state with identical values/schema, but no
	// contact sensitivity paths. This file never leaves the synthetic workspace.
	legacy := decodeState(run(0, "state", "pull"))
	paths := instance(legacy)["sensitive_attributes"].([]any)
	filtered := []any{}
	for _, raw := range paths {
		path := raw.([]any)
		step, ok := path[0].(map[string]any)
		if !ok || (step["value"] != "email" && step["value"] != "phone") {
			filtered = append(filtered, raw)
		}
	}
	if len(paths)-len(filtered) != 2 {
		t.Fatal("import did not record both contact sensitivity paths")
	}
	instance(legacy)["sensitive_attributes"] = filtered
	legacy["serial"] = legacy["serial"].(float64) + 1
	file := filepath.Join(t.TempDir(), "legacy-local-state.json")
	raw, err := json.Marshal(legacy)
	if err != nil || os.WriteFile(file, raw, 0600) != nil {
		t.Fatal("cannot prepare synthetic legacy state")
	}
	run(0, "state", "push", file)
	redacted(run(0, "plan", "-no-color", "-detailed-exitcode", "-out=contacts.tfplan"))
	var plan struct {
		ResourceChanges []struct{ Change struct{ Actions []string } } `json:"resource_changes"`
		PlannedValues   struct {
			Root struct {
				Resources []struct {
					Values    map[string]any
					Sensitive map[string]bool `json:"sensitive_values"`
				}
			} `json:"root_module"`
		} `json:"planned_values"`
	}
	if json.Unmarshal([]byte(run(0, "show", "-json", "contacts.tfplan")), &plan) != nil || len(plan.ResourceChanges) != 1 || strings.Join(plan.ResourceChanges[0].Change.Actions, ",") != "no-op" || len(plan.PlannedValues.Root.Resources) != 1 {
		t.Fatal("legacy state did not produce a no-op resource plan")
	}
	observed := plan.PlannedValues.Root.Resources[0]
	if observed.Values["email"] != email || observed.Values["phone"] != phone || !observed.Sensitive["email"] || !observed.Sensitive["phone"] {
		t.Fatal("plan lost contact values or sensitivity metadata")
	}
	redacted(run(0, "apply", "-refresh-only", "-auto-approve", "-no-color"))
	refreshed := decodeState(run(0, "state", "pull"))
	if !reflect.DeepEqual(instance(refreshed)["attributes"], originalAttributes) || instance(refreshed)["schema_version"] != instance(original)["schema_version"] {
		t.Fatal("sensitivity refresh changed stored values/schema or renewal bookkeeping")
	}
	marked := map[string]bool{}
	for _, raw := range instance(refreshed)["sensitive_attributes"].([]any) {
		path := raw.([]any)
		if step, ok := path[0].(map[string]any); ok {
			if key, ok := step["value"].(string); ok {
				marked[key] = true
			}
		}
	}
	if !marked["email"] || !marked["phone"] {
		t.Fatal("refresh did not persist contact sensitivity paths")
	}
	// A changed contact must stay hidden in human diff while machine-readable
	// plan JSON intentionally retains the actual value and sensitivity markers.
	config(changedEmail, changedPhone, "")
	redacted(run(2, "plan", "-no-color", "-detailed-exitcode"))
	config(email, phone, "output \"contact\" { value=iru_ade_integration.example.email }\n")
	out := run(1, "plan", "-no-color")
	redacted(out)
	if !strings.Contains(out, "Output refers to sensitive values") {
		t.Fatal("unmarked derived output did not require explicit sensitive annotation")
	}
	config(email, phone, "output \"contact\" {\n value=iru_ade_integration.example.email\n sensitive=true\n}\n")
	redacted(run(0, "plan", "-no-color"))
	if writes.Load() != 0 {
		t.Fatal("contact redaction/import/state refresh attempted an API write or renewal")
	}
}
