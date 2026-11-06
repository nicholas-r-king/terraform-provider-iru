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
	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

func TestManagedInstallerKeysSensitiveSchema(t *testing.T) {
	for _, factory := range []func() resource.Resource{NewCustomAppResource, NewInHouseAppResource} {
		var response resource.SchemaResponse
		factory().Schema(context.Background(), resource.SchemaRequest{}, &response)
		attribute, ok := response.Schema.Attributes["file_key"].(schema.StringAttribute)
		if !ok || !attribute.Sensitive || !attribute.Optional || !attribute.Computed || attribute.Required || attribute.WriteOnly || response.Schema.Version != 0 {
			t.Error("installer key must remain optional/computed stored string, with sensitive display metadata and unchanged state version")
		}
		if diags := response.Schema.ValidateImplementation(context.Background()); diags.HasError() {
			t.Fatal(diags)
		}
	}
}

func TestTerraformCLIManagedInstallerKeyRedactionAndLegacyState(t *testing.T) {
	for _, kind := range []string{"custom_app", "in_house_app"} {
		t.Run(kind, func(t *testing.T) {
			key := "tenants/synthetic-private/installer/original.ipa"
			changedKey := "tenants/synthetic-private/installer/changed.ipa"
			resourceType := "iru_" + kind
			endpoint := "/api/v1/library/custom-apps/" + syntheticID
			var remote any = client.App{ID: syntheticID, AppInput: client.AppInput{Name: "Synthetic", FileKey: key, InstallType: "package", InstallEnforcement: "install_once"}}
			settings := "install_type=\"package\""
			if kind == "in_house_app" {
				endpoint = "/api/v1/library/ipa-apps/" + syntheticID
				remote = client.InHouseApp{ID: syntheticID, InHouseAppInput: client.InHouseAppInput{Name: "Synthetic", FileKey: key, RunsOnIphone: true}}
				settings = "runs_on_iphone=true"
			}
			var writes atomic.Int32
			run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != endpoint {
					writes.Add(1)
					w.WriteHeader(405)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(remote)
			}))
			config := func(value, extra string) {
				write(fmt.Sprintf("resource %q \"example\" {\n name=\"Synthetic\"\n file_key=%q\n %s\n}\n%s", resourceType, value, settings, extra))
			}
			redacted := func(value string) {
				t.Helper()
				if strings.Contains(value, key) || strings.Contains(value, changedKey) {
					t.Fatal("installer key appeared in normal human-readable Terraform output")
				}
			}
			decode := func(raw string) map[string]any {
				t.Helper()
				var value map[string]any
				if json.Unmarshal([]byte(raw), &value) != nil {
					t.Fatal("invalid synthetic local state")
				}
				return value
			}
			instance := func(state map[string]any) map[string]any {
				return state["resources"].([]any)[0].(map[string]any)["instances"].([]any)[0].(map[string]any)
			}
			config(key, "")
			redacted(run(0, "import", "-no-color", resourceType+".example", syntheticID))
			config(changedKey, "")
			redacted(run(2, "plan", "-no-color", "-detailed-exitcode"))
			config(key, "")
			original := decode(run(0, "state", "pull"))
			originalAttributes := instance(original)["attributes"]
			legacy := decode(run(0, "state", "pull"))
			paths := instance(legacy)["sensitive_attributes"].([]any)
			filtered := []any{}
			for _, raw := range paths {
				steps := raw.([]any)
				step, ok := steps[0].(map[string]any)
				if !ok || step["value"] != "file_key" {
					filtered = append(filtered, raw)
				}
			}
			if len(paths)-len(filtered) != 1 {
				t.Fatal("import did not persist the installer-key sensitivity path")
			}
			instance(legacy)["sensitive_attributes"] = filtered
			legacy["serial"] = legacy["serial"].(float64) + 1
			filename := filepath.Join(t.TempDir(), "synthetic-legacy-state.json")
			raw, err := json.Marshal(legacy)
			if err != nil || os.WriteFile(filename, raw, 0600) != nil {
				t.Fatal("could not prepare synthetic legacy state")
			}
			run(0, "state", "push", filename)
			redacted(run(0, "state", "show", "-no-color", resourceType+".example"))
			redacted(run(0, "show", "-no-color"))
			redacted(run(0, "plan", "-no-color", "-detailed-exitcode", "-out=keys.tfplan"))
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
			if json.Unmarshal([]byte(run(0, "show", "-json", "keys.tfplan")), &plan) != nil || len(plan.ResourceChanges) != 1 || strings.Join(plan.ResourceChanges[0].Change.Actions, ",") != "no-op" || len(plan.PlannedValues.Root.Resources) != 1 {
				t.Fatal("legacy unmarked state did not produce a no-op plan")
			}
			observed := plan.PlannedValues.Root.Resources[0]
			if observed.Values["file_key"] != key || !observed.Sensitive["file_key"] {
				t.Fatal("machine plan lost the key value or its sensitivity flag")
			}
			redacted(run(0, "apply", "-refresh-only", "-auto-approve", "-no-color"))
			refreshed := decode(run(0, "state", "pull"))
			if !reflect.DeepEqual(instance(refreshed)["attributes"], originalAttributes) || instance(refreshed)["schema_version"] != instance(original)["schema_version"] {
				t.Fatal("redaction refresh changed persisted values or state schema")
			}
			marked := false
			for _, raw := range instance(refreshed)["sensitive_attributes"].([]any) {
				steps := raw.([]any)
				if step, ok := steps[0].(map[string]any); ok && step["value"] == "file_key" {
					marked = true
				}
			}
			if !marked {
				t.Fatal("refresh did not persist key sensitivity")
			}
			expression := resourceType + ".example.file_key"
			config(key, "output \"installer\" { value="+expression+" }\n")
			out := run(1, "plan", "-no-color")
			redacted(out)
			if !strings.Contains(out, "Output refers to sensitive values") {
				t.Fatal("unmarked key output was not rejected")
			}
			config(key, "output \"installer\" {\n value="+expression+"\n sensitive=true\n}\n")
			redacted(run(0, "plan", "-no-color"))
			config(key, "resource \"terraform_data\" \"key\" {\n for_each=toset(["+expression+"])\n input=each.key\n}\n")
			out = run(1, "plan", "-no-color")
			redacted(out)
			if !strings.Contains(out, "Invalid for_each argument") || !strings.Contains(strings.ToLower(out), "sensitive") {
				t.Fatal("sensitive key was not refused as a for_each address")
			}
			if writes.Load() != 0 {
				t.Fatal("key redaction/import/refresh attempted an API write or installer upload")
			}
		})
	}
}
