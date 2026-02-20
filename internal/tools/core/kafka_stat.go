package core

import (
	"context"
	"fmt"

	kafkaadapter "github.com/jaimegago/joe/internal/adapters/datastore/kafka"
	"github.com/jaimegago/joe/internal/llm"
)

// KafkaClient defines the subset of client.Client needed for Kafka tools.
type KafkaClient interface {
	KafkaTopics(ctx context.Context, sourceID string) ([]kafkaadapter.TopicInfo, error)
	KafkaBrokers(ctx context.Context, sourceID string) ([]kafkaadapter.BrokerInfo, error)
	KafkaConsumerGroups(ctx context.Context, sourceID string) ([]kafkaadapter.ConsumerGroupInfo, error)
}

// KafkaTopicsTool lists Kafka topics via joecored.
type KafkaTopicsTool struct {
	Client KafkaClient
}

// NewKafkaTopicsTool creates a new kafka_topics tool.
func NewKafkaTopicsTool(c KafkaClient) *KafkaTopicsTool {
	return &KafkaTopicsTool{Client: c}
}

func (t *KafkaTopicsTool) Name() string { return "kafka_topics" }

func (t *KafkaTopicsTool) Description() string {
	return "List all Kafka topics with their partition count, replication factor, and configuration. " +
		"Use this to discover topics, check replication health, and understand partition layout. " +
		"If you don't know the source_id, call list_sources first."
}

func (t *KafkaTopicsTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"source_id": {
				Type:        "string",
				Description: "ID of the Kafka source to query.",
			},
		},
		Required: []string{"source_id"},
	}
}

func (t *KafkaTopicsTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["source_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: source_id")
	}

	topics, err := t.Client.KafkaTopics(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("kafka topics: %w", err)
	}

	if topics == nil {
		topics = []kafkaadapter.TopicInfo{}
	}

	return map[string]any{
		"topics":    topics,
		"count":     len(topics),
		"source_id": sourceID,
	}, nil
}

// KafkaBrokersTool lists Kafka brokers via joecored.
type KafkaBrokersTool struct {
	Client KafkaClient
}

// NewKafkaBrokersTool creates a new kafka_brokers tool.
func NewKafkaBrokersTool(c KafkaClient) *KafkaBrokersTool {
	return &KafkaBrokersTool{Client: c}
}

func (t *KafkaBrokersTool) Name() string { return "kafka_brokers" }

func (t *KafkaBrokersTool) Description() string {
	return "List all Kafka brokers in the cluster with their ID, host, port, and rack information. " +
		"Use this to inspect cluster topology and identify broker count and placement. " +
		"If you don't know the source_id, call list_sources first."
}

func (t *KafkaBrokersTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"source_id": {
				Type:        "string",
				Description: "ID of the Kafka source to query.",
			},
		},
		Required: []string{"source_id"},
	}
}

func (t *KafkaBrokersTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["source_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: source_id")
	}

	brokers, err := t.Client.KafkaBrokers(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("kafka brokers: %w", err)
	}

	if brokers == nil {
		brokers = []kafkaadapter.BrokerInfo{}
	}

	return map[string]any{
		"brokers":   brokers,
		"count":     len(brokers),
		"source_id": sourceID,
	}, nil
}

// KafkaConsumerGroupsTool lists Kafka consumer groups and their lag via joecored.
type KafkaConsumerGroupsTool struct {
	Client KafkaClient
}

// NewKafkaConsumerGroupsTool creates a new kafka_consumers tool.
func NewKafkaConsumerGroupsTool(c KafkaClient) *KafkaConsumerGroupsTool {
	return &KafkaConsumerGroupsTool{Client: c}
}

func (t *KafkaConsumerGroupsTool) Name() string { return "kafka_consumers" }

func (t *KafkaConsumerGroupsTool) Description() string {
	return "List all Kafka consumer groups with their state, protocol, and per-partition offset lag. " +
		"Total lag per group is included. Use this to identify consumer groups that are falling behind " +
		"and to understand partition offset consumption. " +
		"If you don't know the source_id, call list_sources first."
}

func (t *KafkaConsumerGroupsTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"source_id": {
				Type:        "string",
				Description: "ID of the Kafka source to query.",
			},
		},
		Required: []string{"source_id"},
	}
}

func (t *KafkaConsumerGroupsTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["source_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: source_id")
	}

	groups, err := t.Client.KafkaConsumerGroups(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("kafka consumer groups: %w", err)
	}

	if groups == nil {
		groups = []kafkaadapter.ConsumerGroupInfo{}
	}

	return map[string]any{
		"groups":    groups,
		"count":     len(groups),
		"source_id": sourceID,
	}, nil
}
