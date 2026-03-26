package types

import (
	"context"
)

type Input struct {
	Goal                       string
	RecentMessages             []string
	MaxSteps                   int
	RoomID                     string
	RAGSearch                  func(ctx context.Context, roomID string, query string, topK int, vector string) (string, error)
	AIGCGenerate               func(ctx context.Context, prompt string) (string, error)
	DynamicToolSpecs           []ToolSpec
	MCPCallToolByQualifiedName func(ctx context.Context, qualifiedToolName string, arguments map[string]any) (string, error)
}

type ToolSpec struct {
	Name           string
	Purpose        string
	Usage          string
	InputGuideline string
	Aliases        []string
}

type SchemaField struct {
	Name     string
	Type     string
	Required bool
}

type CoordinatorSchemaConfig struct {
	ThoughtField            string
	ActionField             string
	ToolField               string
	InputField              string
	TopLevelFields          []SchemaField
	ActionFields            []SchemaField
	DisallowToolCombination bool
	ToolEnumFromConfig      bool
}

type AgentIdentityConfig struct {
	Name         string
	Role         string
	Mission      string
	Capabilities []string
	Principles   []string
}

type ValidationIssue struct {
	Code    string
	Field   string
	Message string
	Detail  string
}
