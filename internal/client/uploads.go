// SPDX-License-Identifier: MPL-2.0

package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
)

const maxInstallerBytes int64 = 5 * 1024 * 1024 * 1024

var s3UploadHost = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*\.s3(?:[.-][a-z0-9-]+)?\.amazonaws\.com$`)

// InstallerUpload contains only durable upload identity/checksum information.
// Signed URLs, S3 credentials, policies and API response bodies are discarded.
type InstallerUpload struct {
	FileKey   string
	SHA256    string
	FileSize  int64
	PendingID string
}

func (InstallerUpload) String() string   { return "Iru installer upload (private metadata withheld)" }
func (InstallerUpload) GoString() string { return "Iru installer upload (private metadata withheld)" }

type uploadTicket struct {
	ID       string            `json:"id"`
	FileKey  string            `json:"file_key"`
	PostURL  string            `json:"post_url"`
	PostData map[string]string `json:"post_data"`
	Expires  string            `json:"expires"`
}

func (uploadTicket) String() string   { return "Iru upload ticket (credentials redacted)" }
func (uploadTicket) GoString() string { return "Iru upload ticket (credentials redacted)" }

func ValidInstallerSHA256(value string) bool { return sha256Pattern.MatchString(value) }

func validateUploadTicket(ticket uploadTicket, ipa bool, now time.Time) error {
	u, err := url.Parse(ticket.PostURL)
	if err != nil || u.Scheme != "https" || !s3UploadHost.MatchString(u.Host) || u.User != nil || u.Opaque != "" ||
		u.RawQuery != "" || u.ForceQuery || strings.Contains(ticket.PostURL, "#") || u.Path != "/" || u.EscapedPath() != "/" {
		return errors.New("Iru returned an unsupported S3 upload destination (details withheld)")
	}
	expires, err := time.Parse(time.RFC3339Nano, ticket.Expires)
	if err != nil || !expires.After(now) {
		return errors.New("Iru returned an expired or invalid upload ticket")
	}
	if strings.TrimSpace(ticket.FileKey) == "" || strings.IndexFunc(ticket.FileKey, unicode.IsControl) >= 0 || ticket.PostData["key"] != ticket.FileKey {
		return errors.New("Iru returned inconsistent upload identity metadata")
	}
	if ipa && !ValidID(ticket.ID) {
		return errors.New("Iru omitted the IPA pending upload UUID")
	}
	allowed := map[string]bool{"key": true, "x-amz-algorithm": true, "x-amz-credential": true, "x-amz-date": true, "x-amz-security-token": true, "policy": true, "x-amz-signature": true}
	if len(ticket.PostData) != len(allowed) {
		return errors.New("Iru returned an unsupported S3 upload form (values withheld)")
	}
	for key, value := range ticket.PostData {
		if !allowed[key] || value == "" || len(value) > 64*1024 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return errors.New("Iru returned an invalid S3 upload form (values withheld)")
		}
	}
	if ticket.PostData["x-amz-algorithm"] != "AWS4-HMAC-SHA256" {
		return errors.New("Iru returned an unsupported S3 signing algorithm")
	}
	return nil
}

// UploadInstaller implements the documented two-origin upload workflow. The
// caller must supply an exact planned checksum. Only the Iru request receives
// the bearer token; S3 gets a separate client with no Authorization or cookie jar.
// Files are streamed, never loaded into a multipart buffer or copied to state.
func (c *Client) UploadInstaller(ctx context.Context, kind, filename, expectedSHA string) (InstallerUpload, error) {
	if (kind != "custom-app" && kind != "in-house-app") || !ValidInstallerSHA256(expectedSHA) {
		return InstallerUpload{}, errors.New("installer upload requires a supported kind and exact SHA256")
	}
	if filename == "" || strings.IndexFunc(filename, unicode.IsControl) >= 0 {
		return InstallerUpload{}, errors.New("installer source_file must be a local file path")
	}
	file, err := os.Open(filename)
	if err != nil {
		return InstallerUpload{}, errors.New("could not open installer source_file (path withheld)")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxInstallerBytes {
		return InstallerUpload{}, errors.New("installer must be a nonempty regular file of at most 5 GiB")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	hash := sha256.New()
	if _, err := io.Copy(hash, &contextReader{ctx: ctx, reader: io.LimitReader(file, maxInstallerBytes+1)}); err != nil {
		return InstallerUpload{}, errors.New("could not hash installer source_file (details withheld)")
	}
	expectedSHA = strings.ToLower(expectedSHA)
	if hex.EncodeToString(hash.Sum(nil)) != expectedSHA {
		return InstallerUpload{}, errors.New("installer contents differ from the planned source_sha256; no upload was started")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return InstallerUpload{}, errors.New("could not rewind installer source_file")
	}
	endpoint, field := appsPath+"/upload", "name"
	if kind == "in-house-app" {
		endpoint, field = inHouseAppsPath+"/upload", "filename"
	}
	data, err := c.request(ctx, http.MethodPost, endpoint, map[string]string{field: filepath.Base(filename)}, http.StatusCreated)
	if err != nil {
		return InstallerUpload{}, err
	}
	var ticket uploadTicket
	if json.Unmarshal(data, &ticket) != nil {
		return InstallerUpload{}, errors.New("Iru returned invalid upload ticket JSON (content withheld)")
	}
	if err := validateUploadTicket(ticket, kind == "in-house-app", time.Now()); err != nil {
		return InstallerUpload{}, err
	}
	result := InstallerUpload{FileKey: ticket.FileKey, SHA256: expectedSHA, FileSize: info.Size(), PendingID: strings.ToLower(ticket.ID)}
	if err := c.streamInstaller(ctx, file, info.Size(), expectedSHA, ticket); err != nil {
		return result, err
	}
	if kind == "in-house-app" {
		if err := c.waitForIPA(ctx, result); err != nil {
			return result, err
		}
	} else {
		// S3 acceptance does not establish Iru processing readiness. Match the
		// initial five-second grace used by Iru's irupkg upload workflow before
		// the first Library Item write. This is not a readiness probe, and does
		// not authorize replaying a POST/PATCH that later returns HTTP 503.
		// https://github.com/kandji-inc/irupkg/blob/3fe1ec778c9e9fe7e7f7308dc8785c90e431df68/src/irupkg/helpers/utils.py
		if err := c.sleep(ctx, 5*time.Second); err != nil {
			return result, errors.New("Custom App upload processing wait interrupted; no Library Item write was attempted")
		}
	}
	return result, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func (c *Client) streamInstaller(ctx context.Context, file *os.File, size int64, expectedSHA string, ticket uploadTicket) error {
	// Build only form fields and boundaries in memory. The same open descriptor
	// is hashed and streamed so replacing the path cannot substitute a new file.
	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	for _, key := range []string{"key", "x-amz-algorithm", "x-amz-credential", "x-amz-date", "x-amz-security-token", "policy", "x-amz-signature"} {
		if err := writer.WriteField(key, ticket.PostData[key]); err != nil {
			return errors.New("could not encode installer upload form")
		}
	}
	if _, err := writer.CreateFormFile("file", filepath.Base(file.Name())); err != nil {
		return errors.New("could not encode installer file header")
	}
	boundary := form.Len()
	if writer.Close() != nil {
		return errors.New("could not complete installer upload form")
	}
	all := form.Bytes()
	prefix, suffix := all[:boundary], all[boundary:]
	hash := sha256.New()
	fileReader := io.TeeReader(&contextReader{ctx: ctx, reader: io.LimitReader(file, size+1)}, hash)
	body := io.MultiReader(bytes.NewReader(prefix), fileReader, bytes.NewReader(suffix))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ticket.PostURL, body)
	if err != nil {
		return errors.New("could not construct installer upload request")
	}
	req.ContentLength = int64(len(prefix)) + size + int64(len(suffix))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	// No bearer token, API User-Agent override, cookies, redirects or retries.
	httpClient := http.Client{Transport: c.http.Transport, Timeout: 15 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := httpClient.Do(req)
	if err != nil {
		return errors.New("S3 installer upload failed or timed out (details withheld); the artifact may exist; no request was retried")
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("S3 installer upload returned HTTP %d (body withheld); no request was retried", response.StatusCode)
	}
	if hex.EncodeToString(hash.Sum(nil)) != expectedSHA {
		return errors.New("installer changed while uploading; do not create or update a Library Item from this upload")
	}
	return nil
}

func (c *Client) waitForIPA(ctx context.Context, upload InstallerUpload) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	for attempt := 0; attempt < 60; attempt++ {
		data, err := c.request(ctx, http.MethodGet, inHouseAppsPath+"/upload/"+upload.PendingID+"/status", nil, http.StatusOK)
		if err != nil {
			return err
		}
		var status struct {
			Status   string  `json:"status"`
			SHA256   *string `json:"sha256"`
			Metadata struct {
				FileKey  string `json:"file_key"`
				FileSize *int64 `json:"file_size"`
			} `json:"metadata"`
		}
		if json.Unmarshal(data, &status) != nil {
			return errors.New("Iru returned invalid IPA upload status JSON (content withheld)")
		}
		switch status.Status {
		case "VALIDATED":
			if status.SHA256 == nil || !strings.EqualFold(*status.SHA256, upload.SHA256) || status.Metadata.FileKey != upload.FileKey || (status.Metadata.FileSize != nil && *status.Metadata.FileSize != upload.FileSize) {
				return errors.New("validated IPA upload metadata does not match the local installer")
			}
			return nil
		case "UPLOADING", "UPLOADED":
			if attempt == 59 {
				break
			}
			if err := c.sleep(ctx, 2*time.Second); err != nil {
				return errors.New("IPA upload validation interrupted; no further write was attempted")
			}
		case "UPLOAD_FAILED", "VALIDATE_FAILED":
			return errors.New("Iru reported IPA upload or validation failure (reason withheld); no request was retried")
		default:
			return errors.New("Iru returned an unsupported IPA upload status (content withheld)")
		}
	}
	return errors.New("IPA upload did not validate within its polling bound; no Library Item write was attempted")
}
