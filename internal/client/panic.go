package client

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"
)

const (
	apiPanicPath       = "/api/v1/panic"
	apiPanicStatusPath = "/api/v1/panic/status"
	apiUnlockPath      = "/api/v1/unlock"
)

// PanicStatus represents the response from GET /api/v1/panic/status.
type PanicStatus struct {
	SafeMode      bool      `json:"safe_mode"`
	TriggeredAt   time.Time `json:"triggered_at,omitempty"`
	TriggerSource string    `json:"trigger_source,omitempty"`
	TriggerReason string    `json:"trigger_reason,omitempty"`
}

// TriggerPanic sends an emergency shutdown request to joecored.
// The reason is optional but recommended for the audit log.
func (c *Client) TriggerPanic(ctx context.Context, reason string) error {
	body, _ := json.Marshal(map[string]string{"reason": reason})
	return c.doJSON(ctx, "POST", c.baseURL+apiPanicPath, bytes.NewReader(body), http.StatusOK, nil, "trigger panic")
}

// GetPanicStatus returns the current safe mode status from joecored.
func (c *Client) GetPanicStatus(ctx context.Context) (*PanicStatus, error) {
	var status PanicStatus
	if err := c.doJSON(ctx, "GET", c.baseURL+apiPanicStatusPath, nil, http.StatusOK, &status, "panic status"); err != nil {
		return nil, err
	}
	return &status, nil
}

// Unlock exits safe mode. The reason is mandatory for the audit log.
func (c *Client) Unlock(ctx context.Context, reason string) error {
	body, _ := json.Marshal(map[string]string{"reason": reason})
	return c.doJSON(ctx, "POST", c.baseURL+apiUnlockPath, bytes.NewReader(body), http.StatusOK, nil, "unlock")
}
