// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Inventory returns identity metadata only, never scripts, XML, enrollment
// codes, signed installer URLs, users, devices, or credential-bearing fields.
type InventoryItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func inventoryEndpoint(kind string) (endpoint, nameField string, array bool) {
	switch kind {
	case "blueprints":
		return blueprintsPath, "name", false
	case "tags":
		return tagsPath, "name", false
	case "custom_scripts":
		return scriptsPath, "name", false
	case "custom_apps":
		return appsPath, "name", false
	case "custom_profiles":
		return profilesPath, "name", false
	case "in_house_apps":
		return inHouseAppsPath, "name", false
	case "ade_integrations":
		return adePath, "server_name", false
	case "self_service_categories":
		return "/api/v1/self-service/categories", "name", true
	}
	return "", "", false
}
func ValidInventoryKind(kind string) bool {
	endpoint, _, _ := inventoryEndpoint(kind)
	return endpoint != ""
}
func (c *Client) ListInventory(ctx context.Context, kind string) ([]InventoryItem, error) {
	endpoint, nameField, array := inventoryEndpoint(kind)
	if endpoint == "" {
		return nil, errors.New("unsupported configuration inventory kind")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	next := endpoint
	pages, ids := map[string]bool{}, map[string]bool{}
	items := []InventoryItem{}
	for page := 0; ; page++ {
		if page >= 100 || pages[next] {
			return nil, errors.New("configuration inventory exceeded its page bound or repeated a page")
		}
		pages[next] = true
		data, err := c.request(ctx, http.MethodGet, next, nil, http.StatusOK)
		if err != nil {
			return nil, err
		}
		var records []map[string]json.RawMessage
		var nextRaw json.RawMessage
		if array {
			if json.Unmarshal(data, &records) != nil || records == nil {
				return nil, errors.New("invalid configuration inventory array (content withheld)")
			}
			nextRaw = json.RawMessage("null")
		} else {
			var envelope struct {
				Results *[]map[string]json.RawMessage `json:"results"`
				Next    json.RawMessage               `json:"next"`
			}
			if json.Unmarshal(data, &envelope) != nil || envelope.Results == nil || len(envelope.Next) == 0 {
				return nil, errors.New("invalid configuration inventory page (content withheld)")
			}
			records, nextRaw = *envelope.Results, envelope.Next
		}
		for _, record := range records {
			var item InventoryItem
			if json.Unmarshal(record["id"], &item.ID) != nil || json.Unmarshal(record[nameField], &item.Name) != nil || !ValidID(item.ID) || strings.TrimSpace(item.Name) == "" {
				return nil, errors.New("invalid configuration inventory identity (content withheld)")
			}
			item.ID = strings.ToLower(item.ID)
			if ids[item.ID] || len(items) >= 10000 {
				return nil, errors.New("configuration inventory repeated an ID or exceeded its item bound")
			}
			ids[item.ID] = true
			items = append(items, item)
		}
		if string(nextRaw) == "null" {
			break
		}
		var raw string
		if json.Unmarshal(nextRaw, &raw) != nil || strings.TrimSpace(raw) == "" {
			return nil, errors.New("invalid configuration inventory pagination (content withheld)")
		}
		next, err = c.nextLibraryPage(raw, endpoint)
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}
