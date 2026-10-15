package data

import "context"

type ctxKey string

const (
	userNameKey ctxKey = "user_name"
)

func WithContextUserName(ctx context.Context, userName string) context.Context {
	return context.WithValue(ctx, userNameKey, userName)
}

func CtxUserName(ctx context.Context) string {
	if v, ok := ctx.Value(userNameKey).(string); ok {
		return v
	}

	return "unknown"
}
