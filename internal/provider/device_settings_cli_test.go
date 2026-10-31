// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func TestTerraformCLIDeviceSettings(t *testing.T) {
	var mu sync.Mutex
	patches := []map[string]any{}
	status := 0
	object := map[string]any{"device_id": syntheticID, "blueprint_id": syntheticCategory, "user": map[string]any{"id": syntheticCategory, "email": "synthetic-private-email"}, "asset_tag": "original", "tags": []string{"unmanaged"}, "serial_number": "synthetic-private-serial"}
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if req.URL.Path != "/api/v1/devices/"+syntheticID || req.Header.Get("Authorization") != "Bearer "+cliToken {
			t.Error("wrong device request")
			w.WriteHeader(404)
			return
		}
		if object == nil {
			w.WriteHeader(404)
			return
		}
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "GET":
		case "PATCH":
			var fields map[string]any
			if json.NewDecoder(req.Body).Decode(&fields) != nil {
				w.WriteHeader(400)
				return
			}
			patches = append(patches, fields)
			for key, value := range fields {
				switch key {
				case "user":
					if value == nil {
						object[key] = nil
					} else {
						object[key] = map[string]any{"id": value, "email": "synthetic-private-email"}
					}
				case "asset_tag", "blueprint_id", "tags":
					object[key] = value
				default:
					t.Error("undocumented device PATCH field")
					w.WriteHeader(400)
					return
				}
			}
		default:
			t.Error("settings resource attempted device creation/deletion/command")
			w.WriteHeader(405)
			return
		}
		_ = json.NewEncoder(w).Encode(object)
	}))
	asset, extra := "original", ""
	config := func() {
		write(fmt.Sprintf("resource \"iru_device_settings\" \"example\" {\n device_id = %q\n asset_tag = %q\n%s}\n", syntheticID, asset, extra))
	}
	config()
	run(0, "validate", "-no-color")
	run(0, "import", "-no-color", "iru_device_settings.example", syntheticID)
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	if len(patches) != 0 {
		t.Error("import changed existing device")
	}
	mu.Unlock()
	asset = "managed"
	config()
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	if len(patches) != 1 || len(patches[0]) != 1 || patches[0]["asset_tag"] != "managed" {
		t.Error("asset-only update wrote unmanaged settings")
	}
	object["user"] = map[string]any{"id": "bbbbbbbb-cccc-4ddd-8eee-ffffffffffff"}
	object["tags"] = []string{"unmanaged-changed"}
	object["blueprint_id"] = "bbbbbbbb-cccc-4ddd-8eee-ffffffffffff"
	mu.Unlock()
	run(0, "plan", "-no-color", "-detailed-exitcode")
	asset = ""
	extra = " user_id = \"\"\n tags = []\n"
	config()
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	last := patches[len(patches)-1]
	if len(last) != 3 || last["user"] != nil || last["asset_tag"] != nil {
		t.Error("documented user/asset/tag clearing lost intent")
	}
	if tags, ok := last["tags"].([]any); !ok || len(tags) != 0 {
		t.Error("tag clearing was not an explicit empty list")
	}
	mu.Unlock()
	extra = fmt.Sprintf(" user_id = %q\n blueprint_id = %q\n tags = [\"managed\"]\n", strings.ToUpper(syntheticCategory), strings.ToUpper(syntheticCategory))
	config()
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	object["asset_tag"] = "external drift"
	mu.Unlock()
	run(2, "plan", "-no-color", "-detailed-exitcode")
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	if last := patches[len(patches)-1]; len(last) != 1 || last["asset_tag"] != nil {
		t.Error("drift repair wrote unrelated settings")
	}
	status = 403
	mu.Unlock()
	run(1, "plan", "-no-color")
	mu.Lock()
	status = 0
	before := len(patches)
	mu.Unlock()
	state := run(0, "show", "-json")
	if strings.Contains(state, "synthetic-private-") {
		t.Fatal("device projection exposed unrelated inventory")
	}
	run(0, "destroy", "-auto-approve", "-no-color")
	mu.Lock()
	if object == nil || len(patches) != before {
		t.Error("destroy changed device settings")
	}
	mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	if len(patches) != before {
		t.Error("re-adoption changed matching settings")
	}
	object = nil
	mu.Unlock()
	run(0, "apply", "-refresh-only", "-auto-approve", "-no-color")
	if strings.TrimSpace(run(0, "state", "list")) != "" {
		t.Fatal("deleted device retained settings state")
	}
}

func TestTerraformCLIADEDeviceSettings(t *testing.T) {
	var mu sync.Mutex
	patches := []map[string]any{}
	reportOverride := true
	object := map[string]any{"id": syntheticID, "blueprint_id": syntheticCategory, "user_id": syntheticCategory, "asset_tag": "original", "use_blueprint_routing": false, "override_dep_profile_id": nil, "serial_number": "synthetic-private-serial"}
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if req.URL.Path != "/api/v1/integrations/apple/ade/devices/"+syntheticID || req.Header.Get("Authorization") != "Bearer "+cliToken {
			t.Error("wrong ADE-device endpoint")
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "GET":
		case "PATCH":
			var fields map[string]any
			if json.NewDecoder(req.Body).Decode(&fields) != nil {
				w.WriteHeader(400)
				return
			}
			patches = append(patches, fields)
			for key, value := range fields {
				switch key {
				case "blueprint_id", "use_blueprint_routing", "user_id", "asset_tag", "override_dep_profile_id":
					object[key] = value
				default:
					t.Error("undocumented ADE field")
					w.WriteHeader(400)
					return
				}
			}
		default:
			t.Error("ADE settings used creation/deletion or device command")
			w.WriteHeader(405)
			return
		}
		copy := map[string]any{}
		for key, value := range object {
			if key != "override_dep_profile_id" || reportOverride {
				copy[key] = value
			}
		}
		_ = json.NewEncoder(w).Encode(copy)
	}))
	extra := ""
	config := func() {
		write(fmt.Sprintf("resource \"iru_ade_device_settings\" \"example\" {\n device_id = %q\n%s}\n", syntheticID, extra))
	}
	config()
	run(0, "import", "-no-color", "iru_ade_device_settings.example", syntheticID)
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	if len(patches) != 0 {
		t.Error("ADE import mutated device")
	}
	mu.Unlock()
	extra = " asset_tag = \"managed\"\n"
	config()
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	if len(patches) != 1 || len(patches[0]) != 1 {
		t.Error("ADE asset change wrote unrelated fields")
	}
	mu.Unlock()
	extra = " asset_tag = \"managed\"\n use_blueprint_routing = true\n"
	config()
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	last := patches[len(patches)-1]
	if last["blueprint_id"] != nil || last["use_blueprint_routing"] != true || len(last) != 2 {
		t.Error("ADE routing switch did not pair null/true")
	}
	before := len(patches)
	mu.Unlock()
	extra = " use_blueprint_routing = false\n"
	config()
	run(1, "apply", "-auto-approve", "-no-color")
	mu.Lock()
	if len(patches) != before {
		t.Error("disabling routing without a target wrote to API")
	}
	mu.Unlock()
	extra = fmt.Sprintf(" blueprint_id = %q\n", strings.ToUpper(syntheticCategory))
	config()
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	last = patches[len(patches)-1]
	if last["use_blueprint_routing"] != false || len(last) != 2 {
		t.Error("ADE Blueprint selection did not disable routing")
	}
	mu.Unlock()
	extra = fmt.Sprintf(" blueprint_id = %q\n override_dep_profile_id = %q\n", strings.ToUpper(syntheticCategory), strings.ToUpper(syntheticCategory))
	config()
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	extra = " override_dep_profile_id = \"\"\n"
	config()
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	last = patches[len(patches)-1]
	if value, present := last["override_dep_profile_id"]; !present || value != nil || len(last) != 1 {
		t.Error("ADE profile relink did not send null")
	}
	before = len(patches)
	mu.Unlock()
	run(0, "destroy", "-auto-approve", "-no-color")
	mu.Lock()
	if len(patches) != before || object["blueprint_id"] == nil {
		t.Error("ADE settings destroy altered the device")
	}
	reportOverride = false
	mu.Unlock()
	extra = fmt.Sprintf(" override_dep_profile_id = %q\n", syntheticCategory)
	config()
	run(1, "apply", "-auto-approve", "-no-color")
	mu.Lock()
	if len(patches) != before {
		t.Error("unverifiable ADE override reached PATCH")
	}
	mu.Unlock()
	extra = " asset_tag = \"\"\n"
	config()
	run(1, "validate", "-no-color")
	extra = " user_id = \"\"\n"
	config()
	run(1, "validate", "-no-color")
	extra = fmt.Sprintf(" blueprint_id = %q\n use_blueprint_routing = true\n", syntheticCategory)
	config()
	run(1, "validate", "-no-color")
}

func TestTerraformCLIDeviceMetadataLookups(t *testing.T) {
	for _, scenario := range []struct {
		name            string
		ade, legacyUser bool
	}{{"enrolled", false, false}, {"ade", true, false}, {"ade-legacy-user", true, true}} {
		t.Run(scenario.name, func(t *testing.T) {
			ade := scenario.ade
			name, endpoint := "iru_device", "/api/v1/devices/"
			body := map[string]any{"device_id": syntheticID, "blueprint_id": syntheticCategory, "user": map[string]any{"id": syntheticCategory, "email": "synthetic-private-email"}, "asset_tag": "synthetic", "tags": []string{}, "serial_number": "synthetic-private-serial"}
			if ade {
				name, endpoint = "iru_ade_device", "/api/v1/integrations/apple/ade/devices/"
				body = map[string]any{"id": syntheticID, "blueprint_id": syntheticCategory, "user_id": nil, "asset_tag": nil, "use_blueprint_routing": false, "serial_number": "synthetic-private-serial"}
				if scenario.legacyUser {
					body["user_id"] = 42
				}
			}
			run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Method != "GET" || req.URL.Path != endpoint+syntheticID {
					t.Error("device lookup made a mutation or wrong request")
					w.WriteHeader(403)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(body)
			}))
			write(fmt.Sprintf("data %q \"example\" {\n device_id = %q\n}\n", name, syntheticID))
			run(0, "apply", "-auto-approve", "-no-color")
			run(0, "plan", "-no-color", "-detailed-exitcode")
			state := run(0, "show", "-json")
			if strings.Contains(state, "synthetic-private-") {
				t.Fatal("lookup exposed unrelated device fields")
			}
			if scenario.legacyUser && !strings.Contains(state, `"user_id":"42"`) {
				t.Fatal("ADE lookup did not preserve the legacy numeric user identity as a decimal string")
			}
		})
	}
}
