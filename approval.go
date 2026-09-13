package flow

import (
	"context"

	"github.com/nexssp/kernel/xctx"
)

// ApprovalTokenKey is the universal context key for approval tokens.
// Backed by kernel/xctx so flow remains completely independent of any AI packages.
var ApprovalTokenKey = xctx.NewKey[string]("nexss.approval.token")

// WithApprovalToken attaches an approval token to the context.
func WithApprovalToken(ctx context.Context, token string) context.Context {
	return ApprovalTokenKey.With(ctx, token)
}

// ApprovalTokenFrom extracts an approval token from the context.
func ApprovalTokenFrom(ctx context.Context) string {
	token, _ := ApprovalTokenKey.From(ctx)

	return token
}
