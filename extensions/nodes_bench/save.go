package nodes_bench

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xfs"
)

// BenchSaveReq is a BenchRunRes plus a target file. The embedded
// result is written as-is.
type BenchSaveReq struct {
	File string `json:"file" validate:"required"`
	BenchRunRes
}

// BenchSaveRes reports what was written. The embedded result is
// propagated so a pipeline can chain save into compare.
type BenchSaveRes struct {
	File  string `json:"file"`
	Bytes int    `json:"bytes"`
	BenchRunRes
}

// BenchSave writes benchmark results to a JSON file. The path is
// validated through xfs.Rel, so it must be relative and cannot escape
// the working directory.
var BenchSave = action.New("bench.save", func(_ context.Context, req BenchSaveReq) (BenchSaveRes, error) {
	clean, err := xfs.Rel(req.File)
	if err != nil {
		return BenchSaveRes{}, err
	}

	data, err := json.MarshalIndent(req.BenchRunRes, "", "  ")
	if err != nil {
		return BenchSaveRes{}, xerr.Internal("bench.save: marshal", err)
	}

	if dir := filepath.Dir(clean); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return BenchSaveRes{}, xerr.Internal("bench.save: mkdir", err)
		}
	}
	if err := os.WriteFile(clean, data, 0o600); err != nil {
		return BenchSaveRes{}, xerr.Internal("bench.save: write", err)
	}

	return BenchSaveRes{
		File:        clean,
		Bytes:       len(data),
		BenchRunRes: req.BenchRunRes,
	}, nil
}).Description("Write benchmark results to a JSON file").
	Tag("bench", "io").
	Build()
