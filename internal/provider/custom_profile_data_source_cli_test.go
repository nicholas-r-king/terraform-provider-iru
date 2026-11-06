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

func TestTerraformCLICustomProfileConfiguration(t *testing.T) {
	var mu sync.Mutex
	object := lookupProfileFixture()
	status, transient, gets := 200, 0, 0
	malformed := false
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if req.Method != http.MethodGet || req.URL.Path != "/api/v1/library/custom-profiles/"+profileLookupID || req.URL.RawQuery != "" || req.Header.Get("Authorization") != "Bearer "+cliToken {
			t.Error("profile configuration lookup attempted a write or collection/pagination read")
			w.WriteHeader(405)
			return
		}
		gets++
		w.Header().Set("Content-Type", "application/json")
		if transient > 0 {
			transient--
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(503)
			_, _ = io.WriteString(w, "synthetic-private-transient-response")
			return
		}
		if status != 200 {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, "synthetic-private-error-response")
			return
		}
		if malformed {
			// Missing/null flags must not refresh existing values to false.
			_ = json.NewEncoder(w).Encode(map[string]any{"id": profileLookupID, "name": "Synthetic", "profile": lookupProfileXML, "runs_on_mac": nil})
			return
		}
		_ = json.NewEncoder(w).Encode(object)
	}))
	write(fmt.Sprintf("data \"iru_custom_profile\" \"example\" {\n library_item_id=%q\n}\noutput \"observed_digest\" {\n value=sha256(data.iru_custom_profile.example.profile)\n sensitive=true\n}\n", strings.ToUpper(profileLookupID)))
	run(0, "validate", "-no-color")
	if out := run(0, "apply", "-auto-approve", "-no-color"); strings.Contains(out, "synthetic-private-profile") {
		t.Fatal("normal CLI output exposed sensitive profile XML")
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
	if json.Unmarshal([]byte(run(0, "show", "-json")), &state) != nil || len(state.Values.RootModule.Resources) != 1 {
		t.Fatal("profile data source state is missing")
	}
	resource := state.Values.RootModule.Resources[0]
	if resource.Address != "data.iru_custom_profile.example" || len(resource.Values) != 11 || resource.Values["profile"] != lookupProfileXML || resource.Sensitive["profile"] != true || resource.Values["library_item_id"] != strings.ToUpper(profileLookupID) || resource.Values["id"] != profileLookupID {
		t.Fatal("profile state lost exact XML, sensitivity, identity or projection")
	}
	for _, key := range []string{"active", "runs_on_iphone", "runs_on_ipad", "runs_on_tv", "runs_on_vision"} {
		if resource.Values[key] != false {
			t.Fatal("remote false became missing/null/unknown")
		}
	}
	mu.Lock()
	object.Profile = strings.Replace(lookupProfileXML, "synthetic-private-profile", "synthetic-private-changed", 1)
	object.Active, object.RunsOnMac, object.RunsOnIphone = true, false, true
	object.RunsOnIpad, object.RunsOnTV, object.RunsOnVision = true, true, true
	object.Name, object.MDMIdentifier = "Synthetic renamed", "com.example.synthetic.changed"
	mu.Unlock()
	if out := run(2, "plan", "-no-color", "-detailed-exitcode"); strings.Contains(out, "synthetic-private") {
		t.Fatal("changed XML appeared in human-readable plan")
	}
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	before := run(0, "show", "-json")
	if json.Unmarshal([]byte(before), &state) != nil || len(state.Values.RootModule.Resources) != 1 {
		t.Fatal("refreshed profile state is missing")
	}
	values := state.Values.RootModule.Resources[0].Values
	for _, key := range []string{"active", "runs_on_iphone", "runs_on_ipad", "runs_on_tv", "runs_on_vision"} {
		if values[key] != true {
			t.Fatal("changed true configuration flag was lost")
		}
	}
	if values["runs_on_mac"] != false || values["name"] != "Synthetic renamed" || values["mdm_identifier"] != "com.example.synthetic.changed" || values["profile"] != object.Profile {
		t.Fatal("refresh did not preserve current flags, identity metadata and exact XML")
	}
	for _, failure := range []int{404, 401, 403, 200} {
		mu.Lock()
		status, malformed = failure, failure == 200
		mu.Unlock()
		if out := run(1, "plan", "-no-color"); strings.Contains(out, "synthetic-private") || strings.Contains(out, "<plist") {
			t.Fatal("response/XML leaked in failure diagnostics")
		}
		if run(0, "show", "-json") != before {
			t.Fatal("failed lookup changed persisted state")
		}
	}
	mu.Lock()
	status, malformed, transient = 200, false, 1
	beforeGets := gets
	mu.Unlock()
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	defer mu.Unlock()
	if transient != 0 || gets != beforeGets+2 {
		t.Fatal("transient GET did not recover with one bounded retry")
	}
}

func TestTerraformCLICustomProfileUnknownSelectorDefersRead(t *testing.T) {
	var reads atomic.Int32
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reads.Add(1)
		w.WriteHeader(500)
	}))
	write(fmt.Sprintf("resource \"terraform_data\" \"selector\" { input=%q }\ndata \"iru_custom_profile\" \"deferred\" { library_item_id=terraform_data.selector.output }\n", profileLookupID))
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
		if item.Address != "data.iru_custom_profile.deferred" {
			continue
		}
		found = true
		if len(item.Change.Actions) != 1 || item.Change.Actions[0] != "read" || item.Change.Unknown["library_item_id"] != true || item.Change.Unknown["profile"] != true || item.Change.Unknown["active"] != true {
			t.Fatal("unknown lookup values became null/false or were prematurely read")
		}
	}
	if !found || reads.Load() != 0 {
		t.Fatal("Terraform did not defer the unknown lookup without API calls")
	}
	for _, input := range []string{"null", `"not-a-uuid"`} {
		write("data \"iru_custom_profile\" \"example\" { library_item_id=" + input + " }\n")
		run(1, "validate", "-no-color")
	}
	write(fmt.Sprintf("data \"iru_custom_profile\" \"example\" {\n library_item_id=%q\n active=false\n}\n", profileLookupID))
	run(1, "validate", "-no-color")
	if reads.Load() != 0 {
		t.Fatal("invalid/computed-only configuration reached the API")
	}
}
