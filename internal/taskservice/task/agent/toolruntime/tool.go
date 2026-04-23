package toolruntime

import "context"

// Tool is the interface every built-in tool must implement.
type Tool interface {
	Name() string
	Spec() ToolDescriptor
	Validate(input string) *ValidationError
	Run(ctx context.Context, input string, roomID string) (string, error)
}

// Registry holds all registered built-in tools.
type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

func (r *Registry) Register(t Tool) {
	r.tools[t.Name()] = t
}

func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) Descriptors() []ToolDescriptor {
	out := make([]ToolDescriptor, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t.Spec())
	}
	return out
}
