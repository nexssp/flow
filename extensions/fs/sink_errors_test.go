package fs

import (
	"bytes"
	"errors"
	"io"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var (
	errSinkWrite    = errors.New("sink write failed")
	errSinkClose    = errors.New("sink close failed")
	errSinkUpstream = errors.New("sink upstream failed")
)

type failingSink struct {
	bytes.Buffer
	writeErr error
	closeErr error
	writes   int
	closes   int
}

func (s *failingSink) Write(p []byte) (int, error) {
	s.writes++
	if s.writeErr != nil {
		return 0, s.writeErr
	}
	return s.Buffer.Write(p)
}

func (s *failingSink) Close() error {
	s.closes++
	return s.closeErr
}

func sinkInput(items []string, finalErr error) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		for _, item := range items {
			if !yield(item, nil) {
				return
			}
		}
		if finalErr != nil {
			yield("", finalErr)
		}
	}
}

func TestWriteOutStreamErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		items       []string
		flushEach   bool
		writeErr    error
		closeErr    error
		upstreamErr error
		want        error
		wantItems   int
		wantWrites  int
	}{
		{name: "write", items: []string{strings.Repeat("x", 64*1024+1)}, writeErr: errSinkWrite, closeErr: errSinkClose, want: errSinkWrite, wantWrites: 1},
		{name: "per-item flush", items: []string{"small"}, flushEach: true, writeErr: errSinkWrite, closeErr: errSinkClose, want: errSinkWrite, wantWrites: 1},
		{name: "final flush", items: []string{"small"}, writeErr: errSinkWrite, closeErr: errSinkClose, want: errSinkWrite, wantItems: 1, wantWrites: 1},
		{name: "close", items: []string{"small"}, closeErr: errSinkClose, want: errSinkClose, wantItems: 1, wantWrites: 1},
		{name: "empty close", closeErr: errSinkClose, want: errSinkClose},
		{name: "upstream before cleanup errors", items: []string{"small"}, upstreamErr: errSinkUpstream, writeErr: errSinkWrite, closeErr: errSinkClose, want: errSinkUpstream, wantItems: 1, wantWrites: 1},
		{name: "success", items: []string{"one", "two"}, wantItems: 2, wantWrites: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sink := &failingSink{writeErr: tc.writeErr, closeErr: tc.closeErr}
			seen := 0
			err := writeOutStream(sink, sinkInput(tc.items, tc.upstreamErr), tc.flushEach, func(_ string, itemErr error) bool {
				if itemErr != nil {
					t.Fatalf("unexpected item error: %v", itemErr)
				}
				seen++
				return true
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if seen != tc.wantItems || sink.writes != tc.wantWrites || sink.closes != 1 {
				t.Fatalf("items/writes/closes = %d/%d/%d, want %d/%d/1", seen, sink.writes, sink.closes, tc.wantItems, tc.wantWrites)
			}
			if tc.name == "success" && sink.String() != "onetwo" {
				t.Fatalf("output = %q", sink.String())
			}
		})
	}
}

func TestWriteOutStreamDownstreamStop(t *testing.T) {
	t.Parallel()
	for _, flushEach := range []bool{false, true} {
		for _, failCleanup := range []bool{false, true} {
			sink := &failingSink{}
			seen := 0
			produced := 0
			up := func(yield func(string, error) bool) {
				for _, item := range []string{"one", "two"} {
					produced++
					if !yield(item, nil) {
						return
					}
				}
			}
			err := writeOutStream(sink, up, flushEach, func(_ string, _ error) bool {
				seen++
				if failCleanup {
					sink.writeErr = errSinkWrite
					sink.closeErr = errSinkClose
				}
				return false
			})
			if err != nil || seen != 1 || produced != 1 || sink.closes != 1 || sink.writes != 1 {
				t.Fatalf("flushEach=%v failCleanup=%v: error=%v items=%d produced=%d writes=%d closes=%d", flushEach, failCleanup, err, seen, produced, sink.writes, sink.closes)
			}
		}
	}
}

func TestOutFileSinkIntegration(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nested", "output")
	for _, appendMode := range []bool{false, true} {
		seen := 0
		for item, err := range OutFile[string](OutFileConfig{Path: path, Append: appendMode})(sinkInput([]string{"one", "two"}, nil)) {
			if err != nil {
				t.Fatal(err)
			}
			if item != "one" && item != "two" {
				t.Fatalf("unexpected item %q", item)
			}
			seen++
			break
		}
		if seen != 1 {
			t.Fatalf("items = %d, want 1", seen)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "oneone" {
		t.Fatalf("output = %q", data)
	}
}

func metaSinkInput(items []FileMeta, finalErr error) iter.Seq2[FileMeta, error] {
	return func(yield func(FileMeta, error) bool) {
		for _, item := range items {
			if !yield(item, nil) {
				return
			}
		}
		if finalErr != nil {
			yield(FileMeta{}, finalErr)
		}
	}
}

func TestStdoutSinkErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		path        string
		writeErr    error
		upstreamErr error
		want        error
		wantItems   int
	}{
		{name: "write", path: strings.Repeat("x", 8192), writeErr: errSinkWrite, want: errSinkWrite},
		{name: "flush", path: "small", writeErr: errSinkWrite, want: errSinkWrite},
		{name: "upstream", upstreamErr: errSinkUpstream, writeErr: errSinkWrite, want: errSinkUpstream},
		{name: "success", path: "file.txt", wantItems: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sink := &failingSink{writeErr: tc.writeErr}
			var items []FileMeta
			if tc.path != "" {
				items = []FileMeta{{RelPath: tc.path}}
			}
			seen, errorsSeen := 0, 0
			var got error
			for _, err := range stdoutTo(sink, metaSinkInput(items, tc.upstreamErr)) {
				if err != nil {
					errorsSeen++
					got = err
				} else {
					seen++
				}
			}
			if !errors.Is(got, tc.want) || seen != tc.wantItems || (tc.want != nil && errorsSeen != 1) {
				t.Fatalf("error=%v items=%d errors=%d, want error=%v items=%d", got, seen, errorsSeen, tc.want, tc.wantItems)
			}
			if sink.closes != 0 {
				t.Fatal("stdout sink closed shared output")
			}
			if tc.want == nil && sink.String() != tc.path+"\n" {
				t.Fatalf("output = %q", sink.String())
			}
		})
	}
}

func TestStdoutSinkDownstreamStop(t *testing.T) {
	t.Parallel()
	sink := &failingSink{}
	produced, seen := 0, 0
	up := func(yield func(FileMeta, error) bool) {
		for _, path := range []string{"one", "two"} {
			produced++
			if !yield(FileMeta{RelPath: path}, nil) {
				return
			}
		}
	}
	stdoutTo(sink, up)(func(_ FileMeta, err error) bool {
		seen++
		if err != nil {
			t.Fatal(err)
		}
		sink.writeErr = io.ErrClosedPipe
		return false
	})
	if produced != 1 || seen != 1 || sink.writes != 1 || sink.closes != 0 {
		t.Fatalf("produced/items/writes/closes = %d/%d/%d/%d", produced, seen, sink.writes, sink.closes)
	}
}
