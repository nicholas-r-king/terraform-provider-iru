// SPDX-License-Identifier: MPL-2.0

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const profilesPath = "/api/v1/library/custom-profiles"

type ProfileInput struct {
	Name         string `json:"name"`
	Profile      string `json:"profile"`
	Active       bool   `json:"active"`
	RunsOnMac    bool   `json:"runs_on_mac"`
	RunsOnIphone bool   `json:"runs_on_iphone"`
	RunsOnIpad   bool   `json:"runs_on_ipad"`
	RunsOnTV     bool   `json:"runs_on_tv"`
	RunsOnVision bool   `json:"runs_on_vision"`
}

type Profile struct {
	ID            string `json:"id"`
	MDMIdentifier string `json:"mdm_identifier"`
	ProfileInput
}

// ValidateProfileXML checks syntax only. encoding/xml does not fetch external
// DTDs/entities. Preserve all bytes; never normalize whitespace inside values.
func ValidateProfileXML(content string) error {
	if strings.TrimSpace(content) == "" || len(content) > responseLimit {
		return errors.New("profile must contain XML within the 2 MiB safety limit")
	}
	decoder := xml.NewDecoder(strings.NewReader(content))
	depth, roots := 0, 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.New("profile must be well-formed XML (content withheld)")
		}
		switch node := token.(type) {
		case xml.StartElement:
			if depth == 0 {
				roots++
				if node.Name.Local != "plist" {
					return errors.New("profile XML must have one plist root")
				}
			}
			depth++
		case xml.EndElement:
			depth--
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(node)) != "" {
				return errors.New("profile XML must have one plist root")
			}
		}
	}
	if roots != 1 || depth != 0 {
		return errors.New("profile XML must have one plist root")
	}
	return nil
}

func (input ProfileInput) Validate() error {
	if strings.TrimSpace(input.Name) == "" {
		return errors.New("profile name must not be empty")
	}
	if !input.RunsOnMac && !input.RunsOnIphone && !input.RunsOnIpad && !input.RunsOnTV && !input.RunsOnVision {
		return errors.New("choose at least one supported runs_on_* platform")
	}
	return ValidateProfileXML(input.Profile)
}

func (input ProfileInput) form() url.Values {
	return url.Values{"name": {input.Name}, "active": {strconv.FormatBool(input.Active)},
		"runs_on_mac": {strconv.FormatBool(input.RunsOnMac)}, "runs_on_iphone": {strconv.FormatBool(input.RunsOnIphone)},
		"runs_on_ipad": {strconv.FormatBool(input.RunsOnIpad)}, "runs_on_tv": {strconv.FormatBool(input.RunsOnTV)}, "runs_on_vision": {strconv.FormatBool(input.RunsOnVision)}}
}

func (input ProfileInput) multipart() ([]byte, string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, values := range input.form() {
		if err := writer.WriteField(key, values[0]); err != nil {
			return nil, "", errors.New("could not encode profile fields")
		}
	}
	file, err := writer.CreateFormFile("file", "terraform.mobileconfig")
	if err != nil {
		return nil, "", errors.New("could not encode profile file")
	}
	if _, err := io.WriteString(file, input.Profile); err != nil {
		return nil, "", errors.New("could not encode profile content (withheld)")
	}
	if err := writer.Close(); err != nil {
		return nil, "", errors.New("could not complete profile request")
	}
	return body.Bytes(), writer.FormDataContentType(), nil
}

func decodeProfile(data []byte) (Profile, error) {
	var result Profile
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || json.Unmarshal(data, &result) != nil {
		return result, errors.New("Iru returned invalid Custom Profile JSON (content withheld)")
	}
	for _, key := range []string{"id", "name", "active", "profile", "mdm_identifier", "runs_on_mac", "runs_on_iphone", "runs_on_ipad", "runs_on_tv", "runs_on_vision"} {
		if value, ok := fields[key]; !ok || string(value) == "null" {
			return result, fmt.Errorf("Iru Custom Profile response is missing required field %s (content withheld)", key)
		}
	}
	if !ValidID(result.ID) || strings.TrimSpace(result.MDMIdentifier) == "" {
		return result, errors.New("Iru returned invalid Custom Profile identity metadata")
	}
	if err := result.ProfileInput.Validate(); err != nil {
		return result, fmt.Errorf("Iru returned unsupported Custom Profile data: %w", err)
	}
	result.ID = strings.ToLower(result.ID)
	return result, nil
}

func (c *Client) CreateProfile(ctx context.Context, input ProfileInput) (Profile, error) {
	if err := input.Validate(); err != nil {
		return Profile{}, err
	}
	body, contentType, err := input.multipart()
	if err != nil {
		return Profile{}, err
	}
	data, err := c.requestEncoded(ctx, http.MethodPost, profilesPath, body, contentType, http.StatusCreated)
	if err != nil {
		return Profile{}, err
	}
	return decodeProfile(data)
}

func (c *Client) GetProfile(ctx context.Context, id string) (Profile, error) {
	if !ValidID(id) {
		return Profile{}, errors.New("Custom Profile ID must be a UUID")
	}
	data, err := c.request(ctx, http.MethodGet, profilesPath+"/"+strings.ToLower(id), nil, http.StatusOK)
	if err != nil {
		return Profile{}, err
	}
	result, err := decodeProfile(data)
	if err == nil && !strings.EqualFold(result.ID, id) {
		return Profile{}, errors.New("Iru response ID does not match the requested Custom Profile")
	}
	return result, err
}

func (c *Client) UpdateProfile(ctx context.Context, id string, input ProfileInput, replaceContent bool) (Profile, error) {
	if !ValidID(id) {
		return Profile{}, errors.New("Custom Profile ID must be a UUID")
	}
	if err := input.Validate(); err != nil {
		return Profile{}, err
	}
	body, contentType := []byte(input.form().Encode()), "application/x-www-form-urlencoded"
	if replaceContent {
		var err error
		body, contentType, err = input.multipart()
		if err != nil {
			return Profile{}, err
		}
	}
	data, err := c.requestEncoded(ctx, http.MethodPatch, profilesPath+"/"+strings.ToLower(id), body, contentType, http.StatusOK)
	if err != nil {
		return Profile{}, err
	}
	result, err := decodeProfile(data)
	if err == nil && !strings.EqualFold(result.ID, id) {
		return Profile{}, errors.New("Iru response ID does not match the updated Custom Profile")
	}
	return result, err
}

func (c *Client) DeleteProfile(ctx context.Context, id string) error {
	if !ValidID(id) {
		return errors.New("Custom Profile ID must be a UUID")
	}
	_, err := c.request(ctx, http.MethodDelete, profilesPath+"/"+strings.ToLower(id), nil, http.StatusNoContent)
	if IsNotFound(err) {
		return nil
	}
	return err
}
