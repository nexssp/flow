package journal_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/glebarez/go-sqlite"
	"github.com/nexssp/flow/journal"
)

func TestSQLBranchJournal_DurableRecordAndReplay(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory sqlite failed: %v", err)
	}
	defer db.Close()

	j := journal.NewSQLBranchJournal(db)
	ctx := context.Background()

	if err = j.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema failed: %v", err)
	}

	records := []journal.BranchRecord{
		{
			RunID:       "run_prod_01",
			SourceNode:  "classifier",
			From:        "classifier",
			To:          "security_expert",
			When:        `state.security == true`,
			Status:      journal.BranchSelected,
			Reason:      "matched",
			DecisionSeq: 0,
			CreatedAt:   time.Now().UTC(),
		},
		{
			RunID:       "run_prod_01",
			SourceNode:  "classifier",
			From:        "classifier",
			To:          "general_expert",
			Otherwise:   true,
			Status:      journal.BranchSkipped,
			Reason:      "fallback_skipped",
			DecisionSeq: 1,
			CreatedAt:   time.Now().UTC(),
		},
	}

	if err = j.Put(ctx, "run_prod_01", "classifier", records); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	stored, found, err := j.Get(ctx, "run_prod_01", "classifier")
	if err != nil || !found {
		t.Fatalf("Get failed or not found: found=%t err=%v", found, err)
	}

	if len(stored) != 2 {
		t.Fatalf("expected 2 stored records, got %d", len(stored))
	}
	if stored[0].Status != journal.BranchSelected || stored[1].Status != journal.BranchSkipped {
		t.Fatalf("unexpected statuses in stored journal: %+v", stored)
	}

	if err = j.Put(ctx, "run_prod_01", "classifier", records); err != nil {
		t.Fatalf("idempotent re-put failed: %v", err)
	}
}
