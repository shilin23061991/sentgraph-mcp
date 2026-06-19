// Package memory owns Sentgraph's core behavior. It intentionally depends on a
// narrow gateway interface rather than the Zep SDK directly, so the business
// rules (redaction, limits, chunking, context assembly) stay testable offline.
package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/shilin23061991/sentgraph-mcp/internal/config"
	"github.com/shilin23061991/sentgraph-mcp/internal/redact"
)

const (
	MaxMessagesPerCall = 30
	MaxMessageChars    = 4096
	MaxGraphDataChars  = 10000
)

type Target string

const (
	TargetUser    Target = "user"
	TargetProject Target = "project"
)

type Message struct {
	Role      string
	Content   string
	Name      string
	CreatedAt string
	Metadata  map[string]any
}

type SearchRequest struct {
	Query  string
	Target Target
	Scope  string
	Limit  int
}

type SearchResult struct {
	Kind string
	Text string
	UUID string
}

type AddDataRequest struct {
	Data        string
	Type        string
	Target      Target
	Description string
	Metadata    map[string]any
}

type ContextOptions struct {
	ThreadID string
	Query    string
	Limit    int
}

type Gateway interface {
	EnsureUser(ctx context.Context, userID string) error
	EnsureProjectGraph(ctx context.Context, graphID string) error
	EnsureThread(ctx context.Context, threadID, userID string) error
	AddMessages(ctx context.Context, threadID string, messages []Message, returnContext bool) (string, error)
	GetUserContext(ctx context.Context, threadID string) (string, error)
	SearchGraph(ctx context.Context, req SearchRequest, userID, graphID string) ([]SearchResult, error)
	AddGraphData(ctx context.Context, req AddDataRequest, userID, graphID string) error
	ThreadHistory(ctx context.Context, threadID string, limit int) ([]Message, error)
	DeleteGraphItem(ctx context.Context, kind, uuid string) error
}

type Service struct {
	cfg config.Config
	gw  Gateway
}

func New(cfg config.Config, gw Gateway) *Service {
	return &Service{cfg: cfg, gw: gw}
}

func (s *Service) EnsureIdentity(ctx context.Context, threadID string) error {
	if err := s.cfg.Validate(); err != nil {
		return err
	}
	if threadID == "" {
		return errors.New("thread id is required")
	}
	if err := s.gw.EnsureUser(ctx, s.cfg.UserID); err != nil {
		return err
	}
	if err := s.gw.EnsureProjectGraph(ctx, s.cfg.ProjectGraphID()); err != nil {
		return err
	}
	return s.gw.EnsureThread(ctx, threadID, s.cfg.UserID)
}

func (s *Service) AddTurn(ctx context.Context, threadID string, messages []Message, returnContext bool) (string, error) {
	if threadID == "" {
		return "", errors.New("thread id is required")
	}
	if len(messages) == 0 {
		return "", nil
	}
	if len(messages) > MaxMessagesPerCall {
		return "", fmt.Errorf("too many messages: got %d, max %d", len(messages), MaxMessagesPerCall)
	}

	cleaned := make([]Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == "" {
			return "", errors.New("message role is required")
		}
		msg.Content = truncate(redact.Secrets(msg.Content), MaxMessageChars)
		if strings.TrimSpace(msg.Content) == "" {
			continue
		}
		cleaned = append(cleaned, msg)
	}
	if len(cleaned) == 0 {
		return "", nil
	}
	return s.gw.AddMessages(ctx, threadID, cleaned, returnContext)
}

func (s *Service) GetContext(ctx context.Context, opts ContextOptions) (string, error) {
	if opts.ThreadID == "" {
		return "", errors.New("thread id is required")
	}
	parts := make([]string, 0, 2)
	userContext, err := s.gw.GetUserContext(ctx, opts.ThreadID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(userContext) != "" {
		parts = append(parts, "User memory:\n"+strings.TrimSpace(userContext))
	}

	query := strings.TrimSpace(opts.Query)
	if query != "" {
		limit := opts.Limit
		if limit <= 0 {
			limit = 5
		}
		results, err := s.Search(ctx, SearchRequest{
			Query:  query,
			Target: TargetProject,
			Scope:  "edges",
			Limit:  limit,
		})
		if err != nil {
			return "", err
		}
		if len(results) > 0 {
			var b strings.Builder
			b.WriteString("Project memory:")
			for _, r := range results {
				b.WriteString("\n- ")
				b.WriteString(strings.TrimSpace(r.Text))
			}
			parts = append(parts, b.String())
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

func (s *Service) Search(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	req.Query = strings.TrimSpace(req.Query)
	if req.Query == "" {
		return nil, errors.New("query is required")
	}
	if req.Target == "" {
		req.Target = TargetProject
	}
	if req.Limit <= 0 {
		req.Limit = 10
	}
	if req.Limit > 50 {
		req.Limit = 50
	}
	return s.gw.SearchGraph(ctx, req, s.cfg.UserID, s.cfg.ProjectGraphID())
}

// AddData persists a fact, decision, or document-like payload to user or
// project graph memory. Data above MaxGraphDataChars is split into separate
// Zep graph episodes with chunk metadata. Zep does not expose a transaction for
// these writes, so if chunk N fails, earlier chunks have already been written;
// callers should retry the operation or delete partial chunks if that matters.
func (s *Service) AddData(ctx context.Context, req AddDataRequest) error {
	req.Data = strings.TrimSpace(redact.Secrets(req.Data))
	if req.Data == "" {
		return nil
	}
	if req.Type == "" {
		req.Type = "text"
	}
	if req.Target == "" {
		req.Target = TargetProject
	}

	allChunks := chunks(req.Data, MaxGraphDataChars)
	for i, chunk := range allChunks {
		next := req
		next.Data = chunk
		if len(allChunks) > 1 {
			next.Metadata = cloneMetadata(req.Metadata)
			next.Metadata["sentgraph_chunk_index"] = i
			next.Metadata["sentgraph_chunk_count"] = len(allChunks)
		}
		if err := s.gw.AddGraphData(ctx, next, s.cfg.UserID, s.cfg.ProjectGraphID()); err != nil {
			if i > 0 {
				return fmt.Errorf("add graph data chunk %d/%d: %w (previous chunks were already written)", i+1, len(allChunks), err)
			}
			return fmt.Errorf("add graph data chunk %d/%d: %w", i+1, len(allChunks), err)
		}
	}
	return nil
}

func (s *Service) History(ctx context.Context, threadID string, limit int) ([]Message, error) {
	if threadID == "" {
		return nil, errors.New("thread id is required")
	}
	if limit <= 0 {
		limit = 20
	}
	return s.gw.ThreadHistory(ctx, threadID, limit)
}

func (s *Service) Forget(ctx context.Context, kind, uuid string) error {
	kind = strings.TrimSpace(kind)
	uuid = strings.TrimSpace(uuid)
	if kind == "" || uuid == "" {
		return errors.New("kind and uuid are required")
	}
	switch kind {
	case "edge", "node", "episode":
		return s.gw.DeleteGraphItem(ctx, kind, uuid)
	default:
		return fmt.Errorf("unsupported memory kind %q", kind)
	}
}

func truncate(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit])
}

func chunks(s string, size int) []string {
	r := []rune(s)
	if len(r) <= size {
		return []string{s}
	}
	out := make([]string, 0, (len(r)+size-1)/size)
	for len(r) > 0 {
		n := min(len(r), size)
		out = append(out, string(r[:n]))
		r = r[n:]
	}
	return out
}

func cloneMetadata(in map[string]any) map[string]any {
	out := make(map[string]any, len(in)+2)
	for k, v := range in {
		out[k] = v
	}
	return out
}
