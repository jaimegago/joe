package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStreamTask_DispatchesEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != apiTasksStreamPath {
			t.Errorf("path = %q, want %q", r.URL.Path, apiTasksStreamPath)
		}
		if got := r.Header.Get("Accept"); got != "text/event-stream" {
			t.Errorf("Accept = %q, want text/event-stream", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "event: step\ndata: {\"step_number\":1}\n\n")
		fmt.Fprint(w, "event: final\ndata: {\"final_answer\":\"done\",\"status\":\"completed\"}\n\n")
	}))
	defer srv.Close()

	c := New(srv.URL)

	var got []TaskEvent
	err := c.StreamTask(context.Background(), TaskStreamRequest{Message: "hi"}, func(e TaskEvent) error {
		got = append(got, e)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamTask: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2: %+v", len(got), got)
	}
	if got[0].Type != "step" {
		t.Errorf("event[0].Type = %q, want step", got[0].Type)
	}
	if got[1].Type != "final" {
		t.Errorf("event[1].Type = %q, want final", got[1].Type)
	}

	var final struct {
		FinalAnswer string `json:"final_answer"`
		Status      string `json:"status"`
	}
	if err := json.Unmarshal(got[1].Data, &final); err != nil {
		t.Fatalf("decode final: %v", err)
	}
	if final.FinalAnswer != "done" || final.Status != "completed" {
		t.Errorf("final = %+v, want {done completed}", final)
	}
}

func TestStreamTask_PropagatesErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"error":"service_unavailable","message":"LLM not available"}`)
	}))
	defer srv.Close()

	c := New(srv.URL)
	err := c.StreamTask(context.Background(), TaskStreamRequest{Message: "hi"}, func(TaskEvent) error {
		t.Error("onEvent should not be called on error status")
		return nil
	})
	if err == nil {
		t.Fatal("expected error for non-200 status, got nil")
	}
}

func TestStreamTask_OnEventErrorAborts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "event: step\ndata: {}\n\n")
		fmt.Fprint(w, "event: final\ndata: {}\n\n")
	}))
	defer srv.Close()

	c := New(srv.URL)
	sentinel := fmt.Errorf("stop here")
	calls := 0
	err := c.StreamTask(context.Background(), TaskStreamRequest{Message: "hi"}, func(TaskEvent) error {
		calls++
		return sentinel
	})
	if err != sentinel {
		t.Fatalf("err = %v, want sentinel", err)
	}
	if calls != 1 {
		t.Errorf("onEvent called %d times, want 1 (abort on first error)", calls)
	}
}

func TestParseSSE_TrailingEventNoBlankLine(t *testing.T) {
	body := strings.NewReader("event: final\ndata: {\"x\":1}")
	var got []TaskEvent
	if err := parseSSE(body, func(e TaskEvent) error { got = append(got, e); return nil }); err != nil {
		t.Fatalf("parseSSE: %v", err)
	}
	if len(got) != 1 || got[0].Type != "final" {
		t.Fatalf("got %+v, want one final event", got)
	}
}
