package memory

import (
	"context"
	"strings"
	"testing"

	"github.com/shilin23061991/sentgraph-mcp/internal/config"
)

func TestEnsureIdentityCreatesUserGraphAndThread(t *testing.T) {
	gw := &fakeGateway{}
	svc := New(testConfig(), gw)

	if err := svc.EnsureIdentity(context.Background(), "thread-1"); err != nil {
		t.Fatal(err)
	}

	if gw.userID != "dev-1" || gw.graphID != "proj:alpha" || gw.threadID != "thread-1" {
		t.Fatalf("unexpected identity calls: %+v", gw)
	}
}

func TestAddTurnRedactsAndTruncatesMessages(t *testing.T) {
	gw := &fakeGateway{addContext: "fresh context"}
	svc := New(testConfig(), gw)
	secret := "sk" + "-" + strings.Repeat("x", 30)
	long := secret + strings.Repeat("a", MaxMessageChars+100)

	got, err := svc.AddTurn(context.Background(), "thread-1", []Message{{Role: "user", Content: long}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got != "fresh context" {
		t.Fatalf("context = %q", got)
	}
	if len([]rune(gw.messages[0].Content)) > MaxMessageChars {
		t.Fatalf("message exceeded %d chars", MaxMessageChars)
	}
	if strings.Contains(gw.messages[0].Content, secret) {
		t.Fatalf("secret was not redacted: %q", gw.messages[0].Content)
	}
	if !gw.returnContext {
		t.Fatalf("returnContext not forwarded")
	}
}

func TestAddTurnRejectsTooManyMessages(t *testing.T) {
	svc := New(testConfig(), &fakeGateway{})
	msgs := make([]Message, MaxMessagesPerCall+1)
	for i := range msgs {
		msgs[i] = Message{Role: "user", Content: "x"}
	}
	if _, err := svc.AddTurn(context.Background(), "thread-1", msgs, false); err == nil {
		t.Fatal("expected too-many-messages error")
	}
}

func TestAddDataChunksAndTargetsProject(t *testing.T) {
	gw := &fakeGateway{}
	svc := New(testConfig(), gw)

	if err := svc.AddData(context.Background(), AddDataRequest{Data: strings.Repeat("x", MaxGraphDataChars+1)}); err != nil {
		t.Fatal(err)
	}
	if len(gw.data) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(gw.data))
	}
	if gw.data[0].Target != TargetProject || gw.data[0].Type != "text" {
		t.Fatalf("defaults not applied: %+v", gw.data[0])
	}
	if len([]rune(gw.data[0].Data)) != MaxGraphDataChars || len([]rune(gw.data[1].Data)) != 1 {
		t.Fatalf("bad chunk lengths: %d/%d", len([]rune(gw.data[0].Data)), len([]rune(gw.data[1].Data)))
	}
	if gw.data[0].Metadata["sentgraph_chunk_index"] != 0 || gw.data[1].Metadata["sentgraph_chunk_index"] != 1 {
		t.Fatalf("chunk indexes missing: %+v / %+v", gw.data[0].Metadata, gw.data[1].Metadata)
	}
	if gw.data[0].Metadata["sentgraph_chunk_count"] != 2 || gw.data[1].Metadata["sentgraph_chunk_count"] != 2 {
		t.Fatalf("chunk counts missing: %+v / %+v", gw.data[0].Metadata, gw.data[1].Metadata)
	}
}

func TestSearchDefaultsAndCapsLimit(t *testing.T) {
	gw := &fakeGateway{}
	svc := New(testConfig(), gw)

	_, err := svc.Search(context.Background(), SearchRequest{Query: "architecture", Limit: 999})
	if err != nil {
		t.Fatal(err)
	}
	if gw.search.Target != TargetProject || gw.search.Limit != 50 {
		t.Fatalf("search defaults/caps not applied: %+v", gw.search)
	}
}

func TestGetContextCombinesUserAndProjectMemory(t *testing.T) {
	gw := &fakeGateway{
		userContext: "prefers Go",
		results:     []SearchResult{{Text: "project uses Zep"}},
	}
	svc := New(testConfig(), gw)

	got, err := svc.GetContext(context.Background(), ContextOptions{ThreadID: "t", Query: "zep"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "User memory:") || !strings.Contains(got, "Project memory:") {
		t.Fatalf("missing context sections: %q", got)
	}
}

func TestForgetValidatesKind(t *testing.T) {
	svc := New(testConfig(), &fakeGateway{})
	if err := svc.Forget(context.Background(), "user", "uuid"); err == nil {
		t.Fatal("expected unsupported kind error")
	}
}

func testConfig() config.Config {
	return config.Config{ZepAPIKey: "key", UserID: "dev-1", ProjectID: "alpha", ContextTokenBudget: 2000}
}

type fakeGateway struct {
	userID        string
	graphID       string
	threadID      string
	threadUserID  string
	messages      []Message
	returnContext bool
	addContext    string
	userContext   string
	search        SearchRequest
	results       []SearchResult
	data          []AddDataRequest
	history       []Message
	deletedKind   string
	deletedUUID   string
}

func (f *fakeGateway) EnsureUser(ctx context.Context, userID string) error {
	f.userID = userID
	return nil
}

func (f *fakeGateway) EnsureProjectGraph(ctx context.Context, graphID string) error {
	f.graphID = graphID
	return nil
}

func (f *fakeGateway) EnsureThread(ctx context.Context, threadID, userID string) error {
	f.threadID = threadID
	f.threadUserID = userID
	return nil
}

func (f *fakeGateway) AddMessages(ctx context.Context, threadID string, messages []Message, returnContext bool) (string, error) {
	f.threadID = threadID
	f.messages = messages
	f.returnContext = returnContext
	return f.addContext, nil
}

func (f *fakeGateway) GetUserContext(ctx context.Context, threadID string) (string, error) {
	f.threadID = threadID
	return f.userContext, nil
}

func (f *fakeGateway) SearchGraph(ctx context.Context, req SearchRequest, userID, graphID string) ([]SearchResult, error) {
	f.search = req
	f.userID = userID
	f.graphID = graphID
	return f.results, nil
}

func (f *fakeGateway) AddGraphData(ctx context.Context, req AddDataRequest, userID, graphID string) error {
	f.data = append(f.data, req)
	f.userID = userID
	f.graphID = graphID
	return nil
}

func (f *fakeGateway) ThreadHistory(ctx context.Context, threadID string, limit int) ([]Message, error) {
	f.threadID = threadID
	return f.history, nil
}

func (f *fakeGateway) DeleteGraphItem(ctx context.Context, kind, uuid string) error {
	f.deletedKind = kind
	f.deletedUUID = uuid
	return nil
}
