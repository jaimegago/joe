package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"

	kafkago "github.com/segmentio/kafka-go"
	listgroupsproto "github.com/segmentio/kafka-go/protocol/listgroups"
	metadataproto "github.com/segmentio/kafka-go/protocol/metadata"

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

// TestConnect_EmptySourceConfig exercises the branch where source.Config is nil/empty,
// causing configMap to be initialised as an empty map (which then fails ParseConfig).
func TestConnect_EmptySourceConfig(t *testing.T) {
	a := New()
	// source.Config == nil → empty configMap → ParseConfig returns error (no brokers).
	src := store.Source{Config: nil}
	err := a.Connect(context.Background(), src)
	if err == nil {
		t.Error("Connect() expected error for empty source config, got nil")
	}
}

// TestConnect_DialFails exercises the DialContext failure branch inside Connect.
// A listener is started then immediately closed so the dial gets "connection refused".
func TestConnect_DialFails(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close() // close before Connect dials so DialContext fails

	a := New()
	src := store.Source{Config: mustMarshal(t, map[string]any{"brokers": []any{addr}})}
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("Connect() expected dial error, got nil")
	}
}

// TestConnect_Success exercises the full success path of Connect by providing
// a TCP listener that accepts the probe connection.
func TestConnect_Success(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().String()

	// Accept connections in the background so DialContext succeeds.
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	a := New()
	src := store.Source{Config: mustMarshal(t, map[string]any{"brokers": []any{addr}})}
	if err := a.Connect(context.Background(), src); err != nil {
		t.Fatalf("Connect() unexpected error: %v", err)
	}
	if !a.Status().Connected {
		t.Error("Status().Connected = false after successful Connect, want true")
	}

	// Clean up.
	_ = a.Disconnect()
}

// TestRealKafkaAdmin_Close exercises the close() method on realKafkaAdmin.
func TestRealKafkaAdmin_Close(t *testing.T) {
	r := &realKafkaAdmin{c: nil}
	if err := r.close(); err != nil {
		t.Errorf("close() error = %v, want nil", err)
	}
}

// cancelledContext returns a context that is already cancelled, guaranteeing
// that any network call made with it fails immediately.
func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// mockTransport implements kafkago.RoundTripper and returns a pre-configured
// response or error, allowing tests to drive realKafkaAdmin without a real broker.
type mockTransport struct {
	resp kafkago.Response
	err  error
}

func (m *mockTransport) RoundTrip(_ context.Context, _ net.Addr, _ kafkago.Request) (kafkago.Response, error) {
	return m.resp, m.err
}

// TestRealKafkaAdmin_FetchMetadata_Success exercises the happy-path of
// fetchMetadata including the t.Error != nil skip branch.
func TestRealKafkaAdmin_FetchMetadata_Success(t *testing.T) {
	metaResp := &metadataproto.Response{
		Brokers: []metadataproto.ResponseBroker{
			{NodeID: 1, Host: "kafka1", Port: 9092},
		},
		Topics: []metadataproto.ResponseTopic{
			// ErrorCode == 0 → included
			{ErrorCode: 0, Name: "orders", IsInternal: false, Partitions: []metadataproto.ResponsePartition{{}}},
			// ErrorCode != 0 → skipped
			{ErrorCode: 3, Name: "bad-topic"},
		},
	}
	transport := &mockTransport{resp: metaResp}
	c := &kafkago.Client{
		Addr:      kafkago.TCP("127.0.0.1:9092"),
		Transport: transport,
	}
	r := &realKafkaAdmin{c: c}

	topics, brokers, err := r.fetchMetadata(context.Background())
	if err != nil {
		t.Fatalf("fetchMetadata() unexpected error: %v", err)
	}
	// "bad-topic" is skipped because its ErrorCode != 0.
	if len(topics) != 1 {
		t.Errorf("len(topics) = %d, want 1", len(topics))
	}
	if topics[0].Name != "orders" {
		t.Errorf("topics[0].Name = %q, want orders", topics[0].Name)
	}
	if len(brokers) != 1 {
		t.Errorf("len(brokers) = %d, want 1", len(brokers))
	}
	if brokers[0].Host != "kafka1" {
		t.Errorf("brokers[0].Host = %q, want kafka1", brokers[0].Host)
	}
}

// TestRealKafkaAdmin_ListGroups_Success exercises the happy-path of listGroups.
func TestRealKafkaAdmin_ListGroups_Success(t *testing.T) {
	lgResp := &listgroupsproto.Response{
		ErrorCode: 0,
		Groups: []listgroupsproto.ResponseGroup{
			{GroupID: "g1", ProtocolType: "consumer"},
			{GroupID: "g2", ProtocolType: "connect"},
		},
	}
	transport := &mockTransport{resp: lgResp}
	c := &kafkago.Client{
		Addr:      kafkago.TCP("127.0.0.1:9092"),
		Transport: transport,
	}
	r := &realKafkaAdmin{c: c}

	groups, err := r.listGroups(context.Background())
	if err != nil {
		t.Fatalf("listGroups() unexpected error: %v", err)
	}
	if len(groups) != 2 {
		t.Errorf("len(groups) = %d, want 2", len(groups))
	}
	if groups[0].GroupID != "g1" {
		t.Errorf("groups[0].GroupID = %q, want g1", groups[0].GroupID)
	}
}

// TestRealKafkaAdmin_ListGroups_RespError exercises the resp.Error != nil branch.
func TestRealKafkaAdmin_ListGroups_RespError(t *testing.T) {
	// ErrorCode 15 = GroupAuthorizationFailed — non-zero triggers makeError.
	lgResp := &listgroupsproto.Response{
		ErrorCode: 15,
	}
	transport := &mockTransport{resp: lgResp}
	c := &kafkago.Client{
		Addr:      kafkago.TCP("127.0.0.1:9092"),
		Transport: transport,
	}
	r := &realKafkaAdmin{c: c}

	groups, err := r.listGroups(context.Background())
	if err == nil {
		t.Error("listGroups() expected error for non-zero ErrorCode, got nil")
	}
	if groups != nil {
		t.Errorf("listGroups() groups = %v, want nil on error", groups)
	}
}

// TestRealKafkaAdmin_FetchMetadata_Error exercises the error path of
// fetchMetadata by providing a context that is already cancelled.
func TestRealKafkaAdmin_FetchMetadata_Error(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	addr := ln.Addr().String()
	defer ln.Close()

	c := &kafkago.Client{Addr: kafkago.TCP(addr)}
	r := &realKafkaAdmin{c: c}

	topics, brokers, err := r.fetchMetadata(cancelledContext())
	if err == nil {
		t.Error("fetchMetadata() expected error for cancelled context, got nil")
	}
	if topics != nil {
		t.Errorf("fetchMetadata() topics = %v, want nil on error", topics)
	}
	if brokers != nil {
		t.Errorf("fetchMetadata() brokers = %v, want nil on error", brokers)
	}
}

// TestRealKafkaAdmin_ListGroups_Error exercises the error path of listGroups
// by providing a context that is already cancelled.
func TestRealKafkaAdmin_ListGroups_Error(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	addr := ln.Addr().String()
	defer ln.Close()

	c := &kafkago.Client{Addr: kafkago.TCP(addr)}
	r := &realKafkaAdmin{c: c}

	groups, err := r.listGroups(cancelledContext())
	if err == nil {
		t.Error("listGroups() expected error for cancelled context, got nil")
	}
	if groups != nil {
		t.Errorf("listGroups() groups = %v, want nil on error", groups)
	}
}
