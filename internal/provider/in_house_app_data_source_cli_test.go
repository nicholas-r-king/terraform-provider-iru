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

func TestTerraformCLIInHouseAppConfiguration(t *testing.T) {
	var mu sync.Mutex
	object := lookupInHouseFixture()
	status, transient, gets := 200, 0, 0
	malformed := false
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/library/ipa-apps/"+inHouseLookupID || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer "+cliToken {
			t.Error("configuration lookup attempted a mutation or unexpected URL")
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
			json.NewEncoder(w).Encode(map[string]any{"id": inHouseLookupID, "name": "Synthetic", "file_key": object.FileKey, "active": nil})
			return
		}
		// Excluded URL/pagination/private fields must never be followed or surfaced.
		values := map[string]any{}
		json.Unmarshal(mustJSON(t, object), &values)
		values["file_url"] = "https://other.invalid/synthetic-private-download"
		values["next"] = "https://other.invalid/synthetic-private-next"
		json.NewEncoder(w).Encode(values)
	}))
	write(fmt.Sprintf("data \"iru_in_house_app\" \"example\" {\n library_item_id=%q\n}\noutput \"observed_key_digest\" {\n value=sha256(data.iru_in_house_app.example.file_key)\n sensitive=true\n}\n", strings.ToUpper(inHouseLookupID)))
	run(0, "validate", "-no-color")
	if out := run(0, "apply", "-auto-approve", "-no-color"); strings.Contains(out, "synthetic-private") {
		t.Fatal("human output exposed the installer key")
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
	pull := func() map[string]any {
		t.Helper()
		if json.Unmarshal([]byte(run(0, "show", "-json")), &state) != nil || len(state.Values.RootModule.Resources) != 1 {
			t.Fatal("missing lookup state")
		}
		r := state.Values.RootModule.Resources[0]
		if r.Address != "data.iru_in_house_app.example" || len(r.Values) != 10 || r.Sensitive["file_key"] != true {
			t.Fatal("lookup projection or sensitivity changed")
		}
		return r.Values
	}
	values := pull()
	if values["file_key"] != object.FileKey || values["id"] != inHouseLookupID || values["library_item_id"] != strings.ToUpper(inHouseLookupID) || values["sha256"] != nil || values["file_size"] != nil {
		t.Fatal("identity, key or null optional metadata changed")
	}
	for _, key := range []string{"active", "runs_on_iphone", "runs_on_ipad", "runs_on_tv"} {
		if values[key] != false {
			t.Fatal("remote false became a default/null")
		}
	}
	mu.Lock()
	sum := strings.Repeat("AB", 32)
	size := int64(0)
	object.SHA256, object.FileSize = &sum, &size
	object.Active, object.RunsOnIphone, object.RunsOnIpad, object.RunsOnTV = true, true, true, true
	object.Name, object.FileKey = "Synthetic renamed", "synthetic-private/changed.ipa"
	mu.Unlock()
	if out := run(2, "plan", "-no-color", "-detailed-exitcode"); strings.Contains(out, "synthetic-private") {
		t.Fatal("changed installer key reached human plan")
	}
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	values = pull()
	if values["file_key"] != object.FileKey || values["name"] != object.Name || values["sha256"] != sum || values["file_size"] != float64(0) {
		t.Fatal("observed metadata was normalized or lost")
	}
	for _, key := range []string{"active", "runs_on_iphone", "runs_on_ipad", "runs_on_tv"} {
		if values[key] != true {
			t.Fatal("true flag was lost")
		}
	}
	before := run(0, "show", "-json")
	for _, failure := range []int{404, 401, 403, 200} {
		mu.Lock()
		status, malformed = failure, failure == 200
		mu.Unlock()
		if out := run(1, "plan", "-no-color"); strings.Contains(out, "synthetic-private") {
			t.Fatal("failed response reached diagnostics")
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
	if transient != 0 || gets != beforeGets+2 {
		t.Error("GET did not recover with one bounded retry")
	}
	object.SHA256, object.FileSize = nil, nil
	object.Active, object.RunsOnIphone, object.RunsOnIpad, object.RunsOnTV = false, false, false, false
	mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	values = pull()
	if values["sha256"] != nil || values["file_size"] != nil || values["runs_on_tv"] != false {
		t.Fatal("refresh from populated optional metadata/true flags to null/false failed")
	}
}

func TestTerraformCLIInHouseAppUnknownSelectorDefersRead(t *testing.T) {
	var reads atomic.Int32
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reads.Add(1)
		w.WriteHeader(500)
	}))
	write(fmt.Sprintf("resource \"terraform_data\" \"selector\" { input=%q }\ndata \"iru_in_house_app\" \"deferred\" { library_item_id=terraform_data.selector.output }\n", inHouseLookupID))
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
		if item.Address != "data.iru_in_house_app.deferred" {
			continue
		}
		found = true
		if len(item.Change.Actions) != 1 || item.Change.Actions[0] != "read" || item.Change.Unknown["library_item_id"] != true || item.Change.Unknown["file_key"] != true || item.Change.Unknown["active"] != true {
			t.Fatal("unknown lookup values became null/false or were prematurely read")
		}
	}
	if !found || reads.Load() != 0 {
		t.Fatal("Terraform did not defer the unknown lookup without API calls")
	}
	for _, input := range []string{"null", `"not-a-uuid"`} {
		write("data \"iru_in_house_app\" \"example\" { library_item_id=" + input + " }\n")
		run(1, "validate", "-no-color")
	}
	write(fmt.Sprintf("data \"iru_in_house_app\" \"example\" {\n library_item_id=%q\n active=false\n}\n", inHouseLookupID))
	run(1, "validate", "-no-color")
	if reads.Load() != 0 {
		t.Fatal("invalid/computed-only configuration reached the API")
	}
}
