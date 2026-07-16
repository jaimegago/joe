package llm

import (
	"context"
	"sync"
	"testing"
)

// fakeAdapter is a minimal LLMAdapter that reports an identity so tests can
// observe which inner adapter a SwappableAdapter delegated to.
type fakeAdapter struct {
	id string
}

func (f *fakeAdapter) Chat(_ context.Context, _ ChatRequest) (*ChatResponse, error) {
	return &ChatResponse{Content: f.id}, nil
}

func TestSwappableAdapter_DelegatesAndSwaps(t *testing.T) {
	sw := NewSwappableAdapter(&fakeAdapter{id: "first"}, "model-a")

	if got := sw.Current(); got != "model-a" {
		t.Fatalf("Current() = %q, want %q", got, "model-a")
	}

	resp, err := sw.Chat(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "first" {
		t.Fatalf("Chat delegated to %q, want %q", resp.Content, "first")
	}

	sw.Swap(&fakeAdapter{id: "second"}, "model-b")

	if got := sw.Current(); got != "model-b" {
		t.Fatalf("after Swap, Current() = %q, want %q", got, "model-b")
	}
	resp, err = sw.Chat(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatalf("Chat after swap: %v", err)
	}
	if resp.Content != "second" {
		t.Fatalf("Chat after swap delegated to %q, want %q", resp.Content, "second")
	}
}

// TestSwappableAdapter_ConcurrentReadDuringSwap exercises the race detector:
// readers must never observe a torn/invalid adapter while a swap is in flight.
func TestSwappableAdapter_ConcurrentReadDuringSwap(t *testing.T) {
	sw := NewSwappableAdapter(&fakeAdapter{id: "0"}, "m0")

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				if _, err := sw.Chat(context.Background(), ChatRequest{}); err != nil {
					t.Errorf("Chat: %v", err)
					return
				}
				_ = sw.Current()
			}
		}()
	}
	for i := 0; i < 50; i++ {
		sw.Swap(&fakeAdapter{id: "n"}, "mN")
	}
	wg.Wait()
}
