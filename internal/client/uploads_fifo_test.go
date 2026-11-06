// SPDX-License-Identifier: MPL-2.0
//go:build darwin || linux

package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A subprocess makes the pre-fix blocking open reproducible without leaving a
// stuck goroutine/FIFO opener behind. Its environment contains no real secrets.
func TestInstallerFIFOIsRejectedWithoutBlocking(t *testing.T) {
	if os.Getenv("IRU_FIFO_TEST_HELPER") == "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		if os.Getenv("IRU_FIFO_TEST_CANCELED") == "true" {
			cancel()
		}
		calls := 0
		c := testClient(t, func(*http.Request) (*http.Response, error) {
			calls++
			return nil, errors.New("unexpected synthetic transport")
		})
		filename := os.Getenv("IRU_FIFO_TEST_SOURCE")
		_, err := c.UploadInstaller(ctx, "custom-app", filename, strings.Repeat("a", 64))
		if err == nil || calls != 0 || strings.Contains(err.Error(), filename) {
			t.Fatal("special installer source was accepted, reached transport or leaked its path")
		}
		return
	}
	for _, symlink := range []bool{false, true} {
		for _, canceled := range []bool{false, true} {
			name := "fifo"
			if symlink {
				name += "-symlink"
			}
			if canceled {
				name += "-canceled"
			}
			t.Run(name, func(t *testing.T) {
				directory := t.TempDir()
				filename := filepath.Join(directory, "synthetic-private-source.pkg")
				if err := syscall.Mkfifo(filename, 0600); err != nil {
					t.Fatal("could not prepare synthetic FIFO")
				}
				if symlink {
					alias := filepath.Join(directory, "synthetic-alias.pkg")
					if err := os.Symlink(filename, alias); err != nil {
						t.Fatal("could not prepare synthetic FIFO symlink")
					}
					filename = alias
				}
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestInstallerFIFOIsRejectedWithoutBlocking$")
				// Race-instrumented binaries sleep for one second at exit by
				// default, independently of the source-open operation. Disable
				// only that delay in this child; keep the liveness bound, race
				// detection, report destination and failure exit code unchanged.
				command.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + directory, "TMPDIR=" + directory,
					"GORACE=atexit_sleep_ms=0",
					"IRU_FIFO_TEST_HELPER=1", "IRU_FIFO_TEST_SOURCE=" + filename,
					"IRU_FIFO_TEST_CANCELED=" + map[bool]string{true: "true", false: "false"}[canceled]}
				_, err := command.CombinedOutput()
				if ctx.Err() != nil {
					t.Fatal("installer source open blocked past cancellation; child terminated")
				}
				if err != nil {
					t.Fatal("isolated special-source rejection check failed")
				}
			})
		}
	}
}

func TestInstallerRegularSymlinkAndDescriptorSurvivePathReplacement(t *testing.T) {
	for _, kind := range []string{"custom-app", "in-house-app"} {
		t.Run(kind, func(t *testing.T) {
			content := "synthetic original installer"
			original, sum := localInstaller(t, content)
			replacement, _ := localInstaller(t, "synthetic different bytes")
			alias := filepath.Join(t.TempDir(), filepath.Base(original))
			if os.Symlink(original, alias) != nil {
				t.Fatal("could not prepare regular-file alias")
			}
			ticket := uploadFixture()
			posts, streams, polls := 0, 0, 0
			c := testClient(t, func(req *http.Request) (*http.Response, error) {
				if req.URL.Host != "synthetic.api.iru.com" {
					streams++
					consumeS3(t, req, ticket, sum, int64(len(content)))
					return response(204, ""), nil
				}
				if req.Method == http.MethodPost {
					posts++
					if os.Remove(alias) != nil || os.Symlink(replacement, alias) != nil {
						t.Fatal("could not replace alias after initial hash")
					}
					return response(201, ticketJSON(t, ticket)), nil
				}
				polls++
				body, _ := json.Marshal(map[string]any{"status": "VALIDATED", "sha256": sum,
					"metadata": map[string]any{"file_key": ticket.FileKey, "file_size": len(content)}})
				return response(200, string(body)), nil
			})
			upload, err := c.UploadInstaller(context.Background(), kind, alias, sum)
			wantPolls := 0
			if kind == "in-house-app" {
				wantPolls = 1
			}
			if err != nil || posts != 1 || streams != 1 || polls != wantPolls || upload.SHA256 != sum || upload.FileSize != int64(len(content)) {
				t.Fatal("regular-file alias behavior or original descriptor changed")
			}
		})
	}
}
