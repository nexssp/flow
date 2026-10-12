package bundle

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	compiler "github.com/nexssp/flow/compiler"
)

const ID = "support_tools"

const ticketCategoryBilling = "billing"

func init() { compiler.Register(ID, Bundle) }

func Bundle(_ map[string]string) compiler.Bundle {
	return compiler.Bundle{
		ID: ID,
		Libraries: []action.Library{{
			Name: "support",
			Actions: []action.AnyAction{
				action.New("support.lookup_tier", lookupTier).
					Description("Look up account service tier; unavailable accounts are recoverable").Build(),
				action.New("support.classify", classifyTicket).
					Description("Route tickets by urgency and billing intent").Build(),
				action.New("support.draft_reply", draftReply).
					Description("Create a safe response draft without sending it").Build(),
			},
		}},
	}
}

func lookupTier(_ context.Context, in map[string]any) (map[string]any, error) {
	customerID, ok := in["customer_id"].(string)
	if !ok || strings.TrimSpace(customerID) == "" {
		return nil, errors.New("support.lookup_tier: customer_id is required")
	}
	if strings.HasPrefix(strings.ToLower(customerID), "offline-") {
		return nil, xerr.Unavailable("customer profile service is temporarily unavailable")
	}
	out := cloneMap(in)
	if tier, ok := out["customer_tier"].(string); !ok || strings.TrimSpace(tier) == "" {
		out["customer_tier"] = "standard"
	}
	return out, nil
}

func classifyTicket(_ context.Context, in map[string]any) (map[string]any, error) {
	subject, _ := in["subject"].(string)
	message, _ := in["message"].(string)
	text := strings.ToLower(subject + " " + message)
	tier, _ := in["customer_tier"].(string)

	priority, queue, category := "normal", "customer-support", "general"
	if strings.Contains(text, "charge") || strings.Contains(text, "refund") {
		queue, category = ticketCategoryBilling, ticketCategoryBilling
	}
	if strings.Contains(text, "charged twice") || strings.Contains(text, "fraud") {
		priority = "urgent"
	}
	if strings.EqualFold(tier, "gold") && category == ticketCategoryBilling {
		priority, queue = "urgent", "billing-escalations"
	}
	if priority == "urgent" && queue == ticketCategoryBilling {
		queue = "billing-escalations"
	}

	out := cloneMap(in)
	out["priority"] = priority
	out["queue"] = queue
	out["category"] = category
	return out, nil
}

func draftReply(_ context.Context, in map[string]any) (map[string]any, error) {
	ticketID, _ := in["ticket_id"].(string)
	if strings.TrimSpace(ticketID) == "" {
		return nil, errors.New("support.draft_reply: ticket_id is required")
	}
	out := cloneMap(in)
	out["reply_status"] = "draft"
	out["reply_text"] = fmt.Sprintf("We have received ticket %s. A support specialist will review it shortly.", ticketID)
	return out, nil
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in)+4)
	maps.Copy(out, in)
	return out
}
