// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

func TestTerraformCLIInstallerUploads(t *testing.T) {
	for _, kind := range []string{"custom-app", "in-house-app"} {
		t.Run(kind, func(t *testing.T) {
			var mu sync.Mutex
			uploads, presigns, replacements := 0, 0, 0
			pendingKey, pendingHash := "", ""
			var pendingSize int64
			var app *client.App
			var ipa *client.InHouseApp
			endpoint, resourceType, extension := "/api/v1/library/custom-apps", "iru_custom_app", ".pkg"
			if kind == "in-house-app" {
				endpoint, resourceType, extension = "/api/v1/library/ipa-apps", "iru_in_house_app", ".ipa"
			}
			filename := filepath.Join(t.TempDir(), "synthetic-installer"+extension)
			if os.WriteFile(filename, []byte("synthetic installer A"), 0600) != nil {
				t.Fatal("fixture write failed")
			}
			run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				if req.URL.Path == "/" {
					if req.Method != "POST" || req.Header.Get("Authorization") != "" {
						t.Error("API bearer token reached S3")
						w.WriteHeader(403)
						return
					}
					if req.ParseMultipartForm(1024*1024) != nil {
						t.Error("invalid multipart")
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
					h := sha256.New()
					pendingSize, err = io.Copy(h, file)
					_ = file.Close()
					if err != nil {
						t.Error(err)
					}
					pendingHash = hex.EncodeToString(h.Sum(nil))
					uploads++
					w.WriteHeader(204)
					return
				}
				if req.Header.Get("Authorization") != "Bearer "+cliToken {
					t.Error("missing API token")
					w.WriteHeader(401)
					return
				}
				if req.URL.Path == endpoint+"/upload" {
					presigns++
					pendingKey = fmt.Sprintf("tenants/synthetic/library/installers/version-%d%s", presigns, extension)
					w.WriteHeader(201)
					_ = json.NewEncoder(w).Encode(map[string]any{"id": syntheticID, "expires": time.Now().Add(time.Hour).Format(time.RFC3339), "file_key": pendingKey, "post_url": "https://synthetic-installer.s3.amazonaws.com/", "post_data": map[string]string{"key": pendingKey, "x-amz-algorithm": "AWS4-HMAC-SHA256", "x-amz-credential": "synthetic-credential", "x-amz-date": "20261029T000000Z", "x-amz-security-token": "synthetic-ticket-secret", "policy": "synthetic-policy", "x-amz-signature": "synthetic-signature"}})
					return
				}
				if req.URL.Path == endpoint+"/upload/"+syntheticID+"/status" {
					_ = json.NewEncoder(w).Encode(map[string]any{"status": "VALIDATED", "sha256": pendingHash, "metadata": map[string]any{"file_key": pendingKey, "file_size": pendingSize}})
					return
				}
				if req.URL.Path != endpoint && req.URL.Path != endpoint+"/"+syntheticID {
					t.Error("unexpected installer endpoint")
					w.WriteHeader(404)
					return
				}
				if req.Method == "DELETE" {
					app, ipa = nil, nil
					w.WriteHeader(204)
					return
				}
				if req.Method == "GET" {
					if (kind == "custom-app" && app == nil) || (kind == "in-house-app" && ipa == nil) {
						w.WriteHeader(404)
						return
					}
				} else if req.Method == "POST" || req.Method == "PATCH" {
					key := ""
					if app != nil {
						key = app.FileKey
					}
					if ipa != nil {
						key = ipa.FileKey
					}
					if kind == "custom-app" {
						if req.ParseForm() != nil {
							t.Error("invalid custom-app form")
							w.WriteHeader(400)
							return
						}
						if value, exists := req.Form["file_key"]; exists {
							key = value[0]
							if req.Method == "PATCH" {
								replacements++
							}
						}
						input := client.AppInput{Name: req.Form.Get("name"), FileKey: key, InstallType: req.Form.Get("install_type"), InstallEnforcement: req.Form.Get("install_enforcement"), Active: req.Form.Get("active") == "true"}
						if input.Validate() != nil {
							t.Error("invalid custom-app metadata")
							w.WriteHeader(400)
							return
						}
						sum, size := pendingHash, pendingSize
						app = &client.App{ID: syntheticID, AppInput: input, SHA256: &sum, FileSize: &size}
					} else {
						var fields map[string]json.RawMessage
						if json.NewDecoder(req.Body).Decode(&fields) != nil {
							w.WriteHeader(400)
							return
						}
						body, _ := json.Marshal(fields)
						var input client.InHouseAppInput
						_ = json.Unmarshal(body, &input)
						if _, exists := fields["file_key"]; exists {
							key = input.FileKey
							if req.Method == "PATCH" {
								replacements++
							}
						}
						input.FileKey = key
						sum, size := pendingHash, pendingSize
						ipa = &client.InHouseApp{ID: syntheticID, InHouseAppInput: input, SHA256: &sum, FileSize: &size}
					}
					if req.Method == "POST" {
						w.WriteHeader(201)
					}
				} else {
					w.WriteHeader(405)
					return
				}
				if kind == "custom-app" {
					_ = json.NewEncoder(w).Encode(app)
				} else {
					_ = json.NewEncoder(w).Encode(ipa)
				}
			}))
			name := "Synthetic local installer"
			upperHash := false
			config := func(local bool) {
				artifact := ""
				if local {
					hash := fmt.Sprintf("filesha256(%q)", filepath.ToSlash(filename))
					if upperHash {
						hash = "upper(" + hash + ")"
					}
					artifact = fmt.Sprintf(" source_file = %q\n source_sha256 = %s\n", filepath.ToSlash(filename), hash)
				}
				settings := " install_type = \"package\"\n"
				if kind == "in-house-app" {
					settings = " runs_on_iphone = true\n"
				}
				write(fmt.Sprintf("resource %q \"example\" {\n name = %q\n%s%s}\n", resourceType, name, settings, artifact))
			}
			config(true)
			run(0, "validate", "-no-color")
			run(0, "apply", "-auto-approve", "-no-color")
			run(0, "plan", "-no-color", "-detailed-exitcode")
			name = "Synthetic renamed"
			config(true)
			run(0, "apply", "-auto-approve", "-no-color")
			run(0, "plan", "-no-color", "-detailed-exitcode")
			mu.Lock()
			if uploads != 1 || replacements != 0 {
				t.Error("metadata update uploaded or replaced installer")
			}
			mu.Unlock()
			if os.WriteFile(filename, []byte("synthetic installer B"), 0600) != nil {
				t.Fatal("fixture write failed")
			}
			run(0, "plan", "-no-color", "-out=stale.plan")
			if os.WriteFile(filename, []byte("synthetic installer C"), 0600) != nil {
				t.Fatal("fixture write failed")
			}
			run(1, "apply", "-no-color", "stale.plan")
			mu.Lock()
			if presigns != 1 {
				t.Error("stale planned checksum reached upload API")
			}
			mu.Unlock()
			run(0, "apply", "-auto-approve", "-no-color")
			run(0, "plan", "-no-color", "-detailed-exitcode")
			mu.Lock()
			if uploads != 2 || replacements != 1 {
				t.Error("content change did not replace exactly once")
			}
			mu.Unlock()
			upperHash = true
			config(true)
			run(0, "apply", "-auto-approve", "-no-color")
			run(0, "plan", "-no-color", "-detailed-exitcode")
			mu.Lock()
			if uploads != 2 {
				t.Error("hash spelling caused re-upload")
			}
			if app != nil {
				app.FileKey = "synthetic-external-drift.pkg"
				different := strings.Repeat("a", 64)
				app.SHA256 = &different
			} else {
				ipa.FileKey = "synthetic-external-drift.ipa"
				different := strings.Repeat("a", 64)
				ipa.SHA256 = &different
			}
			mu.Unlock()
			run(2, "plan", "-no-color", "-detailed-exitcode")
			run(0, "apply", "-auto-approve", "-no-color")
			run(0, "plan", "-no-color", "-detailed-exitcode")
			mu.Lock()
			if uploads != 3 {
				t.Error("remote content drift was not repaired")
			}
			mu.Unlock()
			run(0, "state", "rm", resourceType+".example")
			config(false)
			run(0, "import", "-no-color", resourceType+".example", syntheticID)
			run(0, "plan", "-no-color", "-detailed-exitcode")
			state := run(0, "show", "-json")
			if strings.Contains(state, "synthetic-ticket-secret") {
				t.Fatal("signed upload credential persisted")
			}
			run(0, "destroy", "-auto-approve", "-no-color")
		})
	}
}
