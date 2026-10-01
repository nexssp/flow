package fs

import (
	"cmp"
	"fmt"
	"iter"
	"slices"
	"strings"

	"github.com/nexssp/kernel/action"
)

// SortConfig controls fs.sort.
type SortConfig struct {
	// By is one of "rel_path" (default), "size", "mod_time".
	By string `json:"by" cli:"by"`
	// MaxItems bounds the buffer. Zero uses a safe default of 100000.
	MaxItems int `json:"max_items" cli:"max_items"`
}

const sortDefaultMaxItems = 100000

// Sort buffers the stream and yields items in sorted order. Sorting is
// stable within equal keys.
func Sort(cfg SortConfig) action.StreamOp[FileMeta, FileMeta] {
	maxItems := cfg.MaxItems
	if maxItems <= 0 {
		maxItems = sortDefaultMaxItems
	}
	by := cfg.By
	if by == "" {
		by = "rel_path"
	}

	return func(up iter.Seq2[FileMeta, error]) iter.Seq2[FileMeta, error] {
		return func(yield func(FileMeta, error) bool) {
			initial := min(maxItems, 1024)
			buffer := make([]FileMeta, 0, initial)

			for meta, err := range up {
				if err != nil {
					yield(FileMeta{}, err)
					return
				}
				if len(buffer) >= maxItems {
					yield(FileMeta{}, fmt.Errorf("fs.sort: max_items %d exceeded", maxItems))
					return
				}
				buffer = append(buffer, meta)
			}

			slices.SortStableFunc(buffer, comparatorFor(by))
			for _, meta := range buffer {
				if !yield(meta, nil) {
					return
				}
			}
		}
	}
}

func SortOperator() action.NamedOperator {
	return action.NewOperator("fs.sort", Sort)
}

func comparatorFor(by string) func(a, b FileMeta) int {
	switch by {
	case "size":
		return func(a, b FileMeta) int { return cmp.Compare(a.Size, b.Size) }
	case "mod_time":
		return func(a, b FileMeta) int { return a.ModTime.Compare(b.ModTime) }
	default:
		return func(a, b FileMeta) int { return strings.Compare(a.RelPath, b.RelPath) }
	}
}
