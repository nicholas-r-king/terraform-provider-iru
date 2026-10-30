// SPDX-License-Identifier: MPL-2.0

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/mail"
	"strings"
)

const adePath = "/api/v1/integrations/apple/ade"

type ADEInput struct {
	BlueprintID string `json:"blueprint_id"`
	Phone       string `json:"phone"`
	Email       string `json:"email"`
}
type ADEIntegration struct {
	ID        string `json:"id"`
	Blueprint struct {
		ID string `json:"id"`
	} `json:"blueprint"`
	Defaults struct {
		Phone *string `json:"phone"`
		Email *string `json:"email"`
	} `json:"defaults"`
	ServerName        string `json:"server_name"`
	AccessTokenExpiry string `json:"access_token_expiry"`
}

func (input ADEInput) Validate() error {
	if !ValidID(input.BlueprintID) {
		return errors.New("ADE blueprint_id must be a UUID")
	}
	address, err := mail.ParseAddress(input.Email)
	if err != nil || address.Address != input.Email || strings.TrimSpace(input.Phone) == "" {
		return errors.New("ADE requires an email address and nonblank phone")
	}
	return nil
}
func adeMultipart(input ADEInput, token []byte) ([]byte, string, error) {
	if err := input.Validate(); err != nil {
		return nil, "", err
	}
	if len(token) == 0 || len(token) > 1024*1024 {
		return nil, "", errors.New("ADE server token must be nonempty and at most 1 MiB")
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for field, value := range map[string]string{"blueprint_id": input.BlueprintID, "phone": input.Phone, "email": input.Email} {
		if w.WriteField(field, value) != nil {
			return nil, "", errors.New("could not encode ADE metadata")
		}
	}
	file, err := w.CreateFormFile("file", "server-token.p7m")
	if err != nil {
		return nil, "", errors.New("could not encode ADE file")
	}
	if _, err = file.Write(token); err != nil || w.Close() != nil {
		return nil, "", errors.New("could not complete ADE request (content withheld)")
	}
	return body.Bytes(), w.FormDataContentType(), nil
}
func decodeADE(data []byte, expected string) (ADEIntegration, error) {
	var value ADEIntegration
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || json.Unmarshal(data, &value) != nil || !ValidID(value.ID) || (expected != "" && !strings.EqualFold(value.ID, expected)) || !ValidID(value.Blueprint.ID) || value.Defaults.Email == nil || value.Defaults.Phone == nil {
		return value, errors.New("Iru returned invalid ADE managed fields (content withheld)")
	}
	for _, field := range []string{"server_name", "access_token_expiry"} {
		if raw, exists := fields[field]; !exists || string(raw) == "null" {
			return value, errors.New("Iru ADE response is missing metadata (content withheld)")
		}
	}
	value.ID, value.Blueprint.ID = strings.ToLower(value.ID), strings.ToLower(value.Blueprint.ID)
	return value, nil
}
func (c *Client) CreateADEIntegration(ctx context.Context, input ADEInput, token []byte) (ADEIntegration, error) {
	body, contentType, err := adeMultipart(input, token)
	if err != nil {
		return ADEIntegration{}, err
	}
	// Iru documents HTTP 200 for ADE integration creation.
	data, err := c.requestEncoded(ctx, http.MethodPost, adePath+"/", body, contentType, http.StatusOK)
	if err != nil {
		return ADEIntegration{}, err
	}
	return decodeADE(data, "")
}
func (c *Client) GetADEIntegration(ctx context.Context, id string) (ADEIntegration, error) {
	if !ValidID(id) {
		return ADEIntegration{}, errors.New("ADE integration ID must be a UUID")
	}
	data, err := c.request(ctx, http.MethodGet, adePath+"/"+strings.ToLower(id), nil, http.StatusOK)
	if err != nil {
		return ADEIntegration{}, err
	}
	return decodeADE(data, id)
}
func (c *Client) UpdateADEIntegration(ctx context.Context, id string, input ADEInput) (ADEIntegration, error) {
	if !ValidID(id) {
		return ADEIntegration{}, errors.New("ADE integration ID must be a UUID")
	}
	if err := input.Validate(); err != nil {
		return ADEIntegration{}, err
	}
	data, err := c.request(ctx, http.MethodPatch, adePath+"/"+strings.ToLower(id), input, http.StatusOK)
	if err != nil {
		return ADEIntegration{}, err
	}
	return decodeADE(data, id)
}
func (c *Client) RenewADEIntegration(ctx context.Context, id string, input ADEInput, token []byte) (ADEIntegration, error) {
	if !ValidID(id) {
		return ADEIntegration{}, errors.New("ADE integration ID must be a UUID")
	}
	body, contentType, err := adeMultipart(input, token)
	if err != nil {
		return ADEIntegration{}, err
	}
	data, err := c.requestEncoded(ctx, http.MethodPost, adePath+"/"+strings.ToLower(id)+"/renew", body, contentType, http.StatusOK)
	if err != nil {
		return ADEIntegration{}, err
	}
	return decodeADE(data, id)
}
func (c *Client) DeleteADEIntegration(ctx context.Context, id string) error {
	if !ValidID(id) {
		return errors.New("ADE integration ID must be a UUID")
	}
	_, err := c.request(ctx, http.MethodDelete, adePath+"/"+strings.ToLower(id), nil, http.StatusNoContent)
	if IsNotFound(err) {
		return nil
	}
	return err
}
