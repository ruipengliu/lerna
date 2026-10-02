// Package sdk provides local diagnostics in the initial development skeleton.
// WSS, original commands and domain recovery are not implemented yet.
package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type HostStatus struct {
	Protocol       string          `json:"protocol"`
	Role           string          `json:"role"`
	InstanceID     string          `json:"instance_id"`
	BootID         string          `json:"boot_id"`
	Phase          string          `json:"phase"`
	AcceptingTasks bool            `json:"accepting_tasks"`
	Dependencies   map[string]bool `json:"dependencies"`
	Missing        []string        `json:"missing"`
}

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func (client Client) Status(ctx context.Context) (HostStatus, error) {
	var status HostStatus
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(client.BaseURL, "/")+"/status", nil)
	if err != nil {
		return status, err
	}
	transport := client.HTTP
	if transport == nil {
		transport = http.DefaultClient
	}
	response, err := transport.Do(request)
	if err != nil {
		return status, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return status, fmt.Errorf("host diagnostic HTTP status %d", response.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&status); err != nil {
		return status, err
	}
	if status.Protocol != "lerna-dev-status/1" {
		return status, errors.New("unsupported development diagnostic protocol")
	}
	return status, nil
}
