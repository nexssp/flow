package journal

import (
	"context"
	"database/sql"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	"github.com/nexssp/kernel/xerr"
)

type SQLBranchJournal struct {
	db *sql.DB
}

func NewSQLBranchJournal(db *sql.DB) *SQLBranchJournal {
	return &SQLBranchJournal{db: db}
}

func (j *SQLBranchJournal) EnsureSchema(ctx context.Context) error {
	if j == nil || j.db == nil {
		return xerr.Internal("journal: database connection is nil")
	}

	query := `CREATE TABLE IF NOT EXISTS graph_branch_decisions (
		run_id TEXT NOT NULL,
		source_node TEXT NOT NULL,
		from_node TEXT NOT NULL,
		to_node TEXT NOT NULL,
		when_expr TEXT NOT NULL,
		otherwise_flag INTEGER NOT NULL,
		status TEXT NOT NULL,
		reason TEXT NOT NULL,
		decision_seq INTEGER NOT NULL,
		created_at TEXT NOT NULL,
		PRIMARY KEY (run_id, source_node, from_node, to_node)
	);`

	_, err := j.db.ExecContext(ctx, query)
	if err != nil {
		return xerr.Internal("journal: create schema failed", err)
	}

	return nil
}

func (j *SQLBranchJournal) Get(ctx context.Context, runID, sourceNode string) ([]BranchRecord, bool, error) {
	if j == nil || j.db == nil {
		return nil, false, xerr.Internal("journal: database connection is nil")
	}

	query := `SELECT run_id, source_node, from_node, to_node, when_expr, otherwise_flag,
		status, reason, decision_seq, created_at
		FROM graph_branch_decisions
		WHERE run_id = ? AND source_node = ?
		ORDER BY decision_seq ASC;`

	rows, err := j.db.QueryContext(ctx, query, runID, sourceNode)
	if err != nil {
		return nil, false, xerr.Internal("journal: query branch decisions failed", err)
	}
	defer rows.Close()

	var records []BranchRecord

	for rows.Next() {
		var (
			rec          BranchRecord
			otherwiseInt int
			createdAtStr string
		)

		err := rows.Scan(
			&rec.RunID, &rec.SourceNode, &rec.From, &rec.To,
			&rec.When, &otherwiseInt, &rec.Status, &rec.Reason,
			&rec.DecisionSeq, &createdAtStr,
		)
		if err != nil {
			return nil, false, xerr.Internal("journal: scan branch decision failed", err)
		}

		rec.Otherwise = otherwiseInt == 1
		if t, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
			rec.CreatedAt = t
		}

		records = append(records, rec)
	}

	if err := rows.Err(); err != nil {
		return nil, false, xerr.Internal("journal: iterate rows failed", err)
	}

	if len(records) == 0 {
		return nil, false, nil
	}

	return records, true, nil
}

func (j *SQLBranchJournal) Put(ctx context.Context, runID, sourceNode string, records []BranchRecord) error {
	if j == nil || j.db == nil {
		return xerr.Internal("journal: database connection is nil")
	}

	if len(records) == 0 {
		return nil
	}

	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return xerr.Internal("journal: begin transaction failed", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO graph_branch_decisions
		(run_id, source_node, from_node, to_node, when_expr, otherwise_flag, status, reason, decision_seq, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(run_id, source_node, from_node, to_node) DO NOTHING;`)
	if err != nil {
		return xerr.Internal("journal: prepare statement failed", err)
	}
	defer stmt.Close()

	for i := range records {
		rec := &records[i]

		otherwiseInt := 0
		if rec.Otherwise {
			otherwiseInt = 1
		}

		createdAtStr := rec.CreatedAt.UTC().Format(time.RFC3339)

		if _, execErr := stmt.ExecContext(
			ctx,
			rec.RunID,
			rec.SourceNode,
			rec.From,
			rec.To,
			rec.When,
			otherwiseInt,
			string(rec.Status),
			rec.Reason,
			rec.DecisionSeq,
			createdAtStr,
		); execErr != nil {
			return xerr.Internal("journal: insert branch decision failed", execErr)
		}
	}

	if err = tx.Commit(); err != nil {
		return xerr.Internal("journal: commit transaction failed", err)
	}

	return nil
}
