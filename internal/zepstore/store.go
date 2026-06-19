// Package zepstore adapts the official Zep Go SDK to memory.Gateway.
package zepstore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	zep "github.com/getzep/zep-go/v3"
	zepclient "github.com/getzep/zep-go/v3/client"
	"github.com/getzep/zep-go/v3/option"

	"github.com/shilin23061991/sentgraph-mcp/internal/memory"
)

type Store struct {
	client *zepclient.Client
}

func New(apiKey string) *Store {
	return &Store{client: zepclient.NewClient(option.WithAPIKey(apiKey))}
}

func (s *Store) EnsureUser(ctx context.Context, userID string) error {
	if _, err := s.client.User.Get(ctx, userID); err == nil {
		return nil
	} else if !isNotFound(err) {
		return fmt.Errorf("check Zep user %q: %w", userID, err)
	}
	_, err := s.client.User.Add(ctx, &zep.CreateUserRequest{UserID: userID})
	return err
}

func (s *Store) EnsureProjectGraph(ctx context.Context, graphID string) error {
	if _, err := s.client.Graph.Get(ctx, graphID); err == nil {
		return nil
	} else if !isNotFound(err) {
		return fmt.Errorf("check Zep graph %q: %w", graphID, err)
	}
	name := graphID
	_, err := s.client.Graph.Create(ctx, &zep.CreateGraphRequest{
		GraphID: graphID,
		Name:    &name,
	})
	return err
}

func (s *Store) EnsureThread(ctx context.Context, threadID, userID string) error {
	if _, err := s.client.Thread.Get(ctx, threadID, &zep.ThreadGetRequest{Lastn: intPtr(1)}); err == nil {
		return nil
	} else if !isNotFound(err) {
		return fmt.Errorf("check Zep thread %q: %w", threadID, err)
	}
	_, err := s.client.Thread.Create(ctx, &zep.CreateThreadRequest{
		ThreadID: threadID,
		UserID:   userID,
	})
	return err
}

func (s *Store) AddMessages(ctx context.Context, threadID string, messages []memory.Message, returnContext bool) (string, error) {
	req := &zep.AddThreadMessagesRequest{
		Messages:      make([]*zep.Message, 0, len(messages)),
		ReturnContext: boolPtr(returnContext),
	}
	for _, msg := range messages {
		req.Messages = append(req.Messages, &zep.Message{
			Role:      zep.RoleType(msg.Role),
			Content:   msg.Content,
			Name:      stringPtrOrNil(msg.Name),
			CreatedAt: stringPtrOrNil(msg.CreatedAt),
			Metadata:  msg.Metadata,
		})
	}
	resp, err := s.client.Thread.AddMessages(ctx, threadID, req)
	if err != nil || resp == nil || resp.Context == nil {
		return "", err
	}
	return *resp.Context, nil
}

func (s *Store) GetUserContext(ctx context.Context, threadID string) (string, error) {
	resp, err := s.client.Thread.GetUserContext(ctx, threadID, nil)
	if err != nil || resp == nil || resp.Context == nil {
		return "", err
	}
	return *resp.Context, nil
}

func (s *Store) SearchGraph(ctx context.Context, req memory.SearchRequest, userID, graphID string) ([]memory.SearchResult, error) {
	query := &zep.GraphSearchQuery{
		Query: req.Query,
		Limit: intPtr(req.Limit),
	}
	if scope := strings.TrimSpace(req.Scope); scope != "" {
		query.Scope = zep.GraphSearchScope(scope).Ptr()
	}
	switch req.Target {
	case memory.TargetUser:
		query.UserID = &userID
	default:
		query.GraphID = &graphID
	}

	resp, err := s.client.Graph.Search(ctx, query)
	if err != nil || resp == nil {
		return nil, err
	}
	out := make([]memory.SearchResult, 0, len(resp.Edges)+len(resp.Nodes)+len(resp.Episodes))
	for _, edge := range resp.Edges {
		out = append(out, memory.SearchResult{Kind: "edge", Text: edge.Fact, UUID: edge.UUID})
	}
	for _, node := range resp.Nodes {
		text := strings.TrimSpace(strings.Join([]string{node.Name, node.Summary}, " - "))
		out = append(out, memory.SearchResult{Kind: "node", Text: text, UUID: node.UUID})
	}
	for _, ep := range resp.Episodes {
		out = append(out, memory.SearchResult{Kind: "episode", Text: ep.Content, UUID: ep.UUID})
	}
	if len(out) == 0 && resp.Context != nil && *resp.Context != "" {
		out = append(out, memory.SearchResult{Kind: "context", Text: *resp.Context})
	}
	return out, nil
}

func (s *Store) AddGraphData(ctx context.Context, req memory.AddDataRequest, userID, graphID string) error {
	dataType := zep.GraphDataType(req.Type)
	zreq := &zep.AddDataRequest{
		Data:              req.Data,
		Type:              dataType,
		Metadata:          req.Metadata,
		SourceDescription: stringPtrOrNil(req.Description),
	}
	switch req.Target {
	case memory.TargetUser:
		zreq.UserID = &userID
	default:
		zreq.GraphID = &graphID
	}
	_, err := s.client.Graph.Add(ctx, zreq)
	return err
}

func (s *Store) ThreadHistory(ctx context.Context, threadID string, limit int) ([]memory.Message, error) {
	resp, err := s.client.Thread.Get(ctx, threadID, &zep.ThreadGetRequest{Lastn: intPtr(limit)})
	if err != nil || resp == nil {
		return nil, err
	}
	out := make([]memory.Message, 0, len(resp.Messages))
	for _, msg := range resp.Messages {
		out = append(out, memory.Message{
			Role:      string(msg.Role),
			Content:   msg.Content,
			Name:      stringValue(msg.Name),
			CreatedAt: stringValue(msg.CreatedAt),
			Metadata:  msg.Metadata,
		})
	}
	return out, nil
}

func (s *Store) DeleteGraphItem(ctx context.Context, kind, uuid string) error {
	switch kind {
	case "edge":
		_, err := s.client.Graph.Edge.Delete(ctx, uuid)
		return err
	case "node":
		_, err := s.client.Graph.Node.Delete(ctx, uuid)
		return err
	case "episode":
		_, err := s.client.Graph.Episode.Delete(ctx, uuid)
		return err
	default:
		return fmt.Errorf("unknown graph item kind: %q", kind)
	}
}

func isNotFound(err error) bool {
	var notFound *zep.NotFoundError
	return errors.As(err, &notFound)
}

func stringPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func stringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func intPtr(i int) *int { return &i }

func boolPtr(b bool) *bool { return &b }
