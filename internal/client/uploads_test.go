// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func uploadFixture() uploadTicket {
	return uploadTicket{ID: testID, FileKey: "tenants/synthetic/library/ipa_apps/example.ipa", PostURL: "https://synthetic-bucket.s3.us-west-2.amazonaws.com/", Expires: time.Now().Add(time.Hour).Format(time.RFC3339), PostData: map[string]string{
		"key": "tenants/synthetic/library/ipa_apps/example.ipa", "x-amz-algorithm": "AWS4-HMAC-SHA256", "x-amz-credential": "synthetic-aws-credential", "x-amz-date": "20261029T000000Z", "x-amz-security-token": "synthetic-security-token", "policy": "synthetic-policy", "x-amz-signature": "synthetic-signature",
	}}
}
func localInstaller(t *testing.T, content string) (string, string) {
	t.Helper()
	filename := filepath.Join(t.TempDir(), "synthetic-private-installer.pkg")
	if os.WriteFile(filename, []byte(content), 0600) != nil {
		t.Fatal("could not prepare fixture")
	}
	sum := sha256.Sum256([]byte(content))
	return filename, hex.EncodeToString(sum[:])
}
func ticketJSON(t *testing.T, ticket uploadTicket) string {
	t.Helper()
	data, err := json.Marshal(ticket)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func consumeS3(t *testing.T, req *http.Request, ticket uploadTicket, expectedSHA string, expectedSize int64) {
	t.Helper()
	if req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != "" || req.GetBody != nil {
		t.Fatal("Iru credential or replayable buffered body reached S3")
	}
	if req.URL.String() != ticket.PostURL || req.Method != "POST" {
		t.Fatal("wrong S3 destination or method")
	}
	media, params, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if err != nil || media != "multipart/form-data" {
		t.Fatal("S3 requires multipart")
	}
	reader := multipart.NewReader(req.Body, params["boundary"])
	fields := map[string]string{}
	var length int64
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if part.FormName() == "file" {
			if part.FileName() != "synthetic-private-installer.pkg" {
				t.Fatal("wrong installer filename")
			}
			h := sha256.New()
			n, err := io.Copy(h, part)
			if err != nil || n != expectedSize || hex.EncodeToString(h.Sum(nil)) != expectedSHA {
				t.Fatal("streamed bytes changed")
			}
			length = n
		} else {
			data, err := io.ReadAll(part)
			if err != nil {
				t.Fatal(err)
			}
			fields[part.FormName()] = string(data)
		}
	}
	if len(fields) != len(ticket.PostData) || length != expectedSize || req.ContentLength <= length {
		t.Fatal("incomplete multipart upload")
	}
	for key, value := range ticket.PostData {
		if fields[key] != value {
			t.Fatal("lost signed S3 form field")
		}
	}
}

func TestCustomInstallerStreamingAndOriginSeparation(t *testing.T) {
	content := strings.Repeat("synthetic-installer-bytes\x00", 150000)
	filename, sum := localInstaller(t, content)
	ticket := uploadFixture()
	requests := []string{}
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req.URL.Host+req.URL.Path)
		if req.URL.Host == "synthetic.api.iru.com" {
			if req.Method != "POST" || req.URL.Path != appsPath+"/upload" || req.Header.Get("Authorization") != "Bearer "+testToken {
				t.Fatal("wrong presign request")
			}
			var fields map[string]string
			if json.NewDecoder(req.Body).Decode(&fields) != nil || len(fields) != 1 || fields["name"] != filepath.Base(filename) {
				t.Fatal("wrong custom-app upload name field")
			}
			return response(201, ticketJSON(t, ticket)), nil
		}
		consumeS3(t, req, ticket, sum, int64(len(content)))
		return response(204, ""), nil
	})
	upload, err := c.UploadInstaller(context.Background(), "custom-app", filename, strings.ToUpper(sum))
	if err != nil || upload.FileKey != ticket.FileKey || upload.SHA256 != sum || upload.FileSize != int64(len(content)) || len(requests) != 2 {
		t.Fatalf("upload failed: %v", err)
	}
	if strings.Contains(fmt.Sprintf("%v %#v %v %#v", upload, upload, ticket, ticket), "synthetic-security-token") {
		t.Fatal("upload credentials exposed by formatting")
	}
}

func TestIPAUploadWaitsForValidatedMatchingMetadata(t *testing.T) {
	filename, sum := localInstaller(t, "synthetic IPA")
	ticket := uploadFixture()
	polls := 0
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "synthetic.api.iru.com" {
			consumeS3(t, req, ticket, sum, 13)
			return response(204, ""), nil
		}
		if req.Method == "POST" {
			var body map[string]string
			_ = json.NewDecoder(req.Body).Decode(&body)
			if body["filename"] != filepath.Base(filename) || len(body) != 1 || req.URL.Path != inHouseAppsPath+"/upload" {
				t.Fatal("wrong IPA presign fields")
			}
			return response(201, ticketJSON(t, ticket)), nil
		}
		if req.Method != "GET" || req.URL.Path != inHouseAppsPath+"/upload/"+testID+"/status" {
			t.Fatal("wrong IPA status endpoint")
		}
		polls++
		if polls < 3 {
			return response(200, `{"status":"UPLOADED","reason":"synthetic-private-status"}`), nil
		}
		return response(200, `{"status":"VALIDATED","sha256":"`+sum+`","metadata":{"file_key":"`+ticket.FileKey+`","file_size":13}}`), nil
	})
	if _, err := c.UploadInstaller(context.Background(), "in-house-app", filename, sum); err != nil || polls != 3 {
		t.Fatalf("IPA did not validate: %v", err)
	}
}

func TestInstallerValidationStopsBeforeAnyNetworkWrite(t *testing.T) {
	filename, sum := localInstaller(t, "synthetic installer")
	c := testClient(t, func(*http.Request) (*http.Response, error) { t.Fatal("invalid input reached network"); return nil, nil })
	for _, input := range [][3]string{{"unknown", filename, sum}, {"custom-app", filename, "invalid"}, {"custom-app", filename, strings.Repeat("0", 64)}, {"custom-app", filepath.Dir(filename), sum}, {"custom-app", filename + "-missing", sum}} {
		if _, err := c.UploadInstaller(context.Background(), input[0], input[1], input[2]); err == nil {
			t.Fatal("invalid upload input accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.UploadInstaller(ctx, "custom-app", filename, sum); err == nil {
		t.Fatal("cancelled upload accepted")
	}
}

func TestUnsafeOrExpiredUploadTicketsNeverReachS3(t *testing.T) {
	filename, sum := localInstaller(t, "synthetic installer")
	changes := []func(*uploadTicket){func(x *uploadTicket) { x.PostURL = "http://synthetic-bucket.s3.amazonaws.com/" }, func(x *uploadTicket) { x.PostURL = "https://evil.example/" }, func(x *uploadTicket) { x.PostURL = "https://synthetic-bucket.s3.amazonaws.com.evil.example/" }, func(x *uploadTicket) { x.PostURL = "https://user:secret@synthetic-bucket.s3.amazonaws.com/" }, func(x *uploadTicket) { x.PostURL = "https://synthetic-bucket.s3.amazonaws.com:443/" }, func(x *uploadTicket) { x.PostURL = "https://synthetic-bucket.s3.amazonaws.com/?secret=value" }, func(x *uploadTicket) { x.Expires = time.Now().Add(-time.Hour).Format(time.RFC3339) }, func(x *uploadTicket) { x.PostData["key"] = "different" }, func(x *uploadTicket) { x.PostData["unexpected"] = "secret" }, func(x *uploadTicket) { delete(x.PostData, "x-amz-security-token") }, func(x *uploadTicket) { x.PostData["x-amz-algorithm"] = "unknown" }}
	for _, change := range changes {
		ticket := uploadFixture()
		change(&ticket)
		calls := 0
		c := testClient(t, func(req *http.Request) (*http.Response, error) {
			calls++
			if req.URL.Host != "synthetic.api.iru.com" {
				t.Fatal("unsafe ticket reached S3")
			}
			return response(201, ticketJSON(t, ticket)), nil
		})
		if _, err := c.UploadInstaller(context.Background(), "custom-app", filename, sum); err == nil || calls != 1 {
			t.Fatal("invalid ticket accepted or retried")
		}
	}
}

func TestUploadFailureNeverReplaysOrLeaksSignedValues(t *testing.T) {
	filename, sum := localInstaller(t, "synthetic installer")
	ticket := uploadFixture()
	for _, status := range []int{301, 307, 400, 403, 429, 503} {
		s3Calls := 0
		c := testClient(t, func(req *http.Request) (*http.Response, error) {
			if req.URL.Host == "synthetic.api.iru.com" {
				return response(201, ticketJSON(t, ticket)), nil
			}
			s3Calls++
			_ = req.Body.Close()
			r := response(status, "synthetic-private-error")
			r.Header.Set("Location", "https://evil.example/")
			return r, nil
		})
		_, err := c.UploadInstaller(context.Background(), "custom-app", filename, sum)
		if err == nil || s3Calls != 1 || strings.Contains(err.Error(), "synthetic-private") || strings.Contains(err.Error(), "synthetic-security-token") {
			t.Fatal("unsafe failed upload handling")
		}
	}
	calls := 0
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "synthetic.api.iru.com" {
			return response(201, ticketJSON(t, ticket)), nil
		}
		calls++
		return nil, errors.New("synthetic-security-token transport error")
	})
	if _, err := c.UploadInstaller(context.Background(), "custom-app", filename, sum); err == nil || calls != 1 || strings.Contains(err.Error(), "synthetic-security-token") {
		t.Fatal("ambiguous upload retried or leaked details")
	}
}

func TestInstallerMutationDuringUploadCannotProduceSuccess(t *testing.T) {
	filename, sum := localInstaller(t, "synthetic installer")
	ticket := uploadFixture()
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "synthetic.api.iru.com" {
			if err := os.WriteFile(filename, []byte("synthetic different"), 0600); err != nil {
				t.Fatal(err)
			}
			return response(201, ticketJSON(t, ticket)), nil
		}
		_, _ = io.Copy(io.Discard, req.Body)
		return response(204, ""), nil
	})
	if _, err := c.UploadInstaller(context.Background(), "custom-app", filename, sum); err == nil {
		t.Fatal("installer substitution accepted")
	}
}

func TestIPAValidationFailureIsBoundedAndRedacted(t *testing.T) {
	for _, body := range []string{`{"status":"UPLOAD_FAILED","reason":"synthetic-private-reason"}`, `{"status":"VALIDATE_FAILED","reason":"synthetic-private-reason"}`, `{"status":"UNKNOWN-PRIVATE"}`, `{"status":"UPLOADING"}`, `{"status":"VALIDATED","sha256":"wrong","metadata":{}}`} {
		calls := 0
		c := testClient(t, func(*http.Request) (*http.Response, error) { calls++; return response(200, body), nil })
		err := c.waitForIPA(context.Background(), InstallerUpload{PendingID: testID, FileKey: "synthetic", SHA256: strings.Repeat("a", 64), FileSize: 1})
		if err == nil || calls > 60 || strings.Contains(err.Error(), "synthetic-private") || strings.Contains(err.Error(), "UNKNOWN-PRIVATE") {
			t.Fatal("unbounded or unsafe IPA validation")
		}
	}
}
