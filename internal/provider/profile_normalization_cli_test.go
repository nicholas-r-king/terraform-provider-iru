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

// Model the live-observed API rewrite, including a separately generated root UUID.
// The nested payload remains user-managed and must still detect/repair drift.
func TestTerraformCLIProfileGeneratedEnvelope(t *testing.T) {
	const source = `<plist version="1.0"><dict><key>PayloadUUID</key><string>source-uuid</string><key>PayloadIdentifier</key><string>source-id</string><key>PayloadDisplayName</key><string>Source name</string><key>PayloadType</key><string>Configuration</string><key>PayloadVersion</key><integer>1</integer><key>PayloadContent</key><array><dict><key>PayloadIdentifier</key><string>nested-id</string><key>Setting</key><string> managed value </string></dict></array></dict></plist>`
	const mdm = "com.kandji.profile.custom." + syntheticID
	const payloadUUID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	render := func(xml, name string) string {
		xml = strings.NewReplacer("<string>source-uuid</string>", "<string>"+payloadUUID+"</string>", "<string>source-id</string>", "<string>"+mdm+"</string>", "<string>Source name</string>", "<string>"+name+"</string>").Replace(xml)
		if !strings.Contains(xml, "<key>PayloadScope</key>") {
			xml = strings.Replace(xml, "<dict>", "<dict><key>PayloadScope</key><string>System</string>", 1)
		}
		return xml
	}
	var mu sync.Mutex
	var object *client.Profile
	uploads, writes := 0, 0
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if req.Header.Get("Authorization") != "Bearer "+cliToken || (req.URL.Path != "/api/v1/library/custom-profiles" && req.URL.Path != "/api/v1/library/custom-profiles/"+syntheticID) {
			t.Error("unexpected profile request")
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "GET":
			if object == nil {
				w.WriteHeader(404)
				return
			}
		case "POST", "PATCH":
			writes++
			input := client.ProfileInput{}
			if object != nil {
				input = object.ProfileInput
			}
			oldName := input.Name
			if strings.HasPrefix(req.Header.Get("Content-Type"), "multipart/form-data;") {
				if err := req.ParseMultipartForm(2 * 1024 * 1024); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				defer req.MultipartForm.RemoveAll()
				file, _, err := req.FormFile("file")
				if err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				defer file.Close()
				content, err := io.ReadAll(file)
				if err != nil {
					t.Error(err)
					return
				}
				input.Profile = string(content)
				uploads++
			} else if err := req.ParseForm(); err != nil {
				t.Error(err)
				return
			}
			input.Name = req.FormValue("name")
			input.Active = req.FormValue("active") == "true"
			input.RunsOnMac = req.FormValue("runs_on_mac") == "true"
			input.RunsOnIphone = req.FormValue("runs_on_iphone") == "true"
			input.RunsOnIpad = req.FormValue("runs_on_ipad") == "true"
			input.RunsOnTV = req.FormValue("runs_on_tv") == "true"
			input.RunsOnVision = req.FormValue("runs_on_vision") == "true"
			input.Profile = render(input.Profile, input.Name)
			if oldName != "" {
				input.Profile = strings.Replace(input.Profile, "<key>PayloadDisplayName</key><string>"+oldName+"</string>", "<key>PayloadDisplayName</key><string>"+input.Name+"</string>", 1)
			}
			object = &client.Profile{ID: syntheticID, MDMIdentifier: mdm, ProfileInput: input}
			if req.Method == "POST" {
				w.WriteHeader(201)
			}
		case "DELETE":
			writes++
			object = nil
			w.WriteHeader(204)
			return
		default:
			t.Error("unexpected method")
			w.WriteHeader(405)
			return
		}
		if err := json.NewEncoder(w).Encode(object); err != nil {
			t.Error(err)
		}
	}))
	config := func(name, content string) {
		write(fmt.Sprintf("resource \"iru_custom_profile\" \"example\" {\n name=%q\n profile=%q\n runs_on_mac=true\n}\n", name, content))
	}
	config("Synthetic profile", source)
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	config("Synthetic renamed", source)
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	if uploads != 1 {
		t.Errorf("metadata-only update uploaded profile: %d", uploads)
	}
	object.Profile = strings.Replace(object.Profile, " managed value ", "external drift", 1)
	mu.Unlock()
	run(2, "plan", "-no-color", "-detailed-exitcode")
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	if uploads != 2 {
		t.Errorf("repair should upload managed source exactly once: %d", uploads)
	}
	canonical := object.Profile
	beforeWrites := writes
	mu.Unlock()
	run(0, "state", "rm", "iru_custom_profile.example")
	config("Synthetic renamed", canonical)
	run(0, "import", "-no-color", "iru_custom_profile.example", syntheticID)
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	if writes != beforeWrites {
		t.Error("import/no-op wrote to API")
	}
	mu.Unlock()
	run(0, "destroy", "-auto-approve", "-no-color")
}
