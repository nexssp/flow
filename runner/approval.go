package runner

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/nexssp/kernel/xerr"
)

type ApprovalMode string

const (
	ApprovalNone   ApprovalMode = "none"
	ApprovalDanger ApprovalMode = "danger"
	ApprovalAll    ApprovalMode = "all"
)

type TerminalApprovalGate struct {
	mode        ApprovalMode
	autoApprove atomic.Bool
	mu          sync.Mutex
	approved    map[string]bool
}

func NewApprovalGate(mode ApprovalMode) *TerminalApprovalGate {
	if mode == "" {
		mode = ApprovalDanger
	}

	return &TerminalApprovalGate{
		mode:     mode,
		approved: make(map[string]bool),
	}
}

func (g *TerminalApprovalGate) Check(ctx context.Context, actionName, argsJSON, token string) error {
	if g.mode == ApprovalNone || g.autoApprove.Load() {
		return nil
	}

	if g.mode == ApprovalDanger && !isDangerAction(actionName) {
		return nil
	}

	g.mu.Lock()
	if g.approved[actionName] {
		g.mu.Unlock()

		return nil
	}
	g.mu.Unlock()

	fmt.Printf("\n⚠️  [APPROVAL REQUIRED] Action: \033[1;33m%s\033[0m\n", actionName)

	// gocritic emptyStringTest: use != "" instead of len(...) > 0 for strings.
	if argsJSON != "" && argsJSON != "{}" && argsJSON != "null" {
		snipArgs := argsJSON
		if len(snipArgs) > 120 {
			snipArgs = snipArgs[:120] + "..."
		}

		fmt.Printf("   Parameters: %s\n", snipArgs)
	}

	fmt.Print("👉 Authorize execution? [y]es / [n]o / [a]lways approve: ")

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	choice := strings.ToLower(strings.TrimSpace(input))

	switch choice {
	case "y", "yes":
		g.mu.Lock()
		g.approved[actionName] = true
		g.mu.Unlock()

		return nil
	case "a", "always":
		g.autoApprove.Store(true)

		return nil
	default:
		return xerr.Forbidden(fmt.Sprintf("execution of %q denied by operator", actionName))
	}
}

func isDangerAction(name string) bool {
	lower := strings.ToLower(name)

	dangerKeywords := []string{
		"exec", "write", "delete", "rm", "drop", "truncate", "migration", "patch", "deploy",
	}
	for _, kw := range dangerKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}

	return false
}
