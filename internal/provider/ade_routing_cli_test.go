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

func TestTerraformCLIADEIntegrationRouting(t *testing.T) {
	var mu sync.Mutex
	var object *client.ADEInput
	creates, renews, patches := 0, 0, 0
	omitAssignment := false
	const token = "synthetic-routing-server-token"
	const endpoint = "/api/v1/integrations/apple/ade"
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
				t.Error("invalid ADE multipart")
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
			data, _ := io.ReadAll(f)
			_ = f.Close()
			if string(data) != token {
				t.Error("wrong routing token")
			}
			input := client.ADEInput{BlueprintID: req.FormValue("blueprint_id"), UseBlueprintRouting: req.FormValue("use_blueprint_routing") == "true", Phone: req.FormValue("phone"), Email: req.FormValue("email")}
			if input.Validate() != nil {
				t.Error("invalid ADE routing multipart assignment")
				w.WriteHeader(400)
				return
			}
			if strings.HasSuffix(req.URL.Path, "/renew") {
				renews++
			} else {
				creates++
			}
			object = &input
		case "PATCH":
			if object == nil {
				w.WriteHeader(404)
				return
			}
			var fields map[string]json.RawMessage
			if json.NewDecoder(req.Body).Decode(&fields) != nil {
				w.WriteHeader(400)
				return
			}
			data, _ := json.Marshal(fields)
			var input client.ADEInput
			if json.Unmarshal(data, &input) != nil || input.Validate() != nil || (input.UseBlueprintRouting && string(fields["blueprint_id"]) != "null") {
				t.Error("invalid ADE routing JSON assignment")
				w.WriteHeader(400)
				return
			}
			patches++
			object = &input
		case "DELETE":
			object = nil
			w.WriteHeader(204)
			return
		default:
			w.WriteHeader(405)
			return
		}
		body := map[string]any{"id": syntheticID, "defaults": map[string]string{"phone": object.Phone, "email": object.Email}, "server_name": "Synthetic routing server", "access_token_expiry": "2027-10-29T00:00:00Z"}
		if !omitAssignment {
			var blueprint any
			if !object.UseBlueprintRouting {
				blueprint = object.BlueprintID
			}
			body["blueprint_id"], body["use_blueprint_routing"] = blueprint, object.UseBlueprintRouting
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	config := func(routing bool, version int, includeToken bool) {
		assignment := " use_blueprint_routing = true\n"
		if !routing {
			assignment = fmt.Sprintf(" blueprint_id = %q\n use_blueprint_routing = false\n", syntheticNoteDevice)
		}
		secret := ""
		if includeToken {
			secret = fmt.Sprintf(" server_token = base64encode(%q)\n", token)
		}
		write(fmt.Sprintf("resource \"iru_ade_integration\" \"example\" {\n%s email = \"synthetic@example.invalid\"\n phone = \"1234567890\"\n server_token_version = %d\n%s}\n", assignment, version, secret))
	}
	config(true, 0, true)
	run(0, "validate", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	if strings.Contains(run(0, "show", "-json"), token) {
		t.Fatal("routing Apple token persisted in state")
	}
	run(0, "state", "rm", "iru_ade_integration.example")
	config(true, 0, false)
	run(0, "import", "-no-color", "iru_ade_integration.example", strings.ToUpper(syntheticID))
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	if creates != 1 || renews != 0 || patches != 0 {
		t.Error("routing import or no-op plan performed a write")
	}
	mu.Unlock()
	config(false, 0, false)
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	config(true, 0, false)
	run(0, "apply", "-auto-approve", "-no-color")
	config(true, 1, true)
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	if creates != 1 || renews != 1 || patches != 2 {
		t.Error("routing transition or renewal did not perform exactly one requested write")
	}
	omitAssignment = true
	mu.Unlock()
	run(1, "plan", "-no-color", "-detailed-exitcode")
	if !strings.Contains(run(0, "show", "-json"), syntheticID) {
		t.Fatal("unobservable routing refresh discarded integration identity")
	}
	mu.Lock()
	omitAssignment = false
	object.UseBlueprintRouting, object.BlueprintID = false, syntheticNoteDevice
	mu.Unlock()
	run(2, "plan", "-no-color", "-detailed-exitcode")
	config(true, 1, false)
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	run(1, "destroy", "-auto-approve", "-no-color")
	for _, blueprint := range []string{"", syntheticNoteDevice} {
		write(fmt.Sprintf("resource \"iru_ade_integration\" \"example\" {\n use_blueprint_routing = true\n blueprint_id = %q\n email = \"synthetic@example.invalid\"\n phone = \"1234567890\"\n}\n", blueprint))
		run(1, "validate", "-no-color")
	}
}

func TestTerraformCLIADEUnobservableCreateRetainsIdentity(t *testing.T) {
	var mu sync.Mutex
	creates := 0
	const token = "synthetic-partial-routing-token"
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if req.Method != "POST" || req.URL.Path != "/api/v1/integrations/apple/ade/" {
			w.WriteHeader(404)
			return
		}
		creates++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"`+syntheticID+`","use_blueprint_routing":true,"defaults":{"phone":"1234567890","email":"synthetic@example.invalid"},"server_name":"Synthetic server","access_token_expiry":"2027-10-29T00:00:00Z"}`)
	}))
	write(fmt.Sprintf("resource \"iru_ade_integration\" \"example\" {\n use_blueprint_routing = true\n email = \"synthetic@example.invalid\"\n phone = \"1234567890\"\n server_token = base64encode(%q)\n}\n", token))
	run(1, "apply", "-auto-approve", "-no-color")
	state := run(0, "show", "-json")
	if !strings.Contains(state, syntheticID) || strings.Contains(state, token) {
		t.Fatal("partial routing create lost identity or retained token")
	}
	mu.Lock()
	defer mu.Unlock()
	if creates != 1 {
		t.Fatal("ambiguous create was replayed")
	}
}
