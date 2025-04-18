package core

import (
	"context"
	"testing"
	"time"

	"github.com/S-Corkum/mcp-server/pkg/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestContextManager_UpdateContext_ReplaceContent(t *testing.T) {
	// Setup mock database and cache
	mockDB := &MockDatabase{}
	mockCache := &MockCache{}
	
	// Create a context manager
	cm := NewContextManager(mockDB, mockCache)
	
	// Create initial context with some content
	initialContext := &mcp.Context{
		ID:           "test-context-id",
		AgentID:      "test-agent",
		ModelID:      "test-model",
		Content: []mcp.ContextItem{
			{
				Role:      "system",
				Content:   "You are a helpful assistant.",
				Timestamp: time.Now(),
				Tokens:    8,
			},
		},
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		CurrentTokens: 8,
	}
	
	// Create new content to replace existing content
	newContent := []mcp.ContextItem{
		{
			Role:      "user",
			Content:   "Hello, can you help me?",
			Timestamp: time.Now(),
			Tokens:    6,
		},
	}
	
	// Create update request with new content
	updateRequest := &mcp.Context{
		Content: newContent,
	}
	
	// Create options with ReplaceContent set to true
	options := &mcp.ContextUpdateOptions{
		ReplaceContent: true,
	}
	
	// Mock database responses
	mockDB.On("GetContext", mock.Anything, "test-context-id").Return(initialContext, nil)
	
	// Mock database update with expected replaced content
	mockDB.On("UpdateContext", mock.Anything, mock.MatchedBy(func(ctx *mcp.Context) bool {
		// Verify that content was replaced, not appended
		if len(ctx.Content) != 1 {
			return false
		}
		
		// Verify token count was reset and recalculated
		if ctx.CurrentTokens != 6 {
			return false
		}
		
		return true
	})).Return(nil)
	
	// Mock cache set
	mockCache.On("Set", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	
	// Call the UpdateContext method
	result, err := cm.UpdateContext(context.Background(), "test-context-id", updateRequest, options)
	
	// Assert no error and verify results
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 1, len(result.Content))
	assert.Equal(t, "user", result.Content[0].Role)
	assert.Equal(t, "Hello, can you help me?", result.Content[0].Content)
	assert.Equal(t, 6, result.CurrentTokens)
	
	// Verify mocks were called as expected
	mockDB.AssertExpectations(t)
	mockCache.AssertExpectations(t)
}

func TestContextManager_UpdateContext_AppendContent(t *testing.T) {
	// Setup mock database and cache
	mockDB := &MockDatabase{}
	mockCache := &MockCache{}
	
	// Create a context manager
	cm := NewContextManager(mockDB, mockCache)
	
	// Create initial context with some content
	initialContext := &mcp.Context{
		ID:           "test-context-id",
		AgentID:      "test-agent",
		ModelID:      "test-model",
		Content: []mcp.ContextItem{
			{
				Role:      "system",
				Content:   "You are a helpful assistant.",
				Timestamp: time.Now(),
				Tokens:    8,
			},
		},
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		CurrentTokens: 8,
	}
	
	// Create new content to append to existing content
	newContent := []mcp.ContextItem{
		{
			Role:      "user",
			Content:   "Hello, can you help me?",
			Timestamp: time.Now(),
			Tokens:    6,
		},
	}
	
	// Create update request with new content
	updateRequest := &mcp.Context{
		Content: newContent,
	}
	
	// Use nil options to test default append behavior
	var options *mcp.ContextUpdateOptions = nil
	
	// Mock database responses
	mockDB.On("GetContext", mock.Anything, "test-context-id").Return(initialContext, nil)
	
	// Mock database update with expected appended content
	mockDB.On("UpdateContext", mock.Anything, mock.MatchedBy(func(ctx *mcp.Context) bool {
		// Verify that content was appended (should have 2 items)
		if len(ctx.Content) != 2 {
			return false
		}
		
		// Verify token count includes both items
		if ctx.CurrentTokens != 14 {
			return false
		}
		
		return true
	})).Return(nil)
	
	// Mock cache set
	mockCache.On("Set", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	
	// Call the UpdateContext method
	result, err := cm.UpdateContext(context.Background(), "test-context-id", updateRequest, options)
	
	// Assert no error and verify results
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 2, len(result.Content))
	assert.Equal(t, "system", result.Content[0].Role)
	assert.Equal(t, "user", result.Content[1].Role)
	assert.Equal(t, 14, result.CurrentTokens)
	
	// Verify mocks were called as expected
	mockDB.AssertExpectations(t)
	mockCache.AssertExpectations(t)
}
