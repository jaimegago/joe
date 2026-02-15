package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/store"
)

const (
	statusNotConnected = "Not connected to Azure"
	statusConnected    = "Connected to Azure"
	errorNotConnected  = "adapter not connected to Azure"
)

// VM represents an Azure virtual machine.
type VM struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Size     string            `json:"size"`
	State    string            `json:"state"`
	VNetID   string            `json:"vnet_id,omitempty"`
	SubnetID string            `json:"subnet_id,omitempty"`
	Tags     map[string]string `json:"tags,omitempty"`
	Location string            `json:"location,omitempty"`
}

// AKSCluster represents an AKS cluster.
type AKSCluster struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Version   string            `json:"version"`
	Status    string            `json:"status"`
	VNetID    string            `json:"vnet_id,omitempty"`
	SubnetIDs []string          `json:"subnet_ids,omitempty"`
	Tags      map[string]string `json:"tags,omitempty"`
	Location  string            `json:"location,omitempty"`
}

// SQLDatabase represents an Azure SQL database.
type SQLDatabase struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	ServerName string            `json:"server_name"`
	Edition    string            `json:"edition"`
	Status     string            `json:"status"`
	VNetID     string            `json:"vnet_id,omitempty"`
	Tags       map[string]string `json:"tags,omitempty"`
	Location   string            `json:"location,omitempty"`
}

// VNet represents an Azure virtual network.
type VNet struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Address  string            `json:"address"`
	Location string            `json:"location,omitempty"`
	Tags     map[string]string `json:"tags,omitempty"`
}

// AzureAdapter extends the base Adapter with Azure-specific operations.
type AzureAdapter interface {
	adapters.Adapter
	ListVMs(ctx context.Context) ([]VM, error)
	GetVM(ctx context.Context, id string) (*VM, error)
	ListAKSClusters(ctx context.Context) ([]AKSCluster, error)
	GetAKSCluster(ctx context.Context, id string) (*AKSCluster, error)
	ListSQLDatabases(ctx context.Context) ([]SQLDatabase, error)
	GetSQLDatabase(ctx context.Context, id string) (*SQLDatabase, error)
	ListVNets(ctx context.Context) ([]VNet, error)
	GetVNet(ctx context.Context, id string) (*VNet, error)
}

// Adapter is the Azure adapter skeleton.
type Adapter struct {
	mu        sync.RWMutex
	config    Config
	connected bool
}

// New creates a new Azure adapter (not yet connected).
func New() *Adapter {
	return &Adapter{}
}

// Connect validates config and marks the adapter connected.
func (a *Adapter) Connect(source store.Source) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	var configMap map[string]any
	if len(source.Config) > 0 {
		if err := json.Unmarshal(source.Config, &configMap); err != nil {
			return fmt.Errorf("parse source config JSON: %w", err)
		}
	} else {
		configMap = make(map[string]any)
	}

	cfg, err := ParseConfig(configMap)
	if err != nil {
		return fmt.Errorf("parse source config: %w", err)
	}
	a.config = cfg
	a.connected = true
	return nil
}

// Disconnect closes the adapter.
func (a *Adapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.connected = false
	return nil
}

// Status returns the connection status.
func (a *Adapter) Status() adapters.Status {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if !a.connected {
		return adapters.Status{Connected: false, Message: statusNotConnected}
	}
	return adapters.Status{Connected: true, Message: statusConnected}
}

// ListVMs returns VMs.
func (a *Adapter) ListVMs(ctx context.Context) ([]VM, error) {
	_ = ctx
	if err := a.checkConnected(); err != nil {
		return nil, err
	}
	return []VM{}, nil
}

// GetVM returns a VM by ID.
func (a *Adapter) GetVM(ctx context.Context, id string) (*VM, error) {
	_ = ctx
	_ = id
	if err := a.checkConnected(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("vm not found: %s", id)
}

// ListAKSClusters returns AKS clusters.
func (a *Adapter) ListAKSClusters(ctx context.Context) ([]AKSCluster, error) {
	_ = ctx
	if err := a.checkConnected(); err != nil {
		return nil, err
	}
	return []AKSCluster{}, nil
}

// GetAKSCluster returns an AKS cluster by ID.
func (a *Adapter) GetAKSCluster(ctx context.Context, id string) (*AKSCluster, error) {
	_ = ctx
	_ = id
	if err := a.checkConnected(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("aks cluster not found: %s", id)
}

// ListSQLDatabases returns SQL databases.
func (a *Adapter) ListSQLDatabases(ctx context.Context) ([]SQLDatabase, error) {
	_ = ctx
	if err := a.checkConnected(); err != nil {
		return nil, err
	}
	return []SQLDatabase{}, nil
}

// GetSQLDatabase returns a SQL database by ID.
func (a *Adapter) GetSQLDatabase(ctx context.Context, id string) (*SQLDatabase, error) {
	_ = ctx
	_ = id
	if err := a.checkConnected(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("sql database not found: %s", id)
}

// ListVNets returns VNets.
func (a *Adapter) ListVNets(ctx context.Context) ([]VNet, error) {
	_ = ctx
	if err := a.checkConnected(); err != nil {
		return nil, err
	}
	return []VNet{}, nil
}

// GetVNet returns a VNet by ID.
func (a *Adapter) GetVNet(ctx context.Context, id string) (*VNet, error) {
	_ = ctx
	_ = id
	if err := a.checkConnected(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("vnet not found: %s", id)
}

func (a *Adapter) checkConnected() error {
	if !a.connected {
		return fmt.Errorf(errorNotConnected)
	}
	return nil
}
