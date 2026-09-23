package fsio

import (
	"cmp"
	"fmt"
	"iter"
	"slices"
	"strings"

	"github.com/nexssp/kernel/action"
)

type SortConfig struct {
	MaxItems int    `json:"max_items"`
	By       string `json:"by"`
}

func Sort(cfg SortConfig) action.StreamOp[FileMeta, FileMeta] {
	maxItems := cfg.MaxItems
	if maxItems <= 0 {
		maxItems = 100000
	}
	by := cfg.By
	if by == "" {
		by = "rel_path"
	}

	return func(up iter.Seq2[FileMeta, error]) iter.Seq2[FileMeta, error] {
		return func(yield func(FileMeta, error) bool) {
			initial := maxItems
			if initial > 1024 {
				initial = 1024
			}
			buf := make([]FileMeta, 0, initial)

			for item, err := range up {
				if err != nil {
					var zero FileMeta
					yield(zero, err)
					return
				}
				if len(buf) >= maxItems {
					var zero FileMeta
					yield(zero, fmt.Errorf("fs.sort: max_items %d exceeded", maxItems))
					return
				}
				buf = append(buf, item)
			}

			switch by {
			case "size":
				slices.SortFunc(buf, func(a, b FileMeta) int {
					return cmp.Compare(a.Size, b.Size)
				})
			case "mod_time":
				slices.SortFunc(buf, func(a, b FileMeta) int {
					return a.ModTime.Compare(b.ModTime)
				})
			default:
				slices.SortFunc(buf, func(a, b FileMeta) int {
					return strings.Compare(a.RelPath, b.RelPath)
				})
			}

			for _, item := range buf {
				if !yield(item, nil) {
					return
				}
			}
		}
	}
}

func SortOperator() action.NamedOperator {
	return action.NewOperator("fs.sort", Sort)
}
