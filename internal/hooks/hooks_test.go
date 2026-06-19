package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shilin23061991/sentgraph-mcp/internal/memory"
)

func TestHandleSessionStartInjectsContext(t *testing.T) {
	svc := &fakeService{contextBlock: "User memory:\n- likes Go"}
	h := New(svc)

	var out bytes.Buffer
	err := h.Handle(context.Background(), "SessionStart", strings.NewReader(`{"session_id":"s1","cwd":"."}`), &out)
	if err != nil {
		t.Fatal(err)
	}
	if svc.ensuredThreadID != "s1" {
		t.Fatalf("ensured thread id = %q", svc.ensuredThreadID)
	}
	if got := decodeContext(t, out.Bytes()); got != svc.contextBlock {
		t.Fatalf("context = %q", got)
	}
}

func TestHandleUserPromptSubmitPersistsAndFallsBackToContext(t *testing.T) {
	svc := &fakeService{contextBlock: "fallback context"}
	h := New(svc)

	var out bytes.Buffer
	err := h.Handle(context.Background(), "UserPromptSubmit", strings.NewReader(`{"session_id":"s1","prompt":"hello"}`), &out)
	if err != nil {
		t.Fatal(err)
	}
	if len(svc.messages) != 1 || svc.messages[0].Role != "user" || svc.messages[0].Content != "hello" {
		t.Fatalf("messages = %+v", svc.messages)
	}
	if !svc.returnContext {
		t.Fatalf("returnContext was not forwarded")
	}
	if got := decodeContext(t, out.Bytes()); got != "fallback context" {
		t.Fatalf("context = %q", got)
	}
}

func TestHandleStopPersistsLatestAssistant(t *testing.T) {
	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "transcript.jsonl")
	content := strings.Join([]string{
		`{"type":"assistant","message":{"role":"assistant","content":"first"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"latest"}]}}`,
	}, "\n")
	if err := os.WriteFile(transcriptPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	svc := &fakeService{}
	h := New(svc)
	payload := `{"session_id":"s1","cwd":` + quoteJSON(dir) + `,"transcript_path":` + quoteJSON(transcriptPath) + `}`
	err := h.Handle(context.Background(), "Stop", strings.NewReader(payload), &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if len(svc.messages) != 1 || svc.messages[0].Role != "assistant" || svc.messages[0].Content != "latest" {
		t.Fatalf("messages = %+v", svc.messages)
	}
}

func TestOpenTranscriptRejectsOutsidePath(t *testing.T) {
	cwd := t.TempDir()
	outside := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(outside, []byte(`{"type":"assistant"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if f, err := openTranscript(outside, cwd); err == nil {
		_ = f.Close()
		t.Fatal("expected outside transcript path to be rejected")
	}
}

func TestOpenTranscriptRejectsSymlinkEscape(t *testing.T) {
	cwd := t.TempDir()
	target := filepath.Join(t.TempDir(), "outside.jsonl")
	if err := os.WriteFile(target, []byte(`{"type":"assistant"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(cwd, "link.jsonl")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	if f, err := openTranscript(link, cwd); err == nil {
		_ = f.Close()
		t.Fatal("expected symlink escape to be rejected")
	}
}

func TestWriteContextAllowsEmptyContext(t *testing.T) {
	var out bytes.Buffer
	if err := writeContext(&out, "SessionStart", ""); err != nil {
		t.Fatal(err)
	}
	if got := decodeContext(t, out.Bytes()); got != "" {
		t.Fatalf("context = %q", got)
	}
}

type fakeService struct {
	ensuredThreadID string
	contextBlock    string
	messages        []memory.Message
	returnContext   bool
}

func (f *fakeService) EnsureIdentity(ctx context.Context, threadID string) error {
	f.ensuredThreadID = threadID
	return nil
}

func (f *fakeService) AddTurn(ctx context.Context, threadID string, messages []memory.Message, returnContext bool) (string, error) {
	f.messages = messages
	f.returnContext = returnContext
	return "", nil
}

func (f *fakeService) GetContext(ctx context.Context, opts memory.ContextOptions) (string, error) {
	return f.contextBlock, nil
}

func decodeContext(t *testing.T, data []byte) string {
	t.Helper()
	var resp Response
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatal(err)
	}
	return resp.HookSpecificOutput.AdditionalContext
}

func quoteJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
