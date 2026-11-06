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

func TestTerraformCLICustomScriptConfiguration(t *testing.T) {
	var mu sync.Mutex
	fields := map[string]any{}
	_ = json.Unmarshal(mustJSON(t, lookupScriptFixture()), &fields)
	delete(fields, "show_in_self_service")
	delete(fields, "self_service_category_id")
	delete(fields, "self_service_recommended")
	status, transient, gets := 200, 0, 0
	malformed := false
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if req.Method != http.MethodGet || req.URL.Path != "/api/v1/library/custom-scripts/"+scriptLookupID || req.URL.RawQuery != "" || req.Header.Get("Authorization") != "Bearer "+cliToken {
			t.Error("configuration lookup attempted a write or collection/pagination read")
			w.WriteHeader(405)
			return
		}
		gets++
		w.Header().Set("Content-Type", "application/json")
		if transient > 0 {
			transient--
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(503)
			_, _ = io.WriteString(w, "synthetic-private-transient")
			return
		}
		if status != 200 {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, "synthetic-private-error")
			return
		}
		if malformed {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": scriptLookupID, "script": lookupScriptText, "active": nil})
			return
		}
		_ = json.NewEncoder(w).Encode(fields)
	}))
	write(fmt.Sprintf("data \"iru_custom_script\" \"example\" { library_item_id=%q }\noutput \"observed_digest\" {\n value=sha256(jsonencode(data.iru_custom_script.example))\n sensitive=true\n}\n", strings.ToUpper(scriptLookupID)))
	run(0, "validate", "-no-color")
	assertSafe := func(out string) {
		t.Helper()
		if strings.Contains(out, "synthetic-private") || strings.Contains(out, "#!/bin/sh") {
			t.Fatal("sensitive script bytes appeared in human output")
		}
	}
	assertSafe(run(0, "apply", "-auto-approve", "-no-color"))
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
	read := func() map[string]any {
		t.Helper()
		if json.Unmarshal([]byte(run(0, "show", "-json")), &state) != nil || len(state.Values.RootModule.Resources) != 1 {
			t.Fatal("configuration state missing")
		}
		obj := state.Values.RootModule.Resources[0]
		if obj.Address != "data.iru_custom_script.example" || len(obj.Values) != 11 || obj.Values["id"] != scriptLookupID || obj.Values["library_item_id"] != strings.ToUpper(scriptLookupID) || obj.Sensitive["script"] != true || obj.Sensitive["remediation_script"] != true {
			t.Fatal("state projection, identity or sensitivity missing")
		}
		return obj.Values
	}
	values := read()
	if values["script"] != lookupScriptText || values["remediation_script"] != lookupRemediationText || values["active"] != false || values["restart"] != false || values["show_in_self_service"] != nil || values["self_service_category_id"] != nil || values["self_service_recommended"] != nil {
		t.Fatal("exact bytes, known false or optional nulls changed")
	}
	// Every managed execution flag and optional metadata transition is observed,
	// without imposing write-side combinations or trimming shell whitespace.
	for _, mode := range []string{"true", "hidden-metadata", "false", "null", "missing", "every-15-min", "no-enforcement", "empty-remediation"} {
		mu.Lock()
		fields["name"] = "Synthetic " + mode
		fields["script"] = lookupScriptText + "  " // semantically meaningful trailing spaces
		fields["active"], fields["restart"] = mode == "true", mode == "true"
		fields["execution_frequency"] = "every_day"
		fields["remediation_script"] = lookupRemediationText
		fields["show_in_self_service"], fields["self_service_recommended"] = false, false
		delete(fields, "self_service_category_id")
		switch mode {
		case "true":
			fields["show_in_self_service"] = true
			fields["self_service_recommended"] = true
			fields["self_service_category_id"] = syntheticCategory
		case "hidden-metadata":
			fields["self_service_category_id"] = syntheticCategory
			fields["self_service_recommended"] = true
		case "null":
			fields["show_in_self_service"] = nil
			fields["self_service_category_id"] = nil
			fields["self_service_recommended"] = nil
		case "missing", "no-enforcement":
			delete(fields, "show_in_self_service")
			delete(fields, "self_service_recommended")
			if mode == "no-enforcement" {
				fields["execution_frequency"] = "no_enforcement"
			}
		case "every-15-min":
			fields["execution_frequency"] = "every_15_min"
		case "empty-remediation":
			fields["remediation_script"] = ""
		}
		expected := map[string]any{}
		for k, v := range fields {
			expected[k] = v
		}
		mu.Unlock()
		assertSafe(run(2, "plan", "-no-color", "-detailed-exitcode"))
		assertSafe(run(0, "apply", "-auto-approve", "-no-color"))
		run(0, "plan", "-no-color", "-detailed-exitcode")
		values = read()
		for _, key := range []string{"name", "script", "remediation_script", "execution_frequency", "active", "restart", "show_in_self_service", "self_service_category_id", "self_service_recommended"} {
			if values[key] != expected[key] {
				t.Fatalf("refresh lost observed field %s in %s", key, mode)
			}
		}
	}
	before := run(0, "show", "-json")
	for _, failure := range []int{404, 401, 403, 200} {
		mu.Lock()
		status, malformed = failure, failure == 200
		mu.Unlock()
		assertSafe(run(1, "plan", "-no-color"))
		if run(0, "show", "-json") != before {
			t.Fatal("failed read changed persisted state")
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
		t.Fatal("bounded transient retry did not recover")
	}
}

func TestTerraformCLICustomScriptUnknownSelectorDefersRead(t *testing.T) {
	var reads atomic.Int32
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reads.Add(1)
		w.WriteHeader(500)
	}))
	write(fmt.Sprintf("resource \"terraform_data\" \"selector\" { input=%q }\ndata \"iru_custom_script\" \"deferred\" { library_item_id=terraform_data.selector.output }\n", scriptLookupID))
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
		if item.Address != "data.iru_custom_script.deferred" {
			continue
		}
		found = true
		if len(item.Change.Actions) != 1 || item.Change.Actions[0] != "read" || item.Change.Unknown["library_item_id"] != true || item.Change.Unknown["script"] != true || item.Change.Unknown["active"] != true {
			t.Fatal("unknown lookup values became null/false or were prematurely read")
		}
	}
	if !found || reads.Load() != 0 {
		t.Fatal("Terraform did not defer the unknown lookup without API calls")
	}
	for _, input := range []string{"null", `"not-a-uuid"`} {
		write("data \"iru_custom_script\" \"example\" { library_item_id=" + input + " }\n")
		run(1, "validate", "-no-color")
	}
	write(fmt.Sprintf("data \"iru_custom_script\" \"example\" {\n library_item_id=%q\n active=false\n}\n", scriptLookupID))
	run(1, "validate", "-no-color")
	if reads.Load() != 0 {
		t.Fatal("invalid/computed-only configuration reached the API")
	}
}
