// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestTerraformCLIADEIntegrationConfiguration(t *testing.T) {
	var mu sync.Mutex
	fixture := lookupADEFixture()
	gets, status, transient := 0, 200, 0
	malformed := false
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/integrations/apple/ade/"+adeLookupID || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer "+cliToken {
			t.Error("ADE lookup attempted a mutation, renewal, collection or unexpected URL")
			w.WriteHeader(405)
			return
		}
		gets++
		w.Header().Set("Content-Type", "application/json")
		if transient > 0 {
			transient--
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(503)
			io.WriteString(w, "synthetic-private-transient")
			return
		}
		if status != 200 {
			w.WriteHeader(status)
			io.WriteString(w, "synthetic-private-response")
			return
		}
		if malformed {
			json.NewEncoder(w).Encode(map[string]any{"id": adeLookupID, "defaults": map[string]any{"email": nil, "phone": "synthetic-private-phone"}})
			return
		}
		fixture["server_token"] = "synthetic-excluded-server-token"
		fixture["access_token"] = "synthetic-excluded-access-token"
		fixture["server_token_version"] = 999
		fixture["next"] = "https://other.invalid/synthetic-private-next"
		json.NewEncoder(w).Encode(fixture)
	}))
	write(fmt.Sprintf("data \"iru_ade_integration\" \"example\" {\n integration_id=%q\n}\noutput \"contact_digest\" {\n value=sha256(data.iru_ade_integration.example.email)\n sensitive=true\n}\n", strings.ToUpper(adeLookupID)))
	run(0, "validate", "-no-color")
	if out := run(0, "apply", "-auto-approve", "-no-color"); strings.Contains(out, "synthetic-private") || strings.Contains(out, "synthetic-excluded") {
		t.Fatal("human output leaked contact or excluded secret")
	}
	run(0, "plan", "-no-color", "-detailed-exitcode")
	var state struct {
		Values struct {
			RootModule struct {
				Resources []struct {
					Address   string
					Values    map[string]any
					Sensitive map[string]any `json:"sensitive_values"`
				}
			} `json:"root_module"`
		}
	}
	show := func() string {
		t.Helper()
		raw := run(0, "show", "-json")
		if strings.Contains(raw, "synthetic-excluded") {
			t.Fatal("token was stored in data-source state")
		}
		return raw
	}
	pull := func() map[string]any {
		t.Helper()
		if json.Unmarshal([]byte(show()), &state) != nil || len(state.Values.RootModule.Resources) != 1 {
			t.Fatal("missing ADE lookup state")
		}
		r := state.Values.RootModule.Resources[0]
		if r.Address != "data.iru_ade_integration.example" || len(r.Values) != 8 || r.Sensitive["email"] != true || r.Sensitive["phone"] != true {
			t.Fatal("ADE projection or sensitivity changed")
		}
		return r.Values
	}
	values := pull()
	contacts := fixture["defaults"].(map[string]any)
	if values["integration_id"] != strings.ToUpper(adeLookupID) || values["id"] != adeLookupID || values["blueprint_id"] != syntheticCategory || values["use_blueprint_routing"] != false || values["email"] != contacts["email"] || values["phone"] != contacts["phone"] {
		t.Fatal("fixed Blueprint or exact contact state changed")
	}
	// Current routing=true must pair with null Blueprint, not an empty ID/default.
	mu.Lock()
	fixture["blueprint_id"], fixture["use_blueprint_routing"] = nil, true
	fixture["defaults"] = map[string]any{"email": "synthetic-private-changed@example.invalid", "phone": "synthetic-private-changed-phone"}
	fixture["server_name"], fixture["access_token_expiry"] = "Synthetic renamed", "2028-11-06T00:00:00Z"
	mu.Unlock()
	if out := run(2, "plan", "-no-color", "-detailed-exitcode"); strings.Contains(out, "synthetic-private") {
		t.Fatal("changed contact leaked in plan")
	}
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	values = pull()
	if values["blueprint_id"] != nil || values["use_blueprint_routing"] != true || values["server_name"] != "Synthetic renamed" || values["access_token_expiry"] != "2028-11-06T00:00:00Z" {
		t.Fatal("routing or reported server metadata not refreshed")
	}
	before := show()
	for _, failure := range []int{404, 401, 403, 200} {
		mu.Lock()
		status, malformed = failure, failure == 200
		mu.Unlock()
		if out := run(1, "plan", "-no-color"); strings.Contains(out, "synthetic-private") || strings.Contains(out, "synthetic-excluded") {
			t.Fatal("failure leaked private payload")
		}
		if show() != before {
			t.Fatal("failed lookup changed persisted state")
		}
	}
	mu.Lock()
	status, malformed, transient = 200, false, 1
	beforeGets := gets
	mu.Unlock()
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	if transient != 0 || gets != beforeGets+2 {
		t.Error("GET did not recover with one retry")
	}
	delete(fixture, "blueprint_id")
	delete(fixture, "use_blueprint_routing")
	fixture["blueprint"] = map[string]any{"id": strings.ToUpper(syntheticCategory)}
	fixture["defaults"] = map[string]any{"email": "", "phone": ""}
	mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	values = pull()
	if values["blueprint_id"] != syntheticCategory || values["use_blueprint_routing"] != false || values["email"] != "" || values["phone"] != "" {
		t.Fatal("legacy fixed assignment or observed empty contacts failed")
	}
}

func TestTerraformCLIADEIntegrationUnknownSelectorDefersRead(t *testing.T) {
	var reads atomic.Int32
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reads.Add(1)
		w.WriteHeader(500)
	}))
	write(fmt.Sprintf("resource \"terraform_data\" \"selector\" { input=%q }\ndata \"iru_ade_integration\" \"deferred\" { integration_id=terraform_data.selector.output }\n", adeLookupID))
	run(2, "plan", "-no-color", "-detailed-exitcode", "-out=unknown.tfplan")
	var plan struct {
		Changes []struct {
			Address string
			Change  struct {
				Actions []string
				Unknown map[string]any `json:"after_unknown"`
			}
		} `json:"resource_changes"`
	}
	if json.Unmarshal([]byte(run(0, "show", "-json", "unknown.tfplan")), &plan) != nil {
		t.Fatal("deferred plan was not valid JSON")
	}
	found := false
	for _, item := range plan.Changes {
		if item.Address != "data.iru_ade_integration.deferred" {
			continue
		}
		found = true
		if len(item.Change.Actions) != 1 || item.Change.Actions[0] != "read" || item.Change.Unknown["integration_id"] != true || item.Change.Unknown["email"] != true || item.Change.Unknown["use_blueprint_routing"] != true {
			t.Fatal("unknown lookup values became null/false or were prematurely read")
		}
	}
	if !found || reads.Load() != 0 {
		t.Fatal("Terraform did not defer the unknown lookup without API calls")
	}
	for _, input := range []string{"null", `"not-a-uuid"`} {
		write("data \"iru_ade_integration\" \"example\" { integration_id=" + input + " }\n")
		run(1, "validate", "-no-color")
	}
	write(fmt.Sprintf("data \"iru_ade_integration\" \"example\" {\n integration_id=%q\n use_blueprint_routing=false\n}\n", adeLookupID))
	run(1, "validate", "-no-color")
	if reads.Load() != 0 {
		t.Fatal("invalid/computed-only configuration reached the API")
	}
}
