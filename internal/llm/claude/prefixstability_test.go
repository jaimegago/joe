package claude

import (
	"context"
	"encoding/json"
	"math/rand"
	"sort"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/tools"
)

// This file is the enforcement test for the prompt-prefix stability invariant:
//
//	The serialized tool definitions and the concatenation of stable system
//	segments are byte-identical across independent constructions — within one
//	process and across process restarts.
//
// It lives in the claude package rather than in internal/tools because the
// bytes that matter are the ones a provider actually receives. Serializing
// []llm.ToolDefinition with encoding/json would catch a reordered slice but
// would say nothing about the SDK's own marshaller, which is the thing on the
// wire.
//
// The invariant is deliberately NOT extended to the message region. Any
// history reduction edits the front (Session.pruneMessages drops from the
// oldest end), so message-prefix stability and pruning are in direct conflict
// and context limits mean joe cannot choose stability. Excluding the message
// region is what makes the tools-plus-stable-system guarantee testable in
// isolation.

// prefixStabilityConstructions is N: how many independent registries the test
// builds. See the probability argument on the sorted-order assertion below.
const prefixStabilityConstructions = 64

// stubTool is a tool.Tool that exists only to occupy a name in a registry.
type stubTool struct {
	name string
}

func (s stubTool) Name() string        { return s.name }
func (s stubTool) Description() string { return "description of " + s.name }

// Parameters returns a multi-key property map on purpose. Properties are
// handed to the SDK as a Go map rather than a slice, so this is the second
// place an unordered range could enter the prefix, and the test would not see
// it with a single-key schema.
func (s stubTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"zulu":     {Type: "string", Description: "last by name, first if insertion-ordered"},
			"alpha":    {Type: "string", Description: "first by name"},
			"mike":     {Type: "integer", Description: "middle"},
			"delta":    {Type: "boolean", Description: "another"},
			"november": {Type: "string", Description: "and another"},
		},
		Required: []string{"alpha"},
	}
}

func (s stubTool) Execute(context.Context, map[string]any) (any, error) { return nil, nil }

// prefixToolNames is the fixed tool SET. Registration order is shuffled per
// construction, so any ordering the registry derives from insertion order —
// or from a map range — varies across constructions while the set does not.
var prefixToolNames = []string{
	"query_metrics", "search_logs", "get_component", "list_components",
	"run_command", "read_file", "http_request", "dns_query",
	"trace_route", "net_check", "web_search", "describe_pod",
	"get_alerts", "list_zones", "graph_query", "explain_incident",
}

// buildRegistry returns a fresh registry holding exactly prefixToolNames,
// registered in the given order.
func buildRegistry(order []string) *tools.Registry {
	reg := tools.NewRegistry()
	for _, name := range order {
		reg.Register(stubTool{name: name})
	}
	return reg
}

// serializedPrefix renders the cacheable prefix region the way the adapter
// does: the tool definitions through the SDK's own tool conversion, plus the
// stable leading run of the system prompt. It is deliberately the SDK's
// marshaller and not encoding/json on joe's own structs.
func serializedPrefix(t *testing.T, reg *tools.Registry, sys llm.SystemPrompt) []byte {
	t.Helper()

	c := &Client{}
	defs := reg.ToDefinitions()
	toolParams := make([]anthropic.ToolUnionParam, 0, len(defs))
	for _, def := range defs {
		toolParams = append(toolParams, c.convertToolDefinition(def))
	}

	payload := struct {
		Tools  []anthropic.ToolUnionParam `json:"tools"`
		System string                     `json:"system"`
	}{Tools: toolParams, System: sys.StablePrefix()}

	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal prefix: %v", err)
	}
	return b
}

// stableSystem is a stand-in for the task handler's assembly: one stable
// segment followed by volatile ones. Only the leading stable run may enter the
// cacheable prefix.
func stableSystem(turn int) llm.SystemPrompt {
	sys := llm.StaticSystem("You are Joe, an SRE agent. Static instructions.")
	sys = sys.Append("Zone scope: zone-a, zone-b", false)
	sys = sys.Append("Infrastructure graph: 41 nodes, 77 edges", false)
	// Query-derived skills differ per turn — the tail volatility that makes an
	// end-of-system breakpoint useless.
	sys = sys.Append(strings.Repeat("skill-for-this-query ", turn+1), false)
	return sys
}

// TestPrefixIsByteStableAcrossIndependentConstructions is the invariant.
func TestPrefixIsByteStableAcrossIndependentConstructions(t *testing.T) {
	rng := rand.New(rand.NewSource(20260819))

	var want []byte
	for i := 0; i < prefixStabilityConstructions; i++ {
		order := append([]string(nil), prefixToolNames...)
		rng.Shuffle(len(order), func(a, b int) { order[a], order[b] = order[b], order[a] })

		// A different volatile tail on every construction. The prefix must not
		// move when it changes; if StablePrefix ever started returning the
		// whole prompt, this is what would catch it.
		got := serializedPrefix(t, buildRegistry(order), stableSystem(i))

		if i == 0 {
			want = got
			continue
		}
		if string(got) != string(want) {
			t.Fatalf("prefix differs on construction %d despite an identical tool set\n"+
				"registration order was %v\nwant %s\ngot  %s", i, order, want, got)
		}
	}
}

// TestToolDefinitionsAreSortedByName is the assertion that makes the test
// above fail when an unordered map range is reintroduced, rather than merely
// making it likely to.
//
// Byte-identity across N constructions is the invariant, but on its own it is
// a probabilistic detector: a map range could in principle yield the same
// order every time. Sorted order is a specific, checkable witness that the
// result is a deterministic function of the tool SET, and a Go map range
// satisfies it only by coincidence. With 16 tools the chance of one range
// landing sorted is 1/16!, about 5e-14, and the test above independently
// requires it 64 times over.
//
// The mechanism is not mandated — any deterministic function of the set would
// satisfy the invariant — so a future change that replaces sorting with
// another deterministic ordering is expected to update this test's witness. It
// is not expected to delete it.
func TestToolDefinitionsAreSortedByName(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	order := append([]string(nil), prefixToolNames...)
	rng.Shuffle(len(order), func(a, b int) { order[a], order[b] = order[b], order[a] })

	defs := buildRegistry(order).ToDefinitions()
	if len(defs) != len(prefixToolNames) {
		t.Fatalf("got %d definitions, want %d", len(defs), len(prefixToolNames))
	}

	got := make([]string, len(defs))
	for i, d := range defs {
		got[i] = d.Name
	}
	want := append([]string(nil), prefixToolNames...)
	sort.Strings(want)

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tool definitions are not a deterministic function of the tool set:\n"+
				"position %d is %q, want %q\nfull order: %v", i, got[i], want[i], got)
		}
	}
}

// TestBreakpointSitsAtTheStableVolatileBoundary pins the placement rule. The
// trap this whole design exists to avoid is a breakpoint at the END of the
// system prompt, where joe's query-derived skills sit: it would write a fresh
// entry every turn and read one on none.
func TestBreakpointSitsAtTheStableVolatileBoundary(t *testing.T) {
	sys := stableSystem(0)
	blocks := systemBlocks(sys)

	if len(blocks) != 2 {
		t.Fatalf("got %d system blocks, want 2 (stable head + volatile tail)", len(blocks))
	}
	if blocks[0].CacheControl.Type == "" {
		t.Fatal("no cache_control breakpoint on the stable head block")
	}
	if blocks[1].CacheControl.Type != "" {
		t.Fatal("cache_control on the volatile tail: the breakpoint is at end-of-system, " +
			"which writes a fresh entry every turn and reads one on none")
	}
	if blocks[0].Text != sys.StablePrefix() {
		t.Fatalf("breakpoint is not at the declared boundary:\nblock 0 = %q\nstable prefix = %q",
			blocks[0].Text, sys.StablePrefix())
	}

	// Segmenting must change what is cached and nothing about what the model
	// reads.
	if joined := blocks[0].Text + blocks[1].Text; joined != sys.String() {
		t.Fatalf("blocks do not reconstruct the prompt:\ngot  %q\nwant %q", joined, sys.String())
	}
}

// TestStablePrefixStopsAtTheFirstVolatileSegment pins the prefix-match
// property: a stable segment behind a volatile one is part of no stable
// prefix, however stable its own bytes are.
func TestStablePrefixStopsAtTheFirstVolatileSegment(t *testing.T) {
	sys := llm.StaticSystem("static head")
	sys = sys.Append("volatile middle", false)
	sys = sys.Append("stable but unreachable", true)

	if got, want := sys.StablePrefix(), "static head"; got != want {
		t.Fatalf("StablePrefix() = %q, want %q — a stable segment behind a volatile one "+
			"is not part of a byte-exact prefix", got, want)
	}
}

// TestAllStableSystemEmitsOneCachedBlock covers the degenerate case: when
// every segment is stable the boundary genuinely IS the end of the prompt.
// That is the declared boundary, not the end-of-prompt default.
func TestAllStableSystemEmitsOneCachedBlock(t *testing.T) {
	blocks := systemBlocks(llm.StaticSystem("only a constant"))
	if len(blocks) != 1 {
		t.Fatalf("got %d blocks, want 1", len(blocks))
	}
	if blocks[0].CacheControl.Type == "" {
		t.Fatal("no breakpoint on an entirely stable system prompt")
	}
}

// TestNoStablePrefixEmitsNoBreakpoint covers the other degenerate case:
// nothing cacheable leads the prompt, so nothing is marked. Writing a cache
// entry there would pay the write premium for a prefix that never repeats.
func TestNoStablePrefixEmitsNoBreakpoint(t *testing.T) {
	var sys llm.SystemPrompt
	sys = sys.Append("entirely volatile", false)

	blocks := systemBlocks(sys)
	if len(blocks) != 1 {
		t.Fatalf("got %d blocks, want 1", len(blocks))
	}
	if blocks[0].CacheControl.Type != "" {
		t.Fatal("breakpoint written for a prompt with no stable prefix")
	}
}

// TestEmptySystemEmitsNoBlocks preserves the pre-change behaviour that an
// absent system prompt sets no System field at all.
func TestEmptySystemEmitsNoBlocks(t *testing.T) {
	if blocks := systemBlocks(nil); blocks != nil {
		t.Fatalf("got %d blocks for an empty system prompt, want none", len(blocks))
	}
	if blocks := systemBlocks(llm.StaticSystem("")); blocks != nil {
		t.Fatalf("got %d blocks for an empty system prompt, want none", len(blocks))
	}
}
