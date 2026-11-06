// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

func TestTerraformCLIInHouseApp(t *testing.T) {
	var object *client.InHouseApp
	var mu sync.Mutex
	replacements := 0
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if req.URL.Path != "/api/v1/library/ipa-apps" && req.URL.Path != "/api/v1/library/ipa-apps/"+syntheticID {
			w.WriteHeader(404)
			return
		}
		switch req.Method {
		case "GET":
			if object == nil {
				w.WriteHeader(404)
				return
			}
		case "POST", "PATCH":
			if req.Method == "PATCH" && object == nil {
				w.WriteHeader(404)
				return
			}
			var fields map[string]json.RawMessage
			if json.NewDecoder(req.Body).Decode(&fields) != nil {
				w.WriteHeader(400)
				return
			}
			encoded, _ := json.Marshal(fields)
			var input client.InHouseAppInput
			_ = json.Unmarshal(encoded, &input)
			if req.Method == "PATCH" {
				if _, replaced := fields["file_key"]; !replaced {
					input.FileKey = object.FileKey
				} else {
					replacements++
				}
			}
			if input.Validate() != nil {
				t.Error("invalid IPA write")
				w.WriteHeader(400)
				return
			}
			object = &client.InHouseApp{ID: syntheticID, InHouseAppInput: input}
			if req.Method == "POST" {
				w.WriteHeader(201)
			}
		case "DELETE":
			object = nil
			w.WriteHeader(204)
			return
		default:
			w.WriteHeader(405)
			return
		}
		_ = json.NewEncoder(w).Encode(object)
	}))
	config := func(name, key string) {
		write(fmt.Sprintf("resource \"iru_in_house_app\" \"example\" {\n name = %q\n file_key = %q\n runs_on_iphone = true\n}\n", name, key))
	}
	key := "tenants/synthetic/library/ipa_apps/example.ipa"
	config("Synthetic IPA", key)
	run(0, "validate", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	run(0, "state", "rm", "iru_in_house_app.example")
	run(0, "import", "-no-color", "iru_in_house_app.example", strings.ToUpper(syntheticID))
	run(0, "plan", "-no-color", "-detailed-exitcode")
	config("Synthetic renamed", key)
	run(0, "apply", "-auto-approve", "-no-color")
	mu.Lock()
	if replacements != 0 {
		t.Error("metadata update replaced IPA")
	}
	object.Name = "Synthetic external drift"
	mu.Unlock()
	run(2, "plan", "-no-color", "-detailed-exitcode")
	run(0, "apply", "-auto-approve", "-no-color")
	config("Synthetic renamed", "tenants/synthetic/library/ipa_apps/replacement.ipa")
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	if replacements != 1 {
		t.Error("installer replacement was not exactly once")
	}
	mu.Unlock()
	run(0, "destroy", "-auto-approve", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	mu.Lock()
	object = nil
	mu.Unlock()
	run(0, "apply", "-refresh-only", "-auto-approve", "-no-color")
	if strings.TrimSpace(run(0, "state", "list")) != "" {
		t.Fatal("IPA 404 retained state")
	}
}

const syntheticNoteDevice = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"

func TestTerraformCLIDeviceNote(t *testing.T) {
	var object *client.DeviceNote
	var mu sync.Mutex
	posts, deletes := 0, 0
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		endpoint := "/api/v1/devices/" + syntheticNoteDevice + "/notes"
		if req.URL.Path != endpoint && req.URL.Path != endpoint+"/"+syntheticID {
			w.WriteHeader(404)
			return
		}
		switch req.Method {
		case "GET":
			if object == nil {
				w.WriteHeader(404)
				return
			}
		case "POST", "PATCH":
			if req.Method == "PATCH" && object == nil {
				w.WriteHeader(404)
				return
			}
			var input struct {
				Content string `json:"content"`
			}
			if json.NewDecoder(req.Body).Decode(&input) != nil {
				w.WriteHeader(400)
				return
			}
			object = &client.DeviceNote{ID: syntheticID, Content: input.Content}
			if req.Method == "POST" {
				posts++
				w.WriteHeader(201)
			}
		case "DELETE":
			deletes++
			object = nil
			w.WriteHeader(200)
			_, _ = io.WriteString(w, `{"deleted_at":"2026-10-29T00:00:00Z"}`)
			return
		default:
			w.WriteHeader(405)
			return
		}
		_ = json.NewEncoder(w).Encode(object)
	}))
	config := func(content string) {
		write(fmt.Sprintf("resource \"iru_device_note\" \"example\" {\n device_id = %q\n content = %q\n}\n", syntheticNoteDevice, content))
	}
	config("<p>Synthetic note</p>")
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	run(0, "state", "rm", "iru_device_note.example")
	run(0, "import", "-no-color", "iru_device_note.example", strings.ToUpper(syntheticNoteDevice+"/"+syntheticID))
	run(0, "plan", "-no-color", "-detailed-exitcode")
	config("<p>Synthetic changed</p>")
	run(0, "apply", "-auto-approve", "-no-color")
	mu.Lock()
	object.Content = "<p>Synthetic external drift</p>"
	mu.Unlock()
	run(2, "plan", "-no-color", "-detailed-exitcode")
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "destroy", "-auto-approve", "-no-color")
	mu.Lock()
	if posts != 1 || deletes != 1 {
		t.Error("Note import/update created duplicate or failed deletion")
	}
	mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	mu.Lock()
	object = nil
	mu.Unlock()
	run(0, "apply", "-refresh-only", "-auto-approve", "-no-color")
	if strings.TrimSpace(run(0, "state", "list")) != "" {
		t.Fatal("Note 404 retained state")
	}
}

func TestTerraformCLIADEIntegration(t *testing.T) {
	var object *client.ADEIntegration
	var mu sync.Mutex
	creates, renews, patches := 0, 0, 0
	token := "synthetic-server-token-only"
	endpoint := "/api/v1/integrations/apple/ade"
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if req.URL.Path != endpoint+"/" && req.URL.Path != endpoint+"/"+syntheticID && req.URL.Path != endpoint+"/"+syntheticID+"/renew" {
			w.WriteHeader(404)
			return
		}
		switch req.Method {
		case "GET":
			if object == nil {
				w.WriteHeader(404)
				return
			}
		case "POST":
			if req.ParseMultipartForm(1024*1024) != nil {
				t.Error("expected ADE multipart")
				w.WriteHeader(400)
				return
			}
			defer req.MultipartForm.RemoveAll()
			f, _, err := req.FormFile("file")
			if err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			bytes, _ := io.ReadAll(f)
			_ = f.Close()
			if string(bytes) != token {
				t.Error("wrong synthetic token")
			}
			if strings.HasSuffix(req.URL.Path, "/renew") {
				renews++
			} else {
				creates++
				object = &client.ADEIntegration{ID: syntheticID, ServerName: "Synthetic Apple Server", AccessTokenExpiry: "2027-10-29T00:00:00Z"}
			}
			object.Blueprint.ID = req.FormValue("blueprint_id")
			email, phone := req.FormValue("email"), req.FormValue("phone")
			object.Defaults.Email, object.Defaults.Phone = &email, &phone
		case "PATCH":
			if object == nil {
				w.WriteHeader(404)
				return
			}
			patches++
			var input client.ADEInput
			if json.NewDecoder(req.Body).Decode(&input) != nil || input.Validate() != nil {
				t.Error("invalid ADE metadata")
				w.WriteHeader(400)
				return
			}
			object.Blueprint.ID = input.BlueprintID
			object.Defaults.Email, object.Defaults.Phone = &input.Email, &input.Phone
		case "DELETE":
			object = nil
			w.WriteHeader(204)
			return
		default:
			w.WriteHeader(405)
			return
		}
		_ = json.NewEncoder(w).Encode(object)
	}))
	config := func(phone string, version int, includeToken bool) {
		secret := ""
		if includeToken {
			secret = fmt.Sprintf(" server_token = base64encode(%q)\n", token)
		}
		write(fmt.Sprintf("resource \"iru_ade_integration\" \"example\" {\n blueprint_id = %q\n email = \"synthetic@example.invalid\"\n phone = %q\n server_token_version = %d\n%s}\n", syntheticNoteDevice, phone, version, secret))
	}
	config("1234567890", 0, true)
	run(0, "validate", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	state := run(0, "show", "-json")
	if strings.Contains(state, token) || strings.Contains(state, "c3ludGhldGljLXNlcnZlci10b2tlbi1vbmx5") {
		t.Fatal("Apple token persisted in state")
	}
	run(0, "state", "rm", "iru_ade_integration.example")
	config("1234567890", 0, false)
	run(0, "import", "-no-color", "iru_ade_integration.example", strings.ToUpper(syntheticID))
	run(0, "plan", "-no-color", "-detailed-exitcode")
	config("9876543210", 0, false)
	run(0, "apply", "-auto-approve", "-no-color")
	mu.Lock()
	if creates != 1 || renews != 0 || patches != 1 {
		t.Error("metadata change resent Apple token")
	}
	object.Defaults.Phone = new(string)
	*object.Defaults.Phone = "Synthetic drift"
	mu.Unlock()
	run(2, "plan", "-no-color", "-detailed-exitcode")
	run(0, "apply", "-auto-approve", "-no-color")
	config("9876543210", 1, true)
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	if renews != 1 {
		t.Error("token renewal trigger not honored exactly once")
	}
	mu.Unlock()
	config("9876543210", 2, false)
	run(1, "apply", "-auto-approve", "-no-color")
	mu.Lock()
	if renews != 1 {
		t.Error("missing token reached renewal API")
	}
	mu.Unlock()
	config("9876543210", 1, false)
	if output := run(1, "destroy", "-auto-approve", "-no-color"); !strings.Contains(output, "ADE integration deletion blocked") {
		t.Fatal("ADE delete not refused")
	}
	if !strings.Contains(run(0, "state", "list"), "iru_ade_integration.example") {
		t.Fatal("blocked ADE deletion lost state")
	}
}
