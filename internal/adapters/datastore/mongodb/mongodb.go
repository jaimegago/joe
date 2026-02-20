package mongodb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/store"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	statusNotConnected = "Not connected to MongoDB"
	statusConnectedFmt = "Connected to MongoDB at %s"
)

// ErrNotConnected means the adapter is not connected.
var ErrNotConnected = errors.New("adapter not connected to MongoDB")

// mongoRunner abstracts mongo.Client for testability.
type mongoRunner interface {
	ping(ctx context.Context) error
	runCommand(ctx context.Context, dbName string, cmd any) (map[string]any, error)
	disconnect(ctx context.Context) error
}

// realMongoRunner wraps *mongo.Client.
type realMongoRunner struct{ c *mongo.Client }

func (r *realMongoRunner) ping(ctx context.Context) error {
	return r.c.Ping(ctx, nil)
}

func (r *realMongoRunner) runCommand(ctx context.Context, dbName string, cmd any) (map[string]any, error) {
	var result map[string]any
	if err := r.c.Database(dbName).RunCommand(ctx, cmd).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *realMongoRunner) disconnect(ctx context.Context) error {
	return r.c.Disconnect(ctx)
}

// MongoDBAdapter is the interface for MongoDB operations.
type MongoDBAdapter interface {
	adapters.Adapter
	ServerStatus(ctx context.Context) (map[string]any, error)
	ReplicaStatus(ctx context.Context) (map[string]any, error)
	CurrentOp(ctx context.Context) (map[string]any, error)
}

// Adapter is the concrete MongoDB adapter.
type Adapter struct {
	mu        sync.RWMutex
	config    Config
	runner    mongoRunner
	connected bool
}

// New creates a new MongoDB adapter (not yet connected).
func New() *Adapter {
	return &Adapter{}
}

// NewWithRunner creates an adapter with a custom runner (for testing).
func NewWithRunner(r mongoRunner) *Adapter {
	return &Adapter{runner: r, connected: true}
}

// Connect establishes and verifies connectivity to MongoDB.
func (a *Adapter) Connect(ctx context.Context, source store.Source) error {
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

	clientOpts := options.Client().ApplyURI(cfg.URI)
	c, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		return fmt.Errorf("connect to MongoDB: %w", err)
	}

	r := &realMongoRunner{c: c}
	if err := r.ping(ctx); err != nil {
		_ = c.Disconnect(ctx)
		return fmt.Errorf("ping MongoDB at %s: %w", cfg.URI, err)
	}

	a.runner = r
	a.connected = true
	return nil
}

// Disconnect closes the connection.
func (a *Adapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.runner != nil {
		_ = a.runner.disconnect(context.Background())
		a.runner = nil
	}
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
			Message:   fmt.Sprintf(statusConnectedFmt, a.config.URI),
		}
	}
	return adapters.Status{
		Connected: false,
		Message:   statusNotConnected,
	}
}

// ServerStatus runs the serverStatus command.
func (a *Adapter) ServerStatus(ctx context.Context) (map[string]any, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	result, err := a.runner.runCommand(ctx, a.config.Database, bson.D{{Key: "serverStatus", Value: 1}})
	if err != nil {
		return nil, fmt.Errorf("serverStatus command: %w", err)
	}
	return result, nil
}

// ReplicaStatus runs replSetGetStatus. Returns an error if not a replica set.
func (a *Adapter) ReplicaStatus(ctx context.Context) (map[string]any, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	result, err := a.runner.runCommand(ctx, a.config.Database, bson.D{{Key: "replSetGetStatus", Value: 1}})
	if err != nil {
		return nil, fmt.Errorf("replSetGetStatus command: %w", err)
	}
	return result, nil
}

// CurrentOp runs the currentOp command.
func (a *Adapter) CurrentOp(ctx context.Context) (map[string]any, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	result, err := a.runner.runCommand(ctx, a.config.Database, bson.D{{Key: "currentOp", Value: 1}})
	if err != nil {
		return nil, fmt.Errorf("currentOp command: %w", err)
	}
	return result, nil
}

func (a *Adapter) checkConnected() error {
	if !a.connected {
		return ErrNotConnected
	}
	return nil
}
