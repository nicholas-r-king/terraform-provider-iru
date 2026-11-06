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

func TestTerraformCLICustomAppConfiguration(t *testing.T) {
	var mu sync.Mutex
	fields := map[string]any{}
	_ = json.Unmarshal(mustJSON(t, lookupAppFixture()), &fields)
	optional := []string{"unzip_location", "show_in_self_service", "self_service_category_id", "self_service_recommended", "sha256", "file_size"}
	for _, key := range optional {
		delete(fields, key)
	}
	fields["file_url"] = "https://other.invalid/private?token=synthetic-private-signed-url"
	fields["upload_credentials"] = map[string]any{"secret": "synthetic-private-upload"}
	status, transient, gets := 200, 0, 0
	malformed := false
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if req.Method != http.MethodGet || req.URL.Path != "/api/v1/library/custom-apps/"+appLookupID || req.URL.RawQuery != "" || req.Header.Get("Authorization") != "Bearer "+cliToken {
			t.Error("App lookup attempted write, URL download or collection request")
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
			_ = json.NewEncoder(w).Encode(map[string]any{"id": appLookupID, "file_url": fields["file_url"], "active": nil})
			return
		}
		_ = json.NewEncoder(w).Encode(fields)
	}))
	write(fmt.Sprintf("data \"iru_custom_app\" \"example\" { library_item_id=%q }\noutput \"observed_digest\" {\n value=sha256(jsonencode(data.iru_custom_app.example))\n sensitive=true\n}\n", strings.ToUpper(appLookupID)))
	assertSafe := func(out string) {
		t.Helper()
		if strings.Contains(out, "synthetic-private") || strings.Contains(out, "#!/bin/sh") {
			t.Fatal("sensitive content reached human CLI output")
		}
	}
	run(0, "validate", "-no-color")
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
		raw := run(0, "show", "-json")
		if strings.Contains(raw, "synthetic-private-signed-url") || strings.Contains(raw, "synthetic-private-upload") || json.Unmarshal([]byte(raw), &state) != nil || len(state.Values.RootModule.Resources) != 1 {
			t.Fatal("URL/credentials leaked or state missing")
		}
		obj := state.Values.RootModule.Resources[0]
		if obj.Address != "data.iru_custom_app.example" || len(obj.Values) != 17 || obj.Values["id"] != appLookupID || obj.Values["library_item_id"] != strings.ToUpper(appLookupID) {
			t.Fatal("projection/identity changed")
		}
		for _, key := range []string{"file_key", "audit_script", "preinstall_script", "postinstall_script"} {
			if obj.Sensitive[key] != true {
				t.Fatal("private output lacks sensitivity")
			}
		}
		return obj.Values
	}
	values := read()
	for _, key := range optional {
		if values[key] != nil {
			t.Fatal("missing metadata became false/zero/empty")
		}
	}
	if values["active"] != false || values["restart"] != false || values["file_key"] != lookupAppKey {
		t.Fatal("core metadata changed")
	}
	// Exercise all supported install types/enforcement values and every exposed
	// field transition without applying the resource's write-side combinations.
	for _, mode := range []string{"true", "hidden-metadata", "false-zero-empty", "null", "missing", "zip", "image", "no-enforcement"} {
		mu.Lock()
		fields["name"] = "Synthetic " + mode
		fields["file_key"] = lookupAppKey + ".changed"
		fields["install_type"] = "package"
		fields["install_enforcement"] = "install_once"
		fields["active"], fields["restart"] = false, false
		for _, key := range []string{"audit_script", "preinstall_script", "postinstall_script"} {
			fields[key] = lookupAppScript + "  "
		}
		for _, key := range optional {
			delete(fields, key)
		}
		switch mode {
		case "true", "hidden-metadata":
			fields["active"], fields["restart"] = true, true
			fields["install_enforcement"] = "continuously_enforce"
			fields["show_in_self_service"] = mode == "true"
			fields["self_service_category_id"] = syntheticCategory
			fields["self_service_recommended"] = true
			fields["sha256"] = strings.Repeat("A", 64)
			fields["file_size"] = float64(12345)
			fields["unzip_location"] = "/Applications/Synthetic"
		case "false-zero-empty":
			fields["show_in_self_service"], fields["self_service_recommended"] = false, false
			fields["unzip_location"] = ""
			fields["file_size"] = float64(0)
			for _, key := range []string{"audit_script", "preinstall_script", "postinstall_script"} {
				fields[key] = ""
			}
		case "null":
			for _, key := range optional {
				fields[key] = nil
			}
		case "zip":
			fields["install_type"] = "zip"
			fields["unzip_location"] = "/Applications/Other"
		case "image":
			fields["install_type"] = "image"
		case "no-enforcement":
			fields["install_enforcement"] = "no_enforcement"
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
		for _, key := range []string{"name", "file_key", "install_type", "install_enforcement", "audit_script", "preinstall_script", "postinstall_script", "active", "restart", "unzip_location", "show_in_self_service", "self_service_category_id", "self_service_recommended", "sha256", "file_size"} {
			if values[key] != expected[key] {
				t.Fatalf("observed field %s changed in %s", key, mode)
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
		t.Fatal("bounded transient retry failed")
	}
}

func TestTerraformCLICustomAppUnknownSelectorDefersRead(t *testing.T) {
	var reads atomic.Int32
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reads.Add(1)
		w.WriteHeader(500)
	}))
	write(fmt.Sprintf("resource \"terraform_data\" \"selector\" { input=%q }\ndata \"iru_custom_app\" \"deferred\" { library_item_id=terraform_data.selector.output }\n", appLookupID))
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
		if item.Address != "data.iru_custom_app.deferred" {
			continue
		}
		found = true
		if len(item.Change.Actions) != 1 || item.Change.Actions[0] != "read" || item.Change.Unknown["library_item_id"] != true || item.Change.Unknown["audit_script"] != true || item.Change.Unknown["active"] != true {
			t.Fatal("unknown lookup values became null/false or were prematurely read")
		}
	}
	if !found || reads.Load() != 0 {
		t.Fatal("Terraform did not defer the unknown lookup without API calls")
	}
	for _, input := range []string{"null", `"not-a-uuid"`} {
		write("data \"iru_custom_app\" \"example\" { library_item_id=" + input + " }\n")
		run(1, "validate", "-no-color")
	}
	write(fmt.Sprintf("data \"iru_custom_app\" \"example\" {\n library_item_id=%q\n active=false\n}\n", appLookupID))
	run(1, "validate", "-no-color")
	if reads.Load() != 0 {
		t.Fatal("invalid/computed-only configuration reached the API")
	}
}
