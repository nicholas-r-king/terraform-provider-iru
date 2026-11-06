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
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// Upload acceptance and Library Item replacement are separate writes. A failed
// replacement must preserve recovery state without replaying either write;
// independent reads, not an error response, establish the remote outcome.
func TestTerraformCLIInstallerReplacementFailureReconciliation(t *testing.T) {
	for _, kind := range []string{"custom-app", "in-house-app"} {
		for _, failure := range []string{"rejected-503", "committed-503", "committed-disconnect", "committed-malformed"} {
			t.Run(kind+"/"+failure, func(t *testing.T) {
				var mu sync.Mutex
				endpoint, resourceType, extension := "/api/v1/library/custom-apps", "iru_custom_app", ".pkg"
				settings := "install_type = \"package\"\n"
				if kind == "in-house-app" {
					endpoint, resourceType, extension = "/api/v1/library/ipa-apps", "iru_in_house_app", ".ipa"
					settings = "runs_on_iphone = true\n"
				}
				filename := filepath.Join(t.TempDir(), "synthetic"+extension)
				putFile := func(contents string) {
					t.Helper()
					if os.WriteFile(filename, []byte(contents), 0600) != nil {
						t.Fatal("fixture write failed")
					}
				}
				putFile("synthetic installer A")
				presigns, uploads, patches := 0, 0, 0
				pendingKey, pendingHash := "", ""
				var pendingSize int64
				var remote map[string]any
				fail := false
				run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					defer mu.Unlock()
					w.Header().Set("Content-Type", "application/json")
					if r.URL.Path == "/" {
						if r.Method != http.MethodPost || r.Header.Get("Authorization") != "" {
							t.Error("unexpected S3 request or API credential forwarding")
							w.WriteHeader(403)
							return
						}
						if r.ParseMultipartForm(1024*1024) != nil {
							t.Error("invalid multipart")
							w.WriteHeader(400)
							return
						}
						defer r.MultipartForm.RemoveAll()
						file, _, err := r.FormFile("file")
						if err != nil {
							t.Error("missing upload file")
							w.WriteHeader(400)
							return
						}
						hash := sha256.New()
						pendingSize, err = io.Copy(hash, file)
						file.Close()
						if err != nil {
							t.Error("upload read failed")
						}
						pendingHash = hex.EncodeToString(hash.Sum(nil))
						uploads++
						w.WriteHeader(204)
						return
					}
					if r.Header.Get("Authorization") != "Bearer "+cliToken {
						t.Error("missing API token")
						w.WriteHeader(401)
						return
					}
					if r.URL.Path == endpoint+"/upload" && r.Method == http.MethodPost {
						presigns++
						pendingKey = fmt.Sprintf("synthetic/installers/version-%d%s", presigns, extension)
						w.WriteHeader(201)
						json.NewEncoder(w).Encode(map[string]any{"id": syntheticID, "expires": time.Now().Add(time.Hour).Format(time.RFC3339), "file_key": pendingKey, "post_url": "https://synthetic-installer.s3.amazonaws.com/", "post_data": map[string]string{"key": pendingKey, "x-amz-algorithm": "AWS4-HMAC-SHA256", "x-amz-credential": "synthetic-upload-credential", "x-amz-date": "20261106T000000Z", "x-amz-security-token": "synthetic-upload-secret", "policy": "synthetic-upload-policy", "x-amz-signature": "synthetic-upload-signature"}})
						return
					}
					if r.URL.Path == endpoint+"/upload/"+syntheticID+"/status" && r.Method == http.MethodGet {
						json.NewEncoder(w).Encode(map[string]any{"status": "VALIDATED", "sha256": pendingHash, "metadata": map[string]any{"file_key": pendingKey, "file_size": pendingSize}})
						return
					}
					expectedPath := endpoint + "/" + syntheticID
					if r.Method == http.MethodPost {
						expectedPath = endpoint
					}
					if r.URL.Path != expectedPath {
						t.Error("unexpected endpoint")
						w.WriteHeader(404)
						return
					}
					switch r.Method {
					case http.MethodGet:
						if remote == nil {
							w.WriteHeader(404)
							return
						}
					case http.MethodPost, http.MethodPatch:
						if r.Method == http.MethodPatch {
							patches++
							if !fail {
								t.Error("unexpected replacement outside failure phase")
							}
							if failure == "rejected-503" {
								w.WriteHeader(503)
								fmt.Fprint(w, "synthetic-private-update-response")
								return
							}
						}
						fields := map[string]any{}
						if kind == "custom-app" {
							if r.ParseForm() != nil {
								t.Error("invalid app form")
								w.WriteHeader(400)
								return
							}
							for key, values := range r.Form {
								if len(values) != 1 {
									t.Error("invalid form cardinality")
									continue
								}
								switch key {
								case "active", "restart", "show_in_self_service", "self_service_recommended":
									fields[key] = values[0] == "true"
								default:
									fields[key] = values[0]
								}
							}
						} else if json.NewDecoder(r.Body).Decode(&fields) != nil {
							t.Error("invalid IPA request")
							w.WriteHeader(400)
							return
						}
						if remote == nil {
							remote = map[string]any{"id": syntheticID, "name": "Synthetic replacement", "active": false}
							if kind == "custom-app" {
								for key, value := range map[string]any{"install_type": "package", "install_enforcement": "install_once", "audit_script": "", "preinstall_script": "", "postinstall_script": "", "unzip_location": "", "restart": false, "show_in_self_service": false, "self_service_recommended": false} {
									remote[key] = value
								}
							} else {
								remote["runs_on_iphone"], remote["runs_on_ipad"], remote["runs_on_tv"] = true, false, false
							}
						}
						if fields["file_key"] != pendingKey {
							t.Error("replacement did not reference the accepted upload")
						}
						for key, value := range fields {
							remote[key] = value
						}
						remote["sha256"], remote["file_size"] = pendingHash, pendingSize
						if r.Method == http.MethodPatch {
							switch failure {
							case "committed-503":
								w.WriteHeader(503)
								fmt.Fprint(w, "synthetic-private-update-response")
								return
							case "committed-disconnect":
								connection, _, err := w.(http.Hijacker).Hijack()
								if err != nil {
									t.Error("fixture disconnect failed")
									return
								}
								connection.Close()
								return
							case "committed-malformed":
								fmt.Fprint(w, "synthetic-private-malformed-response")
								return
							}
						} else {
							w.WriteHeader(201)
						}
					case http.MethodDelete:
						remote = nil
						w.WriteHeader(204)
						return
					default:
						t.Error("unexpected method")
						w.WriteHeader(405)
						return
					}
					json.NewEncoder(w).Encode(remote)
				}))
				write(fmt.Sprintf("resource %q \"example\" {\n name = \"Synthetic replacement\"\n active = false\n%s source_file = %q\n source_sha256 = filesha256(%q)\n}\n", resourceType, settings, filepath.ToSlash(filename), filepath.ToSlash(filename)))
				privateValues := []string{"synthetic-private-update-response", "synthetic-private-malformed-response", "synthetic-upload-credential", "synthetic-upload-secret", "synthetic-upload-signature", "synthetic-upload-policy"}
				assertPrivate := func(text string) {
					t.Helper()
					for _, private := range privateValues {
						if strings.Contains(text, private) {
							t.Fatal("private upload/update response leaked")
						}
					}
				}
				state := func() map[string]any {
					t.Helper()
					raw := run(0, "state", "pull")
					assertPrivate(raw)
					var value map[string]any
					if json.Unmarshal([]byte(raw), &value) != nil {
						t.Fatal("invalid state")
					}
					return value
				}
				attributes := func(value map[string]any) map[string]any {
					t.Helper()
					resources, ok := value["resources"].([]any)
					if !ok || len(resources) != 1 {
						t.Fatal("resource recovery locator missing")
					}
					instances := resources[0].(map[string]any)["instances"].([]any)
					if len(instances) != 1 {
						t.Fatal("ambiguous resource instance")
					}
					return instances[0].(map[string]any)["attributes"].(map[string]any)
				}
				run(0, "apply", "-auto-approve", "-no-color")
				before := state()
				oldKey, oldHash := attributes(before)["file_key"], attributes(before)["source_sha256"]
				putFile("synthetic installer B")
				mu.Lock()
				fail = true
				mu.Unlock()
				output := run(1, "apply", "-auto-approve", "-no-color")
				assertPrivate(output)
				after := state()
				if !reflect.DeepEqual(canonicalDeleteState(t, before["resources"]), canonicalDeleteState(t, after["resources"])) {
					t.Fatal("failed replacement discarded or changed prior resource state")
				}
				mu.Lock()
				if uploads != 2 || presigns != 2 || patches != 1 {
					t.Error("failed replacement automatically replayed an upload or PATCH")
				}
				committed := failure != "rejected-503"
				if (remote["file_key"] != oldKey) != committed {
					t.Error("fixture outcome did not match failure phase")
				}
				replacementKey, replacementHash := pendingKey, pendingHash
				mu.Unlock()
				// Read-only reconciliation must neither re-upload nor replay the PATCH.
				run(0, "apply", "-refresh-only", "-auto-approve", "-no-color")
				observed := attributes(state())
				expectedKey, expectedHash := oldKey, oldHash
				if committed {
					expectedKey, expectedHash = replacementKey, replacementHash
				}
				if observed["id"] != syntheticID || observed["file_key"] != expectedKey || observed["source_sha256"] != expectedHash {
					t.Fatal("independent refresh did not preserve locator and observe the actual installer")
				}
				mu.Lock()
				if uploads != 2 || presigns != 2 || patches != 1 {
					t.Error("read reconciliation replayed a write")
				}
				mu.Unlock()
				if committed {
					run(0, "plan", "-no-color", "-detailed-exitcode")
					run(0, "apply", "-auto-approve", "-no-color")
					mu.Lock()
					if uploads != 2 || presigns != 2 || patches != 1 {
						t.Error("confirmed replacement re-uploaded or replayed on no-op")
					}
					mu.Unlock()
				} else {
					run(2, "plan", "-no-color", "-detailed-exitcode")
				}
				run(0, "destroy", "-auto-approve", "-no-color")
				if strings.TrimSpace(run(0, "state", "list")) != "" {
					t.Fatal("synthetic cleanup retained a resource")
				}
			})
		}
	}
}
