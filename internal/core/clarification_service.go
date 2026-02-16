package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
)

// GraphOperation represents a single operation to apply to the graph
type GraphOperation struct {
	Type     string         `json:"type"` // "add_node", "add_edge", "delete_node", "delete_edge"
	Node     *graph.Node    `json:"node,omitempty"`
	Edge     *graph.Edge    `json:"edge,omitempty"`
	NodeID   string         `json:"node_id,omitempty"`
	From     string         `json:"from,omitempty"`
	To       string         `json:"to,omitempty"`
	Relation string         `json:"relation,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// GraphOperations represents a batch of operations with metadata
type GraphOperations struct {
	Operations []GraphOperation    `json:"operations"`
	Provenance OperationProvenance `json:"provenance"`
}

// OperationProvenance tracks who confirmed and when
type OperationProvenance struct {
	ConfirmedBy     string `json:"confirmed_by"`     // user who answered
	ConfirmedAt     string `json:"confirmed_at"`     // timestamp
	ClarificationID string `json:"clarification_id"` // which clarification
	Confidence      string `json:"confidence"`       // e.g., "confirmed", "user_approved"
}

// ClarificationService handles clarification-related operations
type ClarificationService struct {
	graphStore graph.GraphStore
	store      *store.Store
}

// NewClarificationService creates a new service
func NewClarificationService(graphStore graph.GraphStore, store *store.Store) *ClarificationService {
	return &ClarificationService{
		graphStore: graphStore,
		store:      store,
	}
}

// ApplyAnswer applies graph operations from an answered clarification
func (s *ClarificationService) ApplyAnswer(ctx context.Context, clarificationID string, answer string, answeredBy string) error {
	if s.store == nil || s.store.Clarifications == nil {
		return fmt.Errorf("store not available for clarification %s", clarificationID)
	}

	// Get the clarification to fetch stored operations
	clarification, err := s.store.Clarifications.Get(ctx, clarificationID)
	if err != nil {
		return fmt.Errorf("failed to fetch clarification %s: %w", clarificationID, err)
	}

	if clarification == nil {
		return fmt.Errorf("clarification %s not found", clarificationID)
	}

	// If no graph operations stored, nothing to apply
	if len(clarification.GraphOperations) == 0 {
		return nil
	}

	// Parse graph operations
	var ops GraphOperations
	if err := json.Unmarshal(clarification.GraphOperations, &ops); err != nil {
		return fmt.Errorf("parse graph operations for clarification %s: %w", clarificationID, err)
	}

	// Set provenance
	ops.Provenance.ConfirmedBy = answeredBy
	ops.Provenance.ConfirmedAt = clarification.AnsweredAt.String()
	ops.Provenance.ClarificationID = clarificationID
	ops.Provenance.Confidence = "confirmed"

	// Apply each operation
	var opErrors []error
	for i, op := range ops.Operations {
		if err := s.applyOperation(ctx, &op, &ops.Provenance); err != nil {
			opErrors = append(opErrors, fmt.Errorf("apply graph operation %d (%s) for clarification %s: %w", i, op.Type, clarificationID, err))
			// Continue applying other operations and return aggregated errors.
			continue
		}
	}

	if len(opErrors) > 0 {
		return errors.Join(opErrors...)
	}

	return nil
}

// applyOperation executes a single graph operation
func (s *ClarificationService) applyOperation(ctx context.Context, op *GraphOperation, provenance *OperationProvenance) error {
	if s.graphStore == nil {
		return fmt.Errorf("graph store not available")
	}

	switch op.Type {
	case "add_node":
		if op.Node == nil {
			return fmt.Errorf("add_node operation missing node data")
		}
		// Record provenance on the node
		if op.Node.Metadata == nil {
			op.Node.Metadata = make(map[string]any)
		}
		op.Node.Metadata["provenance"] = map[string]any{
			"confirmed_by":     provenance.ConfirmedBy,
			"confirmed_at":     provenance.ConfirmedAt,
			"clarification_id": provenance.ClarificationID,
			"confidence":       provenance.Confidence,
		}
		return s.graphStore.AddNode(ctx, *op.Node)

	case "add_edge":
		if op.Edge == nil {
			return fmt.Errorf("add_edge operation missing edge data")
		}
		// Mark edges confirmed by user as UserConfirmed
		op.Edge.Confidence = graph.UserConfirmed
		return s.graphStore.AddEdge(ctx, *op.Edge)

	case "delete_node":
		if op.NodeID == "" {
			return fmt.Errorf("delete_node operation missing node_id")
		}
		return s.graphStore.DeleteNode(ctx, op.NodeID)

	case "delete_edge":
		if op.From == "" || op.To == "" || op.Relation == "" {
			return fmt.Errorf("delete_edge operation missing from/to/relation")
		}
		return s.graphStore.DeleteEdge(ctx, op.From, op.To, op.Relation)

	default:
		return fmt.Errorf("unknown graph operation type: %s", op.Type)
	}
}
