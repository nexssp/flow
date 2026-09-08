package main

import (
	"context"
	"fmt"

	"github.com/nexssp/flow"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/validation"
)

type CreateAccountReq struct {
	Email    string `json:"email" validate:"required,email"`
	Username string `json:"username" validate:"required,min=3"`
}

type UnvalidatedReq struct {
	Note string `json:"note"`
}

func main() {
	ctx := context.Background()

	// 1. Actions defined without any per-action validation code
	createAccount := action.New("account.create", func(_ context.Context, req CreateAccountReq) (string, error) {
		return fmt.Sprintf("Account created for %s (%s)", req.Username, req.Email), nil
	}).Build()

	addNote := action.New("note.add", func(_ context.Context, req UnvalidatedReq) (string, error) {
		return fmt.Sprintf("Note saved: %s", req.Note), nil
	}).Build()

	registry := flow.NewRegistry(createAccount, addNote)

	// 2. Enable Validation Globally across all actions in 1 line
	validatorPlugin := validation.New()
	for _, act := range registry.Actions() {
		act.AddAnyHook(validatorPlugin.GetAnyHooks()[0])
	}

	// 3. Test Tagged Request -> Automatically Validated
	act, _ := registry.Get("account.create")
	_, err := act.DoAny(ctx, CreateAccountReq{Email: "invalid-email", Username: "al"})
	if err != nil {
		fmt.Printf("❌ Global validation caught invalid request: %v\n", err)
	}

	// 4. Test Untagged Request -> Automatically Bypassed with Zero Overhead
	actUntagged, _ := registry.Get("note.add")
	res, err := actUntagged.DoAny(ctx, UnvalidatedReq{Note: "hello world"})
	if err == nil {
		fmt.Printf("✅ Untagged action executed cleanly: %v\n", res)
	}
}
