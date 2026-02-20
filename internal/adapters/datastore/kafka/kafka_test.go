package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jaimegago/joe/internal/store"
)

// mockAdmin implements the kafkaAdmin interface for testing.
type mockAdmin struct {
	metaTopics  []TopicInfo
	metaBrokers []BrokerInfo
	metaErr     error
	groups      []ConsumerGroupInfo
	groupsErr   error
	closed      bool
}

func (m *mockAdmin) fetchMetadata(_ context.Context) ([]TopicInfo, []BrokerInfo, error) {
	return m.metaTopics, m.metaBrokers, m.metaErr
}

func (m *mockAdmin) listGroups(_ context.Context) ([]ConsumerGroupInfo, error) {
	return m.groups, m.groupsErr
}

func (m *mockAdmin) close() error {
	m.closed = true
	return nil
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return data
}

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name    string
		input   map[string]any
		wantErr bool
	}{
		{
			name:  "valid config",
			input: map[string]any{"brokers": []any{"localhost:9092"}},
		},
		{
			name:  "multiple brokers",
			input: map[string]any{"brokers": []any{"b1:9092", "b2:9092"}},
		},
		{
			name:    "missing brokers",
			input:   map[string]any{},
			wantErr: true,
		},
		{
			name:    "empty brokers list",
			input:   map[string]any{"brokers": []any{}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ParseConfig(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(cfg.Brokers) == 0 {
				t.Error("Brokers should not be empty on valid config")
			}
		})
	}
}

func TestConnect_InvalidConfig(t *testing.T) {
	a := New()
	src := store.Source{Config: mustMarshal(t, map[string]any{})} // missing brokers
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("Connect() expected error for missing brokers, got nil")
	}
}

func TestConnect_BadJSON(t *testing.T) {
	a := New()
	src := store.Source{Config: []byte(`{bad`)}
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("Connect() expected error for bad JSON, got nil")
	}
}

func TestStatus_NotConnected(t *testing.T) {
	a := New()
	s := a.Status()
	if s.Connected {
		t.Error("Status().Connected = true, want false")
	}
}

func TestStatus_Connected(t *testing.T) {
	m := &mockAdmin{}
	a := NewWithAdmin(m)
	s := a.Status()
	if !s.Connected {
		t.Error("Status().Connected = false, want true")
	}
}

func TestDisconnect(t *testing.T) {
	m := &mockAdmin{}
	a := NewWithAdmin(m)
	if err := a.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if a.Status().Connected {
		t.Error("Status().Connected = true after disconnect, want false")
	}
	if !m.closed {
		t.Error("close() was not called on the admin")
	}
}

func TestTopics_NotConnected(t *testing.T) {
	a := New()
	_, err := a.Topics(context.Background())
	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("Topics() error = %v, want ErrNotConnected", err)
	}
}

func TestTopics_Success(t *testing.T) {
	topics := []TopicInfo{
		{Name: "orders", Partitions: 3, Internal: false},
		{Name: "__consumer_offsets", Partitions: 50, Internal: true},
	}
	m := &mockAdmin{metaTopics: topics, metaBrokers: nil}
	a := NewWithAdmin(m)

	result, err := a.Topics(context.Background())
	if err != nil {
		t.Fatalf("Topics() error = %v", err)
	}
	if len(result) != 2 {
		t.Errorf("len(result) = %d, want 2", len(result))
	}
	if result[0].Name != "orders" {
		t.Errorf("result[0].Name = %q, want orders", result[0].Name)
	}
	if result[0].Partitions != 3 {
		t.Errorf("result[0].Partitions = %d, want 3", result[0].Partitions)
	}
}

func TestTopics_Error(t *testing.T) {
	m := &mockAdmin{metaErr: errors.New("broker unavailable")}
	a := NewWithAdmin(m)
	_, err := a.Topics(context.Background())
	if err == nil {
		t.Error("Topics() expected error, got nil")
	}
}

func TestBrokers_NotConnected(t *testing.T) {
	a := New()
	_, err := a.Brokers(context.Background())
	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("Brokers() error = %v, want ErrNotConnected", err)
	}
}

func TestBrokers_Success(t *testing.T) {
	brokers := []BrokerInfo{
		{ID: 1, Host: "kafka1", Port: 9092},
		{ID: 2, Host: "kafka2", Port: 9092},
	}
	m := &mockAdmin{metaBrokers: brokers}
	a := NewWithAdmin(m)

	result, err := a.Brokers(context.Background())
	if err != nil {
		t.Fatalf("Brokers() error = %v", err)
	}
	if len(result) != 2 {
		t.Errorf("len(result) = %d, want 2", len(result))
	}
	if result[0].Host != "kafka1" {
		t.Errorf("result[0].Host = %q, want kafka1", result[0].Host)
	}
}

func TestBrokers_Error(t *testing.T) {
	m := &mockAdmin{metaErr: errors.New("timeout")}
	a := NewWithAdmin(m)
	_, err := a.Brokers(context.Background())
	if err == nil {
		t.Error("Brokers() expected error, got nil")
	}
}

func TestConsumerGroups_NotConnected(t *testing.T) {
	a := New()
	_, err := a.ConsumerGroups(context.Background())
	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("ConsumerGroups() error = %v, want ErrNotConnected", err)
	}
}

func TestConsumerGroups_Success(t *testing.T) {
	groups := []ConsumerGroupInfo{
		{GroupID: "payment-consumer", Protocol: "consumer"},
		{GroupID: "analytics-consumer", Protocol: "consumer"},
	}
	m := &mockAdmin{groups: groups}
	a := NewWithAdmin(m)

	result, err := a.ConsumerGroups(context.Background())
	if err != nil {
		t.Fatalf("ConsumerGroups() error = %v", err)
	}
	if len(result) != 2 {
		t.Errorf("len(result) = %d, want 2", len(result))
	}
	if result[0].GroupID != "payment-consumer" {
		t.Errorf("result[0].GroupID = %q, want payment-consumer", result[0].GroupID)
	}
}

func TestConsumerGroups_Error(t *testing.T) {
	m := &mockAdmin{groupsErr: errors.New("unauthorized")}
	a := NewWithAdmin(m)
	_, err := a.ConsumerGroups(context.Background())
	if err == nil {
		t.Error("ConsumerGroups() expected error, got nil")
	}
}

func TestTopics_EmptyResult(t *testing.T) {
	m := &mockAdmin{metaTopics: []TopicInfo{}, metaBrokers: []BrokerInfo{}}
	a := NewWithAdmin(m)
	result, err := a.Topics(context.Background())
	if err != nil {
		t.Fatalf("Topics() error = %v", err)
	}
	if len(result) != 0 {
		t.Errorf("len(result) = %d, want 0", len(result))
	}
}

func TestBrokers_EmptyResult(t *testing.T) {
	m := &mockAdmin{metaTopics: []TopicInfo{}, metaBrokers: []BrokerInfo{}}
	a := NewWithAdmin(m)
	result, err := a.Brokers(context.Background())
	if err != nil {
		t.Fatalf("Brokers() error = %v", err)
	}
	if len(result) != 0 {
		t.Errorf("len(result) = %d, want 0", len(result))
	}
}

func TestConsumerGroups_Empty(t *testing.T) {
	m := &mockAdmin{groups: []ConsumerGroupInfo{}}
	a := NewWithAdmin(m)
	result, err := a.ConsumerGroups(context.Background())
	if err != nil {
		t.Fatalf("ConsumerGroups() error = %v", err)
	}
	if len(result) != 0 {
		t.Errorf("len(result) = %d, want 0", len(result))
	}
}

func TestStatus_Message(t *testing.T) {
	m := &mockAdmin{}
	a := NewWithAdmin(m)
	// Verify the status message contains meaningful info when connected.
	// We set config via the fact that NewWithAdmin sets connected=true but no config.
	s := a.Status()
	if !s.Connected {
		t.Error("Status().Connected = false, want true")
	}
	if s.Message == "" {
		t.Error("Status().Message should not be empty when connected")
	}
}

func TestDisconnect_NoAdmin(t *testing.T) {
	// Disconnect when admin is nil should not panic.
	a := New()
	if err := a.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
}
