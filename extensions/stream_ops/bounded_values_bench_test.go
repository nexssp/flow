package stream_ops

import "testing"

type benchmarkStreamRecord struct {
	ID     int    `json:"id"`
	Name   string `json:"name,omitempty"`
	Values []int  `json:"values,omitempty"`
}

func BenchmarkPrepareStreamItem(b *testing.B) {
	item := benchmarkStreamRecord{ID: 42, Name: "record", Values: []int{1, 2, 3}}
	budget := uint64(maxStreamBufferBytes)
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := prepareStreamItem(item, budget); err != nil {
			b.Fatal(err)
		}
	}
}
