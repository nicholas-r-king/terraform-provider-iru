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

func TestTerraformCLIDirectoryUser(t *testing.T) {
	var mu sync.Mutex
	present := true
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if req.Method != "GET" || req.URL.Path != "/api/v1/users/"+syntheticCategory {
			t.Error("directory user lookup made a mutation or wrong request")
			w.WriteHeader(403)
			return
		}
		if !present {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": syntheticCategory, "name": "Synthetic Person", "email": "synthetic@example.invalid", "active": false, "archived": true, "department": "synthetic-unrelated-private-data"})
	}))
	write(fmt.Sprintf("data \"iru_user\" \"example\" {\n user_id = %q\n}\n", strings.ToUpper(syntheticCategory)))
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	state := run(0, "show", "-json")
	if strings.Contains(state, "synthetic-unrelated") || !strings.Contains(state, "synthetic@example.invalid") {
		t.Fatal("directory user state projection was incorrect")
	}
	mu.Lock()
	present = false
	mu.Unlock()
	run(1, "plan", "-no-color")
}
