// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

type DeviceNote struct {
	ID      string `json:"note_id"`
	Content string `json:"content"`
}

func deviceNotesPath(deviceID string) (string, error) {
	if !ValidID(deviceID) {
		return "", errors.New("Device ID must be a UUID")
	}
	return "/api/v1/devices/" + strings.ToLower(deviceID) + "/notes", nil
}
func decodeDeviceNote(data []byte, expected string) (DeviceNote, error) {
	var note DeviceNote
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || json.Unmarshal(data, &note) != nil || !ValidID(note.ID) || (expected != "" && !strings.EqualFold(note.ID, expected)) || len(fields["content"]) == 0 || string(fields["content"]) == "null" {
		return note, errors.New("Iru returned invalid Device Note data (content withheld)")
	}
	note.ID = strings.ToLower(note.ID)
	return note, nil
}
func (c *Client) CreateDeviceNote(ctx context.Context, deviceID, content string) (DeviceNote, error) {
	endpoint, err := deviceNotesPath(deviceID)
	if err != nil {
		return DeviceNote{}, err
	}
	if strings.TrimSpace(content) == "" {
		return DeviceNote{}, errors.New("Device Note content must not be blank")
	}
	data, err := c.request(ctx, http.MethodPost, endpoint, map[string]string{"content": content}, http.StatusCreated)
	if err != nil {
		return DeviceNote{}, err
	}
	return decodeDeviceNote(data, "")
}
func (c *Client) GetDeviceNote(ctx context.Context, deviceID, id string) (DeviceNote, error) {
	endpoint, err := deviceNotesPath(deviceID)
	if err != nil {
		return DeviceNote{}, err
	}
	if !ValidID(id) {
		return DeviceNote{}, errors.New("Device Note ID must be a UUID")
	}
	data, err := c.request(ctx, http.MethodGet, endpoint+"/"+strings.ToLower(id), nil, http.StatusOK)
	if err != nil {
		return DeviceNote{}, err
	}
	return decodeDeviceNote(data, id)
}
func (c *Client) UpdateDeviceNote(ctx context.Context, deviceID, id, content string) (DeviceNote, error) {
	endpoint, err := deviceNotesPath(deviceID)
	if err != nil {
		return DeviceNote{}, err
	}
	if !ValidID(id) || strings.TrimSpace(content) == "" {
		return DeviceNote{}, errors.New("Device Note requires a UUID and nonblank content")
	}
	data, err := c.request(ctx, http.MethodPatch, endpoint+"/"+strings.ToLower(id), map[string]string{"content": content}, http.StatusOK)
	if err != nil {
		return DeviceNote{}, err
	}
	return decodeDeviceNote(data, id)
}
func (c *Client) DeleteDeviceNote(ctx context.Context, deviceID, id string) error {
	endpoint, err := deviceNotesPath(deviceID)
	if err != nil {
		return err
	}
	if !ValidID(id) {
		return errors.New("Device Note ID must be a UUID")
	}
	// Unlike Library Item deletion, the documented Notes response is HTTP 200.
	_, err = c.request(ctx, http.MethodDelete, endpoint+"/"+strings.ToLower(id), nil, http.StatusOK)
	if IsNotFound(err) {
		return nil
	}
	return err
}
