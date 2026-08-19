package tools

import (
	"fmt"
	"sort"

	"github.com/jaimegago/joe/internal/llm"
)

// Registry manages available tools
type Registry struct {
	tools map[string]Tool
}

// NewRegistry creates a new tool registry
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry
func (r *Registry) Register(tool Tool) {
	r.tools[tool.Name()] = tool
}

// Get retrieves a tool by name
func (r *Registry) Get(name string) (Tool, error) {
	tool, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("tool not found: %s", name)
	}
	return tool, nil
}

// GetAll returns all registered tools
func (r *Registry) GetAll() []Tool {
	tools := make([]Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		tools = append(tools, tool)
	}
	return tools
}

// ToDefinitions converts all registered tools to LLM tool definitions, ordered
// by tool name.
//
// The ordering is load-bearing rather than cosmetic. Tool definitions render at
// position 0 of the provider prompt prefix — ahead of the system prompt and the
// whole accumulated message history — and provider prompt caching is a
// byte-exact prefix match, so a tool order drawn from a Go map range
// invalidates everything behind it whenever the range happens to differ.
//
// The defect did not fire per loop iteration, because the agent loop hoists
// this call once per run. It fired per TURN, because a fresh Registry is
// constructed for every HTTP task, and again within a single run, because the
// tool-intent probe calls this a second time and drew its own order.
//
// Sorting by name is one way to satisfy the property; the invariant requires
// only that the result be a deterministic function of the tool SET —
// identical across independent constructions in one process and across process
// restarts. Registration order is deliberately not the ordering: it would be
// stable per binary while silently changing the prefix whenever a registration
// site moved.
func (r *Registry) ToDefinitions() []llm.ToolDefinition {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)

	definitions := make([]llm.ToolDefinition, 0, len(names))
	for _, name := range names {
		tool := r.tools[name]
		definitions = append(definitions, llm.ToolDefinition{
			Name:        tool.Name(),
			Description: tool.Description(),
			Parameters:  tool.Parameters(),
		})
	}
	return definitions
}
