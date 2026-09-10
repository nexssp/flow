package flow

import "context"

type approvalTokenCtxKey struct{}

func WithApprovalToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, approvalTokenCtxKey{}, token)
}

func ApprovalTokenFrom(ctx context.Context) string {
	if v, ok := ctx.Value(approvalTokenCtxKey{}).(string); ok {
		return v
	}

	return ""
}
