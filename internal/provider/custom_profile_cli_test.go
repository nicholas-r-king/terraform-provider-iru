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

func TestTerraformCLIProfile(t *testing.T) {
	model := syntheticProfile()
	object := &client.Profile{ID: syntheticID, MDMIdentifier: "com.example.synthetic", ProfileInput: model.input()}
	var mu sync.Mutex
	uploads := 0
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
			input := model.input()
			if object != nil {
				input = object.ProfileInput
			}
			if strings.HasPrefix(req.Header.Get("Content-Type"), "multipart/form-data;") {
				if req.ParseMultipartForm(2*1024*1024) != nil {
					t.Error("invalid multipart profile")
					w.WriteHeader(400)
					return
				}
				defer req.MultipartForm.RemoveAll()
				file, _, err := req.FormFile("file")
				if err != nil {
					t.Error("missing profile file")
					w.WriteHeader(400)
					return
				}
				defer file.Close()
				content, err := io.ReadAll(file)
				if err != nil {
					t.Error("profile file unreadable")
					return
				}
				input.Profile = string(content)
				uploads++
			} else if req.ParseForm() != nil {
				t.Error("invalid profile fields")
				return
			}
			input.Name, input.Active = req.FormValue("name"), req.FormValue("active") == "true"
			input.RunsOnMac, input.RunsOnIphone, input.RunsOnIpad = req.FormValue("runs_on_mac") == "true", req.FormValue("runs_on_iphone") == "true", req.FormValue("runs_on_ipad") == "true"
			input.RunsOnTV, input.RunsOnVision = req.FormValue("runs_on_tv") == "true", req.FormValue("runs_on_vision") == "true"
			if input.Validate() != nil {
				t.Error("invalid profile content/targets")
				w.WriteHeader(400)
				return
			}
			object = &client.Profile{ID: syntheticID, MDMIdentifier: "com.example.synthetic", ProfileInput: input}
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
		if err := json.NewEncoder(w).Encode(object); err != nil {
			t.Error("profile mock encode failed")
		}
	}))
	config := func(name, content string) {
		write(fmt.Sprintf("resource \"iru_custom_profile\" \"example\" {\n name = %q\n profile = %q\n runs_on_mac = true\n}\n", name, content))
	}
	content := syntheticProfileXML
	config("Synthetic profile", content)
	run(0, "validate", "-no-color")
	run(0, "import", "-no-color", "iru_custom_profile.example", strings.ToUpper(syntheticID))
	run(0, "plan", "-no-color", "-detailed-exitcode")
	config("Synthetic renamed", content)
	run(0, "apply", "-auto-approve", "-no-color")
	mu.Lock()
	if uploads != 0 {
		t.Error("profile metadata change unnecessarily uploaded content")
	}
	mu.Unlock()
	content = strings.Replace(content, "Synthetic profile", "Synthetic updated", 1)
	config("Synthetic renamed", content)
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	if uploads != 1 {
		t.Error("profile content change must use one multipart upload")
	}
	object.Name = "Synthetic external drift"
	mu.Unlock()
	run(2, "plan", "-no-color", "-detailed-exitcode")
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "destroy", "-auto-approve", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	mu.Lock()
	object = nil
	mu.Unlock()
	run(0, "apply", "-refresh-only", "-auto-approve", "-no-color")
	if strings.TrimSpace(run(0, "state", "list")) != "" {
		t.Fatal("profile 404 refresh did not remove state")
	}
	config("Synthetic profile", "<plist>")
	run(1, "validate", "-no-color")
}
