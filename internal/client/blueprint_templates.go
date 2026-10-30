// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

const blueprintTemplatesPath = "/api/v1/blueprints/templates/"

type BlueprintTemplate struct {
	ID           string
	Name         string
	CategoryID   string
	CategoryName string
}

// Templates use positive integer IDs, unlike tenant Blueprint UUIDs. Flatten
// documented category groups while excluding parameter/security definitions.
func (c *Client) ListBlueprintTemplates(ctx context.Context) ([]BlueprintTemplate, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	next := blueprintTemplatesPath
	pages, ids := map[string]bool{}, map[uint64]bool{}
	result := []BlueprintTemplate{}
	for page := 0; ; page++ {
		if page >= 100 || pages[next] {
			return nil, errors.New("Blueprint template pagination repeated or exceeded its bound")
		}
		pages[next] = true
		data, err := c.request(ctx, http.MethodGet, next, nil, http.StatusOK)
		if err != nil {
			return nil, err
		}
		var envelope struct {
			Next    json.RawMessage `json:"next"`
			Results *[]struct {
				ID        *uint64 `json:"id"`
				Name      *string `json:"name"`
				Templates *[]struct {
					ID   *uint64 `json:"id"`
					Name *string `json:"name"`
				} `json:"templates"`
			} `json:"results"`
		}
		if json.Unmarshal(data, &envelope) != nil || envelope.Results == nil || len(envelope.Next) == 0 {
			return nil, errors.New("Iru returned an invalid Blueprint template page (content withheld)")
		}
		for _, group := range *envelope.Results {
			if group.ID == nil || *group.ID == 0 || group.Name == nil || strings.TrimSpace(*group.Name) == "" || group.Templates == nil {
				return nil, errors.New("Iru returned incomplete Blueprint template category metadata")
			}
			for _, template := range *group.Templates {
				if template.ID == nil || *template.ID == 0 || template.Name == nil || strings.TrimSpace(*template.Name) == "" || ids[*template.ID] || len(result) >= 10000 {
					return nil, errors.New("Iru returned invalid, duplicate or excessive Blueprint template metadata")
				}
				ids[*template.ID] = true
				result = append(result, BlueprintTemplate{ID: strconv.FormatUint(*template.ID, 10), Name: *template.Name, CategoryID: strconv.FormatUint(*group.ID, 10), CategoryName: *group.Name})
			}
		}
		if string(envelope.Next) == "null" {
			break
		}
		var raw string
		if json.Unmarshal(envelope.Next, &raw) != nil || strings.TrimSpace(raw) == "" {
			return nil, errors.New("Iru returned invalid Blueprint template pagination")
		}
		next, err = c.nextLibraryPage(raw, blueprintTemplatesPath)
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(result, func(i, j int) bool {
		left, _ := strconv.ParseUint(result[i].ID, 10, 64)
		right, _ := strconv.ParseUint(result[j].ID, 10, 64)
		return left < right
	})
	return result, nil
}
