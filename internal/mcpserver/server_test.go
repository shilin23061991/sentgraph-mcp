package mcpserver

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/shilin23061991/sentgraph-mcp/internal/config"
	"github.com/shilin23061991/sentgraph-mcp/internal/memory"
)

func TestNewPanicsOnNilService(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	_ = New(nil, "test")
}

func TestToolSurface(t *testing.T) {
	ctx := context.Background()
	svc := memory.New(config.Config{ZepAPIKey: "key", UserID: "dev", ProjectID: "project", ContextTokenBudget: 2000}, &fakeGateway{})
	server := New(svc, "test")

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.MCP().Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	res, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"memory_context":      true,
		"memory_search":       true,
		"memory_history":      true,
		"memory_add_messages": true,
		"memory_add":          true,
		"memory_forget":       true,
	}
	if len(res.Tools) != len(want) {
		t.Fatalf("tool count = %d, want %d", len(res.Tools), len(want))
	}
	for _, tool := range res.Tools {
		if !want[tool.Name] {
			t.Fatalf("unexpected tool %q", tool.Name)
		}
		if tool.Annotations == nil {
			t.Fatalf("tool %q missing annotations", tool.Name)
		}
	}
}

type fakeGateway struct{}

func (f *fakeGateway) EnsureUser(ctx context.Context, userID string) error { return nil }

func (f *fakeGateway) EnsureProjectGraph(ctx context.Context, graphID string) error { return nil }

func (f *fakeGateway) EnsureThread(ctx context.Context, threadID, userID string) error { return nil }

func (f *fakeGateway) AddMessages(ctx context.Context, threadID string, messages []memory.Message, returnContext bool) (string, error) {
	return "", nil
}

func (f *fakeGateway) GetUserContext(ctx context.Context, threadID string) (string, error) {
	return "", nil
}

func (f *fakeGateway) SearchGraph(ctx context.Context, req memory.SearchRequest, userID, graphID string) ([]memory.SearchResult, error) {
	return nil, nil
}

func (f *fakeGateway) AddGraphData(ctx context.Context, req memory.AddDataRequest, userID, graphID string) error {
	return nil
}

func (f *fakeGateway) ThreadHistory(ctx context.Context, threadID string, limit int) ([]memory.Message, error) {
	return nil, nil
}

func (f *fakeGateway) DeleteGraphItem(ctx context.Context, kind, uuid string) error { return nil }
