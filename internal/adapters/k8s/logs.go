package k8s

import (
	"context"
	"fmt"
	"io"

	corev1 "k8s.io/api/core/v1"
)

const maxTailLines = 1000

// GetPodLogs returns logs from a pod.
func (a *Adapter) GetPodLogs(ctx context.Context, namespace, pod, container string, tailLines int) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if !a.connected {
		return "", fmt.Errorf("adapter not connected")
	}

	if tailLines <= 0 {
		tailLines = 100
	}
	if tailLines > maxTailLines {
		tailLines = maxTailLines
	}

	lines := int64(tailLines)
	opts := &corev1.PodLogOptions{
		TailLines: &lines,
	}
	if container != "" {
		opts.Container = container
	}

	req := a.clientset.CoreV1().Pods(namespace).GetLogs(pod, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("get logs for %s/%s: %w", namespace, pod, err)
	}
	defer stream.Close()

	data, err := io.ReadAll(stream)
	if err != nil {
		return "", fmt.Errorf("read logs: %w", err)
	}

	return string(data), nil
}
