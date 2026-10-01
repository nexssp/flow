package fs

import "iter"

// sliceSeq converts a slice into an iter.Seq2 for testing.
func sliceSeq[T any](items []T) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		for _, item := range items {
			if !yield(item, nil) {
				return
			}
		}
	}
}
