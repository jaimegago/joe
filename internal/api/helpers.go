package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jaimegago/joe/internal/adapters"
	awsadapter "github.com/jaimegago/joe/internal/adapters/aws"
	gitadapter "github.com/jaimegago/joe/internal/adapters/git"
	"github.com/jaimegago/joe/internal/adapters/k8s"
)

type apiError struct {
	Error   string         `json:"error"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

var (
	errSourceNotFound    = errors.New("source not found")
	errInvalidSourceType = errors.New("source is not expected adapter type")
)

func writeError(w http.ResponseWriter, status int, code, message string, details ...map[string]any) {
	var payloadDetails map[string]any
	if len(details) > 0 {
		payloadDetails = details[0]
	}
	writeJSON(w, status, apiError{Error: code, Message: message, Details: payloadDetails})
}

func writeInternalError(w http.ResponseWriter, err error, context string) {
	if err != nil {
		slog.Error("api error", "context", context, "error", err)
	}
	writeError(w, http.StatusInternalServerError, errorCodeInternal, internalErrorMessage)
}

func writeBadRequest(w http.ResponseWriter, err error, context, message string) {
	if err != nil {
		slog.Error("api bad request", "context", context, "error", err)
	}
	writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, message)
}

func handleAdapterLookupError(w http.ResponseWriter, err error, sourceID, expected string) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errSourceNotFound) {
		writeError(w, http.StatusNotFound, errorCodeNotFound, fmt.Sprintf("source not found: %s", sourceID), map[string]any{
			"source_id": sourceID,
		})
		return true
	}
	if errors.Is(err, errInvalidSourceType) {
		// Capitalize adapter type for proper display
		displayType := expected
		switch displayType {
		case "aws":
			displayType = "AWS"
		case "k8s":
			displayType = "Kubernetes"
		case "git":
			displayType = "Git"
		}
		article := "a"
		if displayType == "AWS" {
			article = "an"
		}
		writeError(w, http.StatusBadRequest, errorCodeInvalidSource, fmt.Sprintf("source is not %s %s adapter", article, displayType), map[string]any{
			"source_id": sourceID,
			"expected":  expected,
		})
		return true
	}
	writeInternalError(w, err, "adapter lookup")
	return true
}

func (s *Server) getAdapter(sourceID string) (adapters.Adapter, error) {
	adapter, err := s.services.Adapters.Get(sourceID)
	if err != nil {
		if errors.Is(err, adapters.ErrAdapterNotFound) {
			return nil, fmt.Errorf("%w: %s", errSourceNotFound, sourceID)
		}
		return nil, err
	}
	return adapter, nil
}

func (s *Server) getAWSAdapter(sourceID string) (awsadapter.AWSAdapter, error) {
	adapter, err := s.getAdapter(sourceID)
	if err != nil {
		return nil, err
	}
	awsAdapter, ok := adapter.(awsadapter.AWSAdapter)
	if !ok {
		return nil, fmt.Errorf("%w: aws", errInvalidSourceType)
	}
	return awsAdapter, nil
}

func (s *Server) getK8sAdapter(sourceID string) (k8s.KubernetesAdapter, error) {
	adapter, err := s.getAdapter(sourceID)
	if err != nil {
		return nil, err
	}
	k8sAdapter, ok := adapter.(k8s.KubernetesAdapter)
	if !ok {
		return nil, fmt.Errorf("%w: kubernetes", errInvalidSourceType)
	}
	return k8sAdapter, nil
}

func (s *Server) getGitAdapter(sourceID string) (gitadapter.GitAdapter, error) {
	adapter, err := s.getAdapter(sourceID)
	if err != nil {
		return nil, err
	}
	ga, ok := adapter.(gitadapter.GitAdapter)
	if !ok {
		return nil, fmt.Errorf("%w: git", errInvalidSourceType)
	}
	return ga, nil
}
