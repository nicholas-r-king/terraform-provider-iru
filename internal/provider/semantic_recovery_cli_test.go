// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"encoding/json"
	"fmt"
	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"testing"
)

func TestTerraformCLIPartialDeviceAdoptionRetainsLocator(t *testing.T) {
	var mu sync.Mutex
	asset := "before"
	patches := 0
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/devices/"+syntheticID {
			t.Error("unexpected path")
			w.WriteHeader(400)
			return
		}
		if r.Method == "PATCH" {
			patches++
			asset = "after"
			w.WriteHeader(503)
			return
		}
		if r.Method != "GET" {
			t.Error("unexpected method")
			w.WriteHeader(405)
			return
		}
		_, _ = fmt.Fprintf(w, `{"device_id":%q,"blueprint_id":%q,"user":null,"asset_tag":%q,"tags":[]}`, syntheticID, syntheticCategory, asset)
	}))
	write(fmt.Sprintf("resource \"iru_device_settings\" \"example\" {\n device_id=%q\n asset_tag=\"after\"\n}\n", syntheticID))
	run(1, "apply", "-auto-approve", "-no-color")
	stateList := strings.TrimSpace(run(0, "state", "list"))
	var persisted map[string]any
	if json.Unmarshal([]byte(run(0, "state", "pull")), &persisted) != nil {
		t.Fatal("partial state invalid")
	}
	resources := persisted["resources"].([]any)
	attrs := resources[0].(map[string]any)["instances"].([]any)[0].(map[string]any)["attributes"].(map[string]any)
	if attrs["id"] != syntheticID || attrs["device_id"] != syntheticID {
		t.Fatal("partial state lost exact device UUID")
	}
	mu.Lock()
	defer mu.Unlock()
	if patches != 1 || asset != "after" || stateList != "iru_device_settings.example" {
		t.Fatal("failed adoption did not retain the exact resource locator after one ambiguous write")
	}
	t.Log("One ambiguous PATCH retained the existing device locator; no write retry or device lifecycle operation occurred.")
}

func TestTerraformCLISignificantScriptWhitespaceDrift(t *testing.T) {
	prior := "#!/bin/sh\ncat <<'END'\none\nEND\n"
	changed := "#!/bin/sh\ncat <<'END'\none\nEND \n"
	output := func(script string) string {
		cmd := exec.Command("/bin/sh")
		cmd.Stdin = strings.NewReader(script)
		out, err := cmd.Output()
		if err != nil {
			t.Fatal("hardcoded local fixture failed")
		}
		return string(out)
	}
	if output(prior) == output(changed) {
		t.Fatal("fixtures were not semantically different")
	}
	model := syntheticModel()
	object := client.Script{ID: syntheticID, ScriptInput: model.input()}
	object.Script = prior
	var mu sync.Mutex
	writes := 0
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method != "GET" {
			writes++
			w.WriteHeader(405)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(object)
	}))
	write(fmt.Sprintf("resource \"iru_custom_script\" \"example\" {\n name=%q\n script=%q\n active=false\n}\n", object.Name, prior))
	run(0, "import", "-no-color", "iru_custom_script.example", syntheticID)
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	object.Script = changed
	mu.Unlock()
	run(2, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	defer mu.Unlock()
	if writes != 0 {
		t.Fatal("review read probe wrote to synthetic API")
	}
	t.Log("Semantically different hardcoded scripts now produce a repair plan; import/refresh API writes remain zero.")
}

func TestTerraformCLIPartialADEAdoptionRetainsLocator(t *testing.T) {
	var mu sync.Mutex
	asset := "before"
	patches := 0
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/integrations/apple/ade/devices/"+syntheticID {
			t.Error("unexpected path")
			w.WriteHeader(400)
			return
		}
		if r.Method == "PATCH" {
			patches++
			asset = "after"
			w.WriteHeader(503)
			return
		}
		if r.Method != "GET" {
			t.Error("unexpected method")
			w.WriteHeader(405)
			return
		}
		_, _ = fmt.Fprintf(w, `{"id":%q,"blueprint_id":%q,"user_id":null,"asset_tag":%q,"use_blueprint_routing":false,"override_dep_profile_id":null}`, syntheticID, syntheticCategory, asset)
	}))
	write(fmt.Sprintf("resource \"iru_ade_device_settings\" \"example\" {\n device_id=%q\n asset_tag=\"after\"\n}\n", syntheticID))
	run(1, "apply", "-auto-approve", "-no-color")
	stateList := strings.TrimSpace(run(0, "state", "list"))
	var persisted map[string]any
	if json.Unmarshal([]byte(run(0, "state", "pull")), &persisted) != nil {
		t.Fatal("partial state invalid")
	}
	resources := persisted["resources"].([]any)
	attrs := resources[0].(map[string]any)["instances"].([]any)[0].(map[string]any)["attributes"].(map[string]any)
	if attrs["id"] != syntheticID || attrs["device_id"] != syntheticID {
		t.Fatal("partial state lost exact device UUID")
	}
	mu.Lock()
	defer mu.Unlock()
	if patches != 1 || asset != "after" || stateList != "iru_ade_device_settings.example" {
		t.Fatal("failed adoption did not retain the exact resource locator after one ambiguous write")
	}
	t.Log("One ambiguous PATCH retained the existing device locator; no write retry or device lifecycle operation occurred.")
}

func TestTerraformCLIPartialRoutingAdoptionRetainsLocator(t *testing.T) {
	var mu sync.Mutex
	code := "synthetic-before"
	patches := 0
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/blueprint-routing/" {
			t.Error("unexpected path")
			w.WriteHeader(400)
			return
		}
		switch r.Method {
		case http.MethodGet:
			fmt.Fprintf(w, `{"enrollment_code":{"code":%q,"is_active":false}}`, code)
		case http.MethodPatch:
			patches++
			code = "synthetic-after"
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			t.Error("unexpected method")
			w.WriteHeader(405)
		}
	}))
	write("resource \"iru_blueprint_routing\" \"example\" {\n enrollment_code=\"synthetic-after\"\n enrollment_active=false\n}\n")
	run(1, "apply", "-auto-approve", "-no-color")
	var state struct {
		Resources []struct {
			Instances []struct{ Attributes map[string]any }
		}
	}
	if json.Unmarshal([]byte(run(0, "state", "pull")), &state) != nil || len(state.Resources) != 1 || len(state.Resources[0].Instances) != 1 {
		t.Fatal("failed routing adoption lost its recoverable state locator")
	}
	attrs := state.Resources[0].Instances[0].Attributes
	if attrs["id"] != "tenant" || attrs["enrollment_code"] != "synthetic-before" || attrs["enrollment_active"] != false {
		t.Fatal("failed routing adoption did not retain prior observed settings")
	}
	mu.Lock()
	defer mu.Unlock()
	if patches != 1 || code != "synthetic-after" {
		t.Fatal("ambiguous routing PATCH was replayed or never attempted")
	}
	t.Log("Ambiguous singleton PATCH retained tenant identity and prior observed settings after exactly one write; remote success is not inferred.")
}
