package providers

import (
	"context"
	"github.com/S-Corkum/mcp-server/pkg/mcp"
)

// ContextStorage defines the interface for context storage providers
type ContextStorage interface {
	// StoreContext stores a context
	StoreContext(ctx context.Context, contextData *mcp.Context) error
	
	// GetContext retrieves a context by ID
	GetContext(ctx context.Context, contextID string) (*mcp.Context, error)
	
	// DeleteContext deletes a context
	DeleteContext(ctx context.Context, contextID string) error
	
	// ListContexts lists contexts for an agent and optionally a session
	ListContexts(ctx context.Context, agentID string, sessionID string) ([]*mcp.Context, error)
}
