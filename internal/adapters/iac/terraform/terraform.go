package terraform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/store"
)

const (
	statusNotConnected = "Not connected to Terraform state"
	statusConnectedFmt = "Connected to Terraform state at %s"
)

// ErrNotConnected indicates the adapter is not connected.
var ErrNotConnected = errors.New("adapter not connected to Terraform state")

// Resource represents one Terraform-managed resource.
type Resource struct {
	Address   string             `json:"address"`
	Type      string             `json:"type"`
	Name      string             `json:"name"`
	Provider  string             `json:"provider"`
	Mode      string             `json:"mode"` // "managed" or "data"
	Instances []ResourceInstance `json:"instances"`
}

// ResourceInstance holds the attributes of one resource instance.
// Sensitive attributes are redacted.
type ResourceInstance struct {
	Attributes map[string]any `json:"attributes"`
}

// Output represents a Terraform output value.
type Output struct {
	Value     any    `json:"value"`
	Type      string `json:"type"`
	Sensitive bool   `json:"sensitive"`
}

// State is the parsed content of a Terraform state file.
type State struct {
	FormatVersion    string            `json:"format_version"`
	TerraformVersion string            `json:"terraform_version"`
	Resources        []Resource        `json:"resources"`
	Outputs          map[string]Output `json:"outputs"`
}

// stateReader abstracts file reading for testability.
type stateReader interface {
	readFile(path string) ([]byte, error)
	resolvePath(path string) (string, error)
}

// osReader uses the real OS filesystem.
type osReader struct{}

func (osReader) readFile(path string) ([]byte, error) { return os.ReadFile(path) }
func (osReader) resolvePath(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", path, err)
	}
	if info.IsDir() {
		candidate := filepath.Join(path, "terraform.tfstate")
		if _, err := os.Stat(candidate); err != nil {
			return "", fmt.Errorf("terraform.tfstate not found in %s", path)
		}
		return candidate, nil
	}
	return path, nil
}

// TerraformAdapter extends the base Adapter with Terraform state operations.
type TerraformAdapter interface {
	adapters.Adapter
	Resources(ctx context.Context, resourceType string) ([]Resource, error)
	GetResource(ctx context.Context, address string) (*Resource, error)
	Outputs(ctx context.Context) (map[string]Output, error)
}

// Adapter is the concrete Terraform state adapter.
type Adapter struct {
	mu           sync.RWMutex
	config       Config
	resolvedPath string
	reader       stateReader
	connected    bool
}

// New creates a new Terraform adapter (not yet connected).
func New() *Adapter {
	return &Adapter{reader: osReader{}}
}

// NewWithReader creates an adapter with an injected reader (for testing).
func NewWithReader(reader stateReader, cfg Config, resolvedPath string) *Adapter {
	return &Adapter{
		reader:       reader,
		config:       cfg,
		resolvedPath: resolvedPath,
		connected:    true,
	}
}

// Connect parses config and verifies the state file is readable.
func (a *Adapter) Connect(_ context.Context, source store.Component) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	cfg, err := ParseConfig(source.Config)
	if err != nil {
		return fmt.Errorf("parse source config: %w", err)
	}
	a.config = cfg

	resolved, err := a.reader.resolvePath(cfg.StatePath)
	if err != nil {
		return fmt.Errorf("resolve state path: %w", err)
	}

	// Try reading the file to validate it's accessible and valid JSON.
	data, err := a.reader.readFile(resolved)
	if err != nil {
		return fmt.Errorf("read state file %s: %w", resolved, err)
	}
	var check struct {
		FormatVersion string `json:"format_version"`
	}
	if err := json.Unmarshal(data, &check); err != nil {
		return fmt.Errorf("parse state file %s: not valid Terraform JSON state: %w", resolved, err)
	}

	a.resolvedPath = resolved
	a.connected = true
	return nil
}

// Disconnect clears the adapter state.
func (a *Adapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.connected = false
	return nil
}

// Status returns the current connection status.
func (a *Adapter) Status() adapters.Status {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.connected {
		return adapters.Status{
			Connected: true,
			Message:   fmt.Sprintf(statusConnectedFmt, a.resolvedPath),
		}
	}
	return adapters.Status{Connected: false, Message: statusNotConnected}
}

// Resources lists all managed resources in the state, optionally filtered by type.
func (a *Adapter) Resources(_ context.Context, resourceType string) ([]Resource, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	state, err := a.loadState()
	if err != nil {
		return nil, err
	}

	var out []Resource
	for _, r := range state.Resources {
		if r.Mode != "managed" {
			continue
		}
		if resourceType != "" && r.Type != resourceType {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

// GetResource returns a single resource by address.
func (a *Adapter) GetResource(_ context.Context, address string) (*Resource, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	state, err := a.loadState()
	if err != nil {
		return nil, err
	}

	for _, r := range state.Resources {
		if r.Address == address {
			return &r, nil
		}
	}
	return nil, fmt.Errorf("resource %q not found in state", address)
}

// Outputs returns all output values from the state.
func (a *Adapter) Outputs(_ context.Context) (map[string]Output, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	state, err := a.loadState()
	if err != nil {
		return nil, err
	}

	if state.Outputs == nil {
		return map[string]Output{}, nil
	}

	// Redact sensitive outputs.
	out := make(map[string]Output, len(state.Outputs))
	for k, v := range state.Outputs {
		if v.Sensitive {
			out[k] = Output{Value: "[redacted]", Type: v.Type, Sensitive: true}
		} else {
			out[k] = v
		}
	}
	return out, nil
}

// --- internal helpers ---

func (a *Adapter) checkConnected() error {
	if !a.connected {
		return ErrNotConnected
	}
	return nil
}

// tfstateJSON is the raw Terraform state file format.
type tfstateJSON struct {
	FormatVersion    string              `json:"format_version"`
	TerraformVersion string              `json:"terraform_version"`
	Resources        []tfResourceJSON    `json:"resources"`
	Outputs          map[string]tfOutput `json:"outputs"`
}

type tfResourceJSON struct {
	Mode      string           `json:"mode"`
	Type      string           `json:"type"`
	Name      string           `json:"name"`
	Provider  string           `json:"provider"`
	Instances []tfInstanceJSON `json:"instances"`
}

type tfInstanceJSON struct {
	Attributes          map[string]any `json:"attributes"`
	SensitiveAttributes []string       `json:"sensitive_attributes"`
}

type tfOutput struct {
	Value     any  `json:"value"`
	Type      any  `json:"type"` // can be string or array
	Sensitive bool `json:"sensitive"`
}

func (a *Adapter) loadState() (*State, error) {
	data, err := a.reader.readFile(a.resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("read state file: %w", err)
	}

	var raw tfstateJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse state file: %w", err)
	}

	state := &State{
		FormatVersion:    raw.FormatVersion,
		TerraformVersion: raw.TerraformVersion,
		Outputs:          make(map[string]Output),
	}

	for _, r := range raw.Resources {
		resource := Resource{
			Address:  resourceAddress(r),
			Type:     r.Type,
			Name:     r.Name,
			Provider: r.Provider,
			Mode:     r.Mode,
		}
		for _, inst := range r.Instances {
			attrs := redactSensitive(inst.Attributes, inst.SensitiveAttributes)
			resource.Instances = append(resource.Instances, ResourceInstance{Attributes: attrs})
		}
		state.Resources = append(state.Resources, resource)
	}

	for k, v := range raw.Outputs {
		typeStr := ""
		switch t := v.Type.(type) {
		case string:
			typeStr = t
		}
		state.Outputs[k] = Output{
			Value:     v.Value,
			Type:      typeStr,
			Sensitive: v.Sensitive,
		}
	}

	return state, nil
}

func resourceAddress(r tfResourceJSON) string {
	// module.name.resource_type.resource_name or resource_type.resource_name
	if strings.Contains(r.Provider, "module") {
		return r.Type + "." + r.Name
	}
	return r.Type + "." + r.Name
}

func redactSensitive(attrs map[string]any, sensitive []string) map[string]any {
	if len(sensitive) == 0 {
		return attrs
	}
	out := make(map[string]any, len(attrs))
	for k, v := range attrs {
		out[k] = v
	}
	for _, key := range sensitive {
		// sensitive_attributes may be nested paths like "password" or "credentials.0.key"
		top := strings.SplitN(key, ".", 2)[0]
		if _, ok := out[top]; ok {
			out[top] = "[redacted]"
		}
	}
	return out
}
