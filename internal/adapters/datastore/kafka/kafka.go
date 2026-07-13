package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/store"
	kafkago "github.com/segmentio/kafka-go"
)

const (
	statusNotConnected = "Not connected to Kafka"
	statusConnectedFmt = "Connected to Kafka brokers: %v"
)

// ErrNotConnected means the adapter is not connected.
var ErrNotConnected = errors.New("adapter not connected to Kafka")

// TopicInfo holds metadata for a Kafka topic.
type TopicInfo struct {
	Name       string `json:"name"`
	Partitions int    `json:"partitions"`
	Internal   bool   `json:"internal"`
}

// BrokerInfo holds metadata for a Kafka broker.
type BrokerInfo struct {
	ID   int    `json:"id"`
	Host string `json:"host"`
	Port int    `json:"port"`
}

// ConsumerGroupInfo holds metadata for a Kafka consumer group.
type ConsumerGroupInfo struct {
	GroupID  string `json:"group_id"`
	State    string `json:"state"`
	Protocol string `json:"protocol"`
}

// kafkaAdmin abstracts kafka.Client for testability.
type kafkaAdmin interface {
	fetchMetadata(ctx context.Context) ([]TopicInfo, []BrokerInfo, error)
	listGroups(ctx context.Context) ([]ConsumerGroupInfo, error)
	close() error
}

// realKafkaAdmin wraps kafka.Client.
type realKafkaAdmin struct {
	c *kafkago.Client
}

func (r *realKafkaAdmin) fetchMetadata(ctx context.Context) ([]TopicInfo, []BrokerInfo, error) {
	resp, err := r.c.Metadata(ctx, &kafkago.MetadataRequest{Topics: nil})
	if err != nil {
		return nil, nil, err
	}

	topics := make([]TopicInfo, 0, len(resp.Topics))
	for _, t := range resp.Topics {
		if t.Error != nil {
			continue
		}
		topics = append(topics, TopicInfo{
			Name:       t.Name,
			Partitions: len(t.Partitions),
			Internal:   t.Internal,
		})
	}

	brokers := make([]BrokerInfo, 0, len(resp.Brokers))
	for _, b := range resp.Brokers {
		brokers = append(brokers, BrokerInfo{
			ID:   b.ID,
			Host: b.Host,
			Port: b.Port,
		})
	}

	return topics, brokers, nil
}

func (r *realKafkaAdmin) listGroups(ctx context.Context) ([]ConsumerGroupInfo, error) {
	resp, err := r.c.ListGroups(ctx, &kafkago.ListGroupsRequest{})
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, resp.Error
	}

	groups := make([]ConsumerGroupInfo, 0, len(resp.Groups))
	for _, g := range resp.Groups {
		groups = append(groups, ConsumerGroupInfo{
			GroupID:  g.GroupID,
			Protocol: g.ProtocolType,
		})
	}
	return groups, nil
}

func (r *realKafkaAdmin) close() error {
	// kafka.Client has no Close method; the transport is managed externally.
	return nil
}

// KafkaAdapter is the interface for Kafka operations.
type KafkaAdapter interface {
	adapters.Adapter
	Topics(ctx context.Context) ([]TopicInfo, error)
	Brokers(ctx context.Context) ([]BrokerInfo, error)
	ConsumerGroups(ctx context.Context) ([]ConsumerGroupInfo, error)
}

// Adapter is the concrete Kafka adapter.
type Adapter struct {
	mu        sync.RWMutex
	config    Config
	admin     kafkaAdmin
	connected bool
}

// New creates a new Kafka adapter (not yet connected).
func New() *Adapter {
	return &Adapter{}
}

// NewWithAdmin creates an adapter with a custom admin (for testing).
func NewWithAdmin(a kafkaAdmin) *Adapter {
	return &Adapter{admin: a, connected: true}
}

// Connect verifies connectivity to Kafka by dialing the first broker.
func (a *Adapter) Connect(ctx context.Context, source store.Component) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	var configMap map[string]any
	if len(source.Config) > 0 {
		if err := json.Unmarshal(source.Config, &configMap); err != nil {
			return fmt.Errorf("parse component config JSON: %w", err)
		}
	} else {
		configMap = make(map[string]any)
	}

	cfg, err := ParseConfig(configMap)
	if err != nil {
		return fmt.Errorf("parse component config: %w", err)
	}
	a.config = cfg

	// Verify connectivity by dialing the first broker.
	conn, err := kafkago.DialContext(ctx, "tcp", cfg.Brokers[0])
	if err != nil {
		return fmt.Errorf("dial Kafka broker %s: %w", cfg.Brokers[0], err)
	}
	_ = conn.Close()

	c := &kafkago.Client{
		Addr: kafkago.TCP(cfg.Brokers...),
	}
	a.admin = &realKafkaAdmin{c: c}
	a.connected = true
	return nil
}

// Disconnect closes the connection.
func (a *Adapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.admin != nil {
		_ = a.admin.close()
		a.admin = nil
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
			Message:   fmt.Sprintf(statusConnectedFmt, a.config.Brokers),
		}
	}
	return adapters.Status{
		Connected: false,
		Message:   statusNotConnected,
	}
}

// Topics returns topic metadata from the cluster.
func (a *Adapter) Topics(ctx context.Context) ([]TopicInfo, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	topics, _, err := a.admin.fetchMetadata(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch metadata: %w", err)
	}
	return topics, nil
}

// Brokers returns broker metadata from the cluster.
func (a *Adapter) Brokers(ctx context.Context) ([]BrokerInfo, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	_, brokers, err := a.admin.fetchMetadata(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch metadata: %w", err)
	}
	return brokers, nil
}

// ConsumerGroups returns the list of consumer groups.
func (a *Adapter) ConsumerGroups(ctx context.Context) ([]ConsumerGroupInfo, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	groups, err := a.admin.listGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	return groups, nil
}

func (a *Adapter) checkConnected() error {
	if !a.connected {
		return ErrNotConnected
	}
	return nil
}
