//go:build e2e
// +build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/jaimegago/joe/internal/prompts"
)

// The scripted responder stands in for the LLM. It is an HTTP server in the
// test process that speaks the openai-compat `chat/completions` shape joe's
// openai-compat provider consumes, so joe reaches it through configuration
// alone: no product code knows it exists. No model is in the loop — what the
// responder answers is a script the test sets, and what is under test is
// joe's plumbing from the request to the persisted, rendered outcome.
//
// Anything the script does not answer is rejected loudly: logged, recorded,
// answered with a non-2xx, and reported as a test failure when the harness
// stops. Never a hang, never an empty 200.

// ChatRequest is the part of joe's chat/completions request a script reads.
type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Tools    []ChatTool    `json:"tools,omitempty"`
}

// ChatMessage is one message of a ChatRequest.
type ChatMessage struct {
	Role       string         `json:"role"`
	Content    *string        `json:"content"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

// Text returns the message content, or "" for a null content.
func (m ChatMessage) Text() string {
	if m.Content == nil {
		return ""
	}
	return *m.Content
}

// ChatTool is one tool definition joe offered in a ChatRequest.
type ChatTool struct {
	Type     string `json:"type"`
	Function struct {
		Name string `json:"name"`
	} `json:"function"`
}

// HasTools reports whether joe offered any tools in this request.
func (r ChatRequest) HasTools() bool { return len(r.Tools) > 0 }

// LastMessage returns the final message of the request, or a zero message.
func (r ChatRequest) LastMessage() ChatMessage {
	if len(r.Messages) == 0 {
		return ChatMessage{}
	}
	return r.Messages[len(r.Messages)-1]
}

// IsToolIntentProbe reports whether this is the probe joe sends after any
// reply that carries no tool calls (agentloop's probeUnfulfilledToolIntent).
// It is keyed on the probe text joe sends, so the script follows the probe if
// its wording changes.
func (r ChatRequest) IsToolIntentProbe() bool {
	last := r.LastMessage()
	return last.Role == "user" && last.Text() == prompts.UnfulfilledToolIntentProbe
}

// SystemText returns the concatenated content of the request's system
// messages.
func (r ChatRequest) SystemText() string {
	var b strings.Builder
	for _, m := range r.Messages {
		if m.Role == "system" {
			b.WriteString(m.Text())
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// IsAutoRunPassOver reports whether this request belongs to an Auto Joe run
// whose resolved scope is exactly components. It is keyed on the scope
// section joe writes into a run's system prompt (prompts.AutoRunScopeSection,
// over the sorted set the run resolved), so a script can tell concurrent runs
// apart by the scope a test chose, and follows the section if its wording
// changes.
func (r ChatRequest) IsAutoRunPassOver(components ...string) bool {
	sorted := append([]string(nil), components...)
	sort.Strings(sorted)
	return strings.Contains(r.SystemText(), prompts.AutoRunScopeSection(sorted))
}

// HasToolResult reports whether the conversation already carries a tool
// result, i.e. joe has executed a tool call earlier in this run or session.
func (r ChatRequest) HasToolResult() bool {
	for _, m := range r.Messages {
		if m.Role == "tool" {
			return true
		}
	}
	return false
}

// ToolCall is a tool call a Reply asks joe to make.
type ToolCall struct {
	Name string
	Args map[string]any
}

// Usage is the token usage a Reply reports, so a test can drive joe's token
// accounting — a ceiling, a cost limit — from the responder.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
}

// Reply is what the responder answers to one chat/completions call.
type Reply struct {
	Content   string
	ToolCalls []ToolCall
	Usage     Usage
}

// Script decides the reply to the call-th chat/completions request (1-based).
// Returning an error rejects the request loudly.
type Script func(call int, req ChatRequest) (Reply, error)

// Sequence scripts the replies in order. A request past the end is rejected.
func Sequence(replies ...Reply) Script {
	return func(call int, req ChatRequest) (Reply, error) {
		if call > len(replies) {
			return Reply{}, fmt.Errorf("sequence holds %d replies, this is call %d", len(replies), call)
		}
		return replies[call-1], nil
	}
}

// Default answers for the stub when a test sets no script.
const (
	DefaultToolName = "dns_lookup"
	DefaultAnswer   = "e2e responder: dns_lookup ran, the turn is complete."
)

// DefaultScript is the script the responder runs when a test sets none. It
// completes one agentic turn with one tool call:
//
//   - the no-tool-call probe → plain text, so the turn ends;
//   - tools offered and no tool result yet → one call to dns_lookup for
//     localhost, a Read-class tool that needs no cluster and no component;
//   - tools offered after a tool result → DefaultAnswer as plain text;
//   - no tools offered (a forced synthesis) → DefaultAnswer as plain text.
func DefaultScript(call int, req ChatRequest) (Reply, error) {
	usage := Usage{PromptTokens: 10, CompletionTokens: 5}
	switch {
	case req.IsToolIntentProbe():
		return Reply{Content: "No tool call was intended; the answer stands.", Usage: usage}, nil
	case req.HasTools() && !req.HasToolResult():
		return Reply{
			ToolCalls: []ToolCall{{Name: DefaultToolName, Args: map[string]any{"hostname": "localhost", "record_type": "A"}}},
			Usage:     usage,
		}, nil
	default:
		return Reply{Content: DefaultAnswer, Usage: usage}, nil
	}
}

// RecordedCall is one chat/completions request the responder received, with
// the reply it gave or the reason it rejected it.
type RecordedCall struct {
	Request  ChatRequest
	Reply    Reply
	Rejected string
}

// Responder is the harness-owned scripted LLM endpoint.
type Responder struct {
	t        *testing.T
	server   *http.Server
	listener net.Listener

	mu     sync.Mutex
	script Script
	calls  []RecordedCall
}

// NewResponder starts a responder on an OS-assigned loopback port, running
// DefaultScript until a test sets its own.
func NewResponder(t *testing.T) (*Responder, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("responder listen: %w", err)
	}
	r := &Responder{t: t, listener: ln, script: DefaultScript}
	mux := http.NewServeMux()
	mux.HandleFunc("/", r.handle)
	r.server = &http.Server{Handler: mux}
	go r.server.Serve(ln) //nolint:errcheck // Serve returns ErrServerClosed on Close
	return r, nil
}

// BaseURL is the value joe's openai-compat provider takes as base_url.
func (r *Responder) BaseURL() string {
	return fmt.Sprintf("http://%s/v1", r.listener.Addr().String())
}

// SetScript replaces the script. A test may set it before or after joe starts.
func (r *Responder) SetScript(s Script) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.script = s
}

// Calls returns a copy of every request received so far.
func (r *Responder) Calls() []RecordedCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]RecordedCall(nil), r.calls...)
}

// Rejections returns the calls the responder refused.
func (r *Responder) Rejections() []RecordedCall {
	var out []RecordedCall
	for _, c := range r.Calls() {
		if c.Rejected != "" {
			out = append(out, c)
		}
	}
	return out
}

// Close stops the server.
func (r *Responder) Close() {
	r.server.Close()
}

func (r *Responder) reject(w http.ResponseWriter, rec RecordedCall, reason string) {
	rec.Rejected = reason
	r.mu.Lock()
	r.calls = append(r.calls, rec)
	r.mu.Unlock()
	r.t.Logf("RESPONDER REJECTED a request: %s", reason)
	http.Error(w, "e2e responder rejected the request: "+reason, http.StatusNotImplemented)
}

func (r *Responder) handle(w http.ResponseWriter, httpReq *http.Request) {
	if httpReq.Method != http.MethodPost || httpReq.URL.Path != "/v1/chat/completions" {
		r.reject(w, RecordedCall{}, fmt.Sprintf("unscripted endpoint %s %s", httpReq.Method, httpReq.URL.Path))
		return
	}
	var req ChatRequest
	if err := json.NewDecoder(httpReq.Body).Decode(&req); err != nil {
		r.reject(w, RecordedCall{}, fmt.Sprintf("undecodable request body: %v", err))
		return
	}

	r.mu.Lock()
	call := len(r.calls) + 1
	script := r.script
	r.mu.Unlock()

	rec := RecordedCall{Request: req}
	reply, err := script(call, req)
	if err != nil {
		r.reject(w, rec, fmt.Sprintf("call %d is outside the script: %v", call, err))
		return
	}
	if reply.Content == "" && len(reply.ToolCalls) == 0 {
		r.reject(w, rec, fmt.Sprintf("call %d: the script returned an empty reply", call))
		return
	}

	body, err := encodeReply(call, reply)
	if err != nil {
		r.reject(w, rec, fmt.Sprintf("call %d: %v", call, err))
		return
	}
	rec.Reply = reply
	r.mu.Lock()
	r.calls = append(r.calls, rec)
	r.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.Write(body) //nolint:errcheck // the client hanging up is joe's error to report
}

type wireToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func encodeReply(call int, reply Reply) ([]byte, error) {
	var toolCalls []wireToolCall
	for i, tc := range reply.ToolCalls {
		args, err := json.Marshal(tc.Args)
		if err != nil {
			return nil, fmt.Errorf("marshal args for %q: %w", tc.Name, err)
		}
		var w wireToolCall
		w.ID = fmt.Sprintf("call_%d_%d", call, i)
		w.Type = "function"
		w.Function.Name = tc.Name
		w.Function.Arguments = string(args)
		toolCalls = append(toolCalls, w)
	}
	resp := map[string]any{
		"id":    fmt.Sprintf("e2e-%d", call),
		"model": "e2e-responder",
		"choices": []map[string]any{{
			"index": 0,
			"message": map[string]any{
				"role":       "assistant",
				"content":    reply.Content,
				"tool_calls": toolCalls,
			},
		}},
		"usage": map[string]int{
			"prompt_tokens":     reply.Usage.PromptTokens,
			"completion_tokens": reply.Usage.CompletionTokens,
			"total_tokens":      reply.Usage.PromptTokens + reply.Usage.CompletionTokens,
		},
	}
	return json.Marshal(resp)
}
