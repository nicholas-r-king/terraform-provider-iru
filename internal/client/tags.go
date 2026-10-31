// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

const tagsPath = "/api/v1/tags"

// A missing Tag is proven only by a complete listing. An HTTP404 for the
// collection (or a later page) is a read failure, not evidence of object absence.
type tagAbsentError struct{}

func (*tagAbsentError) Error() string { return "Iru Tag was absent from a complete listing" }
func (*tagAbsentError) Unwrap() error {
	return &APIError{Method: http.MethodGet, StatusCode: http.StatusNotFound}
}
func IsTagAbsent(err error) bool {
	var absent *tagAbsentError
	return errors.As(err, &absent)
}

type Tag struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func decodeTag(data []byte, expected string) (Tag, error) {
	var tag Tag
	if json.Unmarshal(data, &tag) != nil || !ValidID(tag.ID) || strings.TrimSpace(tag.Name) == "" || (expected != "" && !strings.EqualFold(expected, tag.ID)) {
		return tag, errors.New("Iru returned invalid Tag metadata (content withheld)")
	}
	tag.ID = strings.ToLower(tag.ID)
	return tag, nil
}

func (c *Client) CreateTag(ctx context.Context, name string) (Tag, error) {
	if strings.TrimSpace(name) == "" {
		return Tag{}, errors.New("Tag name must not be blank")
	}
	data, err := c.request(ctx, http.MethodPost, tagsPath, map[string]string{"name": name}, http.StatusCreated)
	if err != nil {
		return Tag{}, err
	}
	return decodeTag(data, "")
}

func (c *Client) UpdateTag(ctx context.Context, id, name string) (Tag, error) {
	if !ValidID(id) || strings.TrimSpace(name) == "" {
		return Tag{}, errors.New("Tag requires a UUID and nonblank name")
	}
	data, err := c.request(ctx, http.MethodPatch, tagsPath+"/"+strings.ToLower(id), map[string]string{"name": name}, http.StatusOK)
	if err != nil {
		return Tag{}, err
	}
	return decodeTag(data, id)
}

// There is no documented GET /tags/{id}. Refresh/import use the complete,
// bounded listing and exact UUID selection, never a name-based shortcut.
func (c *Client) GetTag(ctx context.Context, id string) (Tag, error) {
	if !ValidID(id) {
		return Tag{}, errors.New("Tag ID must be a UUID")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	next := tagsPath
	pages, ids := map[string]bool{}, map[string]bool{}
	var found *Tag
	for page := 0; ; page++ {
		if page >= 100 || pages[next] {
			return Tag{}, errors.New("Tag pagination exceeded its bound or repeated a page")
		}
		pages[next] = true
		data, err := c.request(ctx, http.MethodGet, next, nil, http.StatusOK)
		if err != nil {
			return Tag{}, err
		}
		var envelope struct {
			Next    json.RawMessage `json:"next"`
			Results *[]Tag          `json:"results"`
		}
		if json.Unmarshal(data, &envelope) != nil || envelope.Results == nil || len(envelope.Next) == 0 {
			return Tag{}, errors.New("Iru returned an invalid Tag page (content withheld)")
		}
		for _, tag := range *envelope.Results {
			if !ValidID(tag.ID) || strings.TrimSpace(tag.Name) == "" {
				return Tag{}, errors.New("Iru returned invalid Tag metadata (content withheld)")
			}
			tag.ID = strings.ToLower(tag.ID)
			if ids[tag.ID] || len(ids) >= 10000 {
				return Tag{}, errors.New("Tag listing repeated a UUID or exceeded its item bound")
			}
			ids[tag.ID] = true
			if strings.EqualFold(tag.ID, id) {
				copy := tag
				found = &copy
			}
		}
		if string(envelope.Next) == "null" {
			break
		}
		var raw string
		if json.Unmarshal(envelope.Next, &raw) != nil || strings.TrimSpace(raw) == "" {
			return Tag{}, errors.New("Iru returned invalid Tag pagination metadata")
		}
		next, err = c.nextLibraryPage(raw, tagsPath)
		if err != nil {
			return Tag{}, err
		}
	}
	if found == nil {
		return Tag{}, &tagAbsentError{}
	}
	return *found, nil
}

func (c *Client) DeleteTag(ctx context.Context, id string) error {
	if !ValidID(id) {
		return errors.New("Tag ID must be a UUID")
	}
	_, err := c.request(ctx, http.MethodDelete, tagsPath+"/"+strings.ToLower(id), nil, http.StatusNoContent)
	if IsNotFound(err) {
		return nil
	}
	return err
}
