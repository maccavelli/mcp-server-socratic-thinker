package socratic

import "context"

type contextKey string

const sessionIDContextKey contextKey = "session_id"

// ContextWithSessionID returns a child context carrying the MCP session identifier.
func ContextWithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionIDContextKey, sessionID)
}

// SessionIDFromContext extracts the MCP session identifier from ctx.
func SessionIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(sessionIDContextKey).(string); ok {
		return v
	}
	return ""
}
