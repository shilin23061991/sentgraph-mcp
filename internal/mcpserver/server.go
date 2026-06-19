package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/shilin23061991/sentgraph-mcp/internal/memory"
)

type Server struct {
	inner *mcp.Server
}

var (
	closedWorld    = false
	nonDestructive = false
	destructive    = true
)

func New(svc *memory.Service, version string) *Server {
	if svc == nil {
		panic("mcpserver: service cannot be nil")
	}
	s := mcp.NewServer(&mcp.Implementation{Name: "sentgraph", Version: version}, nil)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "memory_context",
		Title:       "Get memory context",
		Description: "Get assembled memory context for the user and current project.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &closedWorld},
	}, contextTool(svc))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "memory_search",
		Title:       "Search memory",
		Description: "Search user or project graph memory for facts, nodes, or episodes.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &closedWorld},
	}, searchTool(svc))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "memory_history",
		Title:       "Get thread history",
		Description: "Get recently stored messages for a thread.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &closedWorld},
	}, historyTool(svc))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "memory_add_messages",
		Title:       "Add conversation messages",
		Description: "Persist user/assistant messages to Zep thread memory.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &nonDestructive, OpenWorldHint: &closedWorld},
	}, addMessagesTool(svc))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "memory_add",
		Title:       "Add memory data",
		Description: "Persist a fact, decision, or project datum to user or project graph memory.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &nonDestructive, OpenWorldHint: &closedWorld},
	}, addDataTool(svc))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "memory_forget",
		Title:       "Forget memory item",
		Description: "Delete a memory edge, node, or episode by UUID.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &destructive, OpenWorldHint: &closedWorld},
	}, forgetTool(svc))

	return &Server{inner: s}
}

func (s *Server) Run(ctx context.Context, t mcp.Transport) error {
	return s.inner.Run(ctx, t)
}

func (s *Server) MCP() *mcp.Server {
	return s.inner
}

type contextInput struct {
	ThreadID string `json:"thread_id" jsonschema:"conversation thread/session id"`
	Query    string `json:"query,omitempty" jsonschema:"optional query to pull project-specific memory"`
	Limit    int    `json:"limit,omitempty" jsonschema:"project memory result limit"`
}

type contextOutput struct {
	Context string `json:"context"`
}

func contextTool(svc *memory.Service) func(context.Context, *mcp.CallToolRequest, contextInput) (*mcp.CallToolResult, contextOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in contextInput) (*mcp.CallToolResult, contextOutput, error) {
		out, err := svc.GetContext(ctx, memory.ContextOptions{ThreadID: in.ThreadID, Query: in.Query, Limit: in.Limit})
		return nil, contextOutput{Context: out}, err
	}
}

type searchInput struct {
	Query  string        `json:"query" jsonschema:"focused natural-language query"`
	Target memory.Target `json:"target,omitempty" jsonschema:"user or project; default project"`
	Scope  string        `json:"scope,omitempty" jsonschema:"edges, nodes, episodes, or auto"`
	Limit  int           `json:"limit,omitempty" jsonschema:"max results; capped at 50"`
}

type searchOutput struct {
	Results []memory.SearchResult `json:"results"`
}

func searchTool(svc *memory.Service) func(context.Context, *mcp.CallToolRequest, searchInput) (*mcp.CallToolResult, searchOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in searchInput) (*mcp.CallToolResult, searchOutput, error) {
		results, err := svc.Search(ctx, memory.SearchRequest{Query: in.Query, Target: in.Target, Scope: in.Scope, Limit: in.Limit})
		return nil, searchOutput{Results: results}, err
	}
}

type historyInput struct {
	ThreadID string `json:"thread_id" jsonschema:"conversation thread/session id"`
	Limit    int    `json:"limit,omitempty" jsonschema:"number of recent messages to return"`
}

type historyOutput struct {
	Messages []memory.Message `json:"messages"`
}

func historyTool(svc *memory.Service) func(context.Context, *mcp.CallToolRequest, historyInput) (*mcp.CallToolResult, historyOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in historyInput) (*mcp.CallToolResult, historyOutput, error) {
		msgs, err := svc.History(ctx, in.ThreadID, in.Limit)
		return nil, historyOutput{Messages: msgs}, err
	}
}

type addMessagesInput struct {
	ThreadID      string           `json:"thread_id" jsonschema:"conversation thread/session id"`
	Messages      []memory.Message `json:"messages" jsonschema:"messages to persist"`
	ReturnContext bool             `json:"return_context,omitempty" jsonschema:"return updated context from Zep"`
}

type addMessagesOutput struct {
	Context string `json:"context,omitempty"`
}

func addMessagesTool(svc *memory.Service) func(context.Context, *mcp.CallToolRequest, addMessagesInput) (*mcp.CallToolResult, addMessagesOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in addMessagesInput) (*mcp.CallToolResult, addMessagesOutput, error) {
		ctxBlock, err := svc.AddTurn(ctx, in.ThreadID, in.Messages, in.ReturnContext)
		return nil, addMessagesOutput{Context: ctxBlock}, err
	}
}

type addDataInput struct {
	Data        string         `json:"data" jsonschema:"fact, decision, JSON, or document chunk to persist"`
	Type        string         `json:"type,omitempty" jsonschema:"text or json; default text"`
	Target      memory.Target  `json:"target,omitempty" jsonschema:"user or project; default project"`
	Description string         `json:"description,omitempty" jsonschema:"source description"`
	Metadata    map[string]any `json:"metadata,omitempty" jsonschema:"optional scalar metadata"`
}

type addDataOutput struct {
	OK bool `json:"ok"`
}

func addDataTool(svc *memory.Service) func(context.Context, *mcp.CallToolRequest, addDataInput) (*mcp.CallToolResult, addDataOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in addDataInput) (*mcp.CallToolResult, addDataOutput, error) {
		err := svc.AddData(ctx, memory.AddDataRequest{
			Data:        in.Data,
			Type:        in.Type,
			Target:      in.Target,
			Description: in.Description,
			Metadata:    in.Metadata,
		})
		return nil, addDataOutput{OK: err == nil}, err
	}
}

type forgetInput struct {
	Kind string `json:"kind" jsonschema:"edge, node, or episode"`
	UUID string `json:"uuid" jsonschema:"Zep item UUID"`
}

type forgetOutput struct {
	OK bool `json:"ok"`
}

func forgetTool(svc *memory.Service) func(context.Context, *mcp.CallToolRequest, forgetInput) (*mcp.CallToolResult, forgetOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in forgetInput) (*mcp.CallToolResult, forgetOutput, error) {
		err := svc.Forget(ctx, in.Kind, in.UUID)
		return nil, forgetOutput{OK: err == nil}, err
	}
}
