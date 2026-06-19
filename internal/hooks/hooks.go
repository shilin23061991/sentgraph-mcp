package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/shilin23061991/sentgraph-mcp/internal/memory"
	"github.com/shilin23061991/sentgraph-mcp/internal/transcript"
)

// defaultThreadID is used when a hook payload has no session id. This keeps
// early startup hooks useful while still making the fallback explicit.
const defaultThreadID = "sentgraph-default"

type Service interface {
	EnsureIdentity(ctx context.Context, threadID string) error
	AddTurn(ctx context.Context, threadID string, messages []memory.Message, returnContext bool) (string, error)
	GetContext(ctx context.Context, opts memory.ContextOptions) (string, error)
}

type Handler struct {
	service Service
}

func New(service Service) *Handler {
	return &Handler{service: service}
}

type Payload struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	CWD            string `json:"cwd"`
	Prompt         string `json:"prompt"`
	HookEventName  string `json:"hook_event_name"`
}

type Response struct {
	HookSpecificOutput hookSpecificOutput `json:"hookSpecificOutput,omitempty"`
}

type hookSpecificOutput struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext,omitempty"`
}

func (h *Handler) Handle(ctx context.Context, event string, r io.Reader, w io.Writer) error {
	var p Payload
	if err := json.NewDecoder(r).Decode(&p); err != nil && err != io.EOF {
		return err
	}
	threadID := firstNonEmpty(p.SessionID, defaultThreadID)
	if err := h.service.EnsureIdentity(ctx, threadID); err != nil {
		return err
	}

	switch event {
	case "SessionStart", "PreCompact":
		return h.injectContext(ctx, w, event, threadID, "")
	case "UserPromptSubmit":
		prompt := strings.TrimSpace(p.Prompt)
		var contextBlock string
		if prompt != "" {
			var err error
			contextBlock, err = h.service.AddTurn(ctx, threadID, []memory.Message{{Role: "user", Content: prompt}}, true)
			if err != nil {
				return err
			}
		}
		if contextBlock == "" {
			return h.injectContext(ctx, w, event, threadID, prompt)
		}
		return writeContext(w, event, contextBlock)
	case "Stop", "SessionEnd":
		return h.persistLatestAssistant(ctx, p.TranscriptPath, p.CWD, threadID)
	default:
		return nil
	}
}

func (h *Handler) injectContext(ctx context.Context, w io.Writer, event, threadID, query string) error {
	contextBlock, err := h.service.GetContext(ctx, memory.ContextOptions{ThreadID: threadID, Query: query, Limit: 5})
	if err != nil {
		return err
	}
	return writeContext(w, event, contextBlock)
}

func (h *Handler) persistLatestAssistant(ctx context.Context, transcriptPath, cwd, threadID string) error {
	if transcriptPath == "" {
		return nil
	}
	f, err := openTranscript(transcriptPath, cwd)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
	}()

	entries, err := transcript.Parse(f)
	if err != nil {
		return err
	}
	text := transcript.LastByRole(entries, "assistant")
	if strings.TrimSpace(text) == "" {
		return nil
	}
	_, err = h.service.AddTurn(ctx, threadID, []memory.Message{{Role: "assistant", Content: text}}, false)
	return err
}

func openTranscript(transcriptPath, cwd string) (*os.File, error) {
	if !filepath.IsAbs(transcriptPath) {
		if cwd == "" {
			wd, err := os.Getwd()
			if err != nil {
				return nil, err
			}
			cwd = wd
		}
		transcriptPath = filepath.Join(cwd, transcriptPath)
	}
	path, err := filepath.Abs(transcriptPath)
	if err != nil {
		return nil, err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	allowedRoots, err := allowedTranscriptRoots(cwd)
	if err != nil {
		return nil, err
	}
	for _, root := range allowedRoots {
		rel, err := filepath.Rel(root, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel) {
			return os.Open(path)
		}
	}
	return nil, fmt.Errorf("transcript path %q is outside allowed roots", transcriptPath)
}

func allowedTranscriptRoots(cwd string) ([]string, error) {
	roots := make([]string, 0, 2)
	if cwd == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		cwd = wd
	}
	absCWD, err := filepath.Abs(cwd)
	if err != nil {
		return nil, err
	}
	roots = append(roots, evalRoot(absCWD))
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		roots = append(roots, evalRoot(filepath.Join(home, ".claude", "projects")))
	}
	return roots, nil
}

func evalRoot(root string) string {
	if evaluated, err := filepath.EvalSymlinks(root); err == nil {
		return evaluated
	}
	return root
}

func writeContext(w io.Writer, event, contextBlock string) error {
	return json.NewEncoder(w).Encode(Response{
		HookSpecificOutput: hookSpecificOutput{
			HookEventName:     event,
			AdditionalContext: contextBlock,
		},
	})
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
