package fs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xctx"
)

func TestDownloadStreamsAndReportsByteProgress(t *testing.T) {
	const chunkSize = 1 << 20
	body := bytes.Repeat([]byte("x"), 4*chunkSize)
	progressSeen := make(chan struct{})
	var progressOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		if _, err := w.Write(body[:chunkSize]); err != nil {
			return
		}
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("flush response: %v", err)
			return
		}
		select {
		case <-progressSeen:
		case <-time.After(3 * time.Second):
			t.Error("download did not report bytes before the response finished")
			return
		}
		_, _ = w.Write(body[chunkSize:])
	}))
	defer server.Close()

	progressEvents := make(chan xctx.Progress, 128)
	ctx := xctx.WithProgressReporter(t.Context(), func(progress xctx.Progress) {
		progressEvents <- progress
		if progress.Current > 0 {
			progressOnce.Do(func() { close(progressSeen) })
		}
	})
	outputDir := t.TempDir()
	result, err := invokeDownload(ctx, map[string]any{
		"url":            server.URL,
		"path":           "large.bin",
		"output_dir":     outputDir,
		"expected_bytes": int64(len(body)),
	})
	if err != nil {
		t.Fatalf("fs.download returned error: %v", err)
	}
	if result.Files[0].Bytes != int64(len(body)) || result.Files[0].TransferredBytes != int64(len(body)) {
		t.Fatalf("download result = %#v, want %d bytes", result.Files[0], len(body))
	}
	got, err := os.ReadFile(filepath.Join(outputDir, "large.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("downloaded body length = %d, want %d", len(got), len(body))
	}

	var last xctx.Progress
	seenBytes := false
	for len(progressEvents) > 0 {
		progress := <-progressEvents
		if progress.Unit != "bytes" || progress.Metadata["source"] != "fs.download" {
			t.Fatalf("progress event = %#v, want byte progress from fs.download", progress)
		}
		if progress.Current > 0 {
			seenBytes = true
			last = progress
		}
	}
	if !seenBytes || last.Current != int64(len(body)) || last.Total != int64(len(body)) {
		t.Fatalf("final progress = %#v, want %d/%d bytes", last, len(body), len(body))
	}
}

func TestDownloadResumesWithRange(t *testing.T) {
	outputDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outputDir, "model.bin.part"), []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Range"); got != "bytes=3-" {
			t.Errorf("Range header = %q, want bytes=3-", got)
		}
		w.Header().Set("Content-Range", "bytes 3-9/10")
		w.Header().Set("Content-Length", "7")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(w, "defghij")
	}))
	defer server.Close()

	result, err := invokeDownload(t.Context(), map[string]any{
		"url":            server.URL,
		"path":           "model.bin",
		"output_dir":     outputDir,
		"expected_bytes": int64(10),
	})
	if err != nil {
		t.Fatalf("fs.download returned error: %v", err)
	}
	if !result.Files[0].Resumed || result.Files[0].TransferredBytes != 7 || result.Files[0].Bytes != 10 {
		t.Fatalf("download result = %#v, want resumed 7-byte transfer to 10-byte file", result.Files[0])
	}
	got, err := os.ReadFile(filepath.Join(outputDir, "model.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "abcdefghij" {
		t.Fatalf("file contents = %q, want abcdefghij", got)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "model.bin.part")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial file still exists after successful transfer: %v", err)
	}
}

func TestDownloadRejectsBadStatusRangeAndSize(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		header    string
		body      string
		expected  int64
		partial   string
		wantError string
	}{
		{name: "bad status", status: http.StatusServiceUnavailable, body: "temporarily unavailable", expected: 5, wantError: "HTTP 503"},
		{name: "bad range start", status: http.StatusPartialContent, header: "bytes 2-9/10", body: "12345678", expected: 10, partial: "abc", wantError: "unexpected Content-Range start"},
		{name: "wrong size", status: http.StatusOK, body: "four", expected: 5, wantError: "incomplete transfer"},
		{name: "partial without range", status: http.StatusPartialContent, header: "bytes 0-3/4", body: "four", expected: 4, wantError: "partial content without a Range request"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			outputDir := t.TempDir()
			if tc.partial != "" {
				if err := os.WriteFile(filepath.Join(outputDir, "file.bin.part"), []byte(tc.partial), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.header != "" {
					w.Header().Set("Content-Range", tc.header)
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()

			_, err := invokeDownload(t.Context(), map[string]any{
				"url":            server.URL,
				"path":           "file.bin",
				"output_dir":     outputDir,
				"expected_bytes": tc.expected,
			})
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("error = %v, want text %q", err, tc.wantError)
			}
		})
	}
}

func TestDownloadCancellationRetainsPartialFile(t *testing.T) {
	const totalSize = 8 << 20
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(totalSize))
		_, _ = w.Write(bytes.Repeat([]byte("c"), 1<<20))
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("flush response: %v", err)
			return
		}
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(t.Context())
	var canceled atomic.Bool
	ctx = xctx.WithProgressReporter(ctx, func(progress xctx.Progress) {
		if progress.Current > 0 && canceled.CompareAndSwap(false, true) {
			cancel()
		}
	})
	outputDir := t.TempDir()
	_, err := invokeDownload(ctx, map[string]any{
		"url":            server.URL,
		"path":           "cancelled.bin",
		"output_dir":     outputDir,
		"expected_bytes": int64(totalSize),
	})
	cancel()
	if err == nil {
		t.Fatal("fs.download succeeded after request cancellation")
	}
	if !canceled.Load() {
		t.Fatal("progress callback did not cancel the request")
	}
	partial, err := os.Stat(filepath.Join(outputDir, "cancelled.bin.part"))
	if err != nil {
		t.Fatalf("partial file was not retained: %v", err)
	}
	if partial.Size() <= 0 || partial.Size() >= totalSize {
		t.Fatalf("partial file size = %d, want between 1 and %d", partial.Size(), totalSize)
	}
}

func TestDownloadRejectsTraversalAndSymlinkPaths(t *testing.T) {
	var requested atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requested.Store(true)
		_, _ = io.WriteString(w, "x")
	}))
	defer server.Close()

	root := t.TempDir()
	_, err := invokeDownload(t.Context(), map[string]any{
		"url":        server.URL,
		"path":       "../escape.bin",
		"output_dir": root,
	})
	if err == nil || !strings.Contains(err.Error(), "invalid file path") {
		t.Fatalf("traversal error = %v, want file-path validation failure", err)
	}
	if requested.Load() {
		t.Fatal("HTTP server was contacted for a traversal path")
	}

	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	_, err = invokeDownload(t.Context(), map[string]any{
		"url":        server.URL,
		"path":       "escape/created.bin",
		"output_dir": root,
	})
	if err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("symlink path error = %v, want symbolic-link rejection", err)
	}
	if requested.Load() {
		t.Fatal("HTTP server was contacted for a symlink path")
	}
	if _, err := os.Stat(filepath.Join(outside, "created.bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("outside file exists after rejected path: %v", err)
	}
}

func TestDownloadReplacesExactTargetSymlink(t *testing.T) {
	const contents = "new"
	var requested atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requested.Store(true)
		_, _ = io.WriteString(w, contents)
	}))
	defer server.Close()

	outputDir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.bin")
	if err := os.WriteFile(outside, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(outputDir, "artifact.bin")
	if err := os.Symlink(outside, target); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}

	result, err := invokeDownload(t.Context(), map[string]any{
		"url":            server.URL,
		"path":           "artifact.bin",
		"output_dir":     outputDir,
		"expected_bytes": int64(len(contents)),
	})
	if err != nil {
		t.Fatalf("fs.download returned error: %v", err)
	}
	if !requested.Load() || len(result.Files) != 1 || result.Files[0].Skipped {
		t.Fatalf("download result = %#v, requested = %t; want a fetched file", result, requested.Load())
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != contents {
		t.Fatalf("downloaded file = %q, want %q", got, contents)
	}
	gotOutside, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotOutside) != "old" {
		t.Fatalf("outside target changed to %q", gotOutside)
	}
	info, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("download target mode = %s, want regular file", info.Mode())
	}
}

func TestDownloadManifestWritesProvenanceReceipt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/config.json":
			_, _ = io.WriteString(w, "{}")
		case "/weights.bin":
			_, _ = io.WriteString(w, "weights")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	outputDir := t.TempDir()

	result, err := invokeDownload(t.Context(), map[string]any{
		"output_dir": outputDir,
		"files": []map[string]any{
			{"url": server.URL + "/config.json", "path": "config.json", "expected_bytes": int64(2)},
			{"url": server.URL + "/weights.bin", "path": "weights.bin", "expected_bytes": int64(7)},
		},
		"receipt_path": ".download-receipt.json",
		"provenance":   map[string]any{"repo_id": "LiquidAI/LFM3-1.7B", "resolved_commit": "abc123"},
	})
	if err != nil {
		t.Fatalf("fs.download returned error: %v", err)
	}
	if len(result.Files) != 2 || result.TotalBytes != 9 || result.TransferredBytes != 9 {
		t.Fatalf("manifest result = %#v", result)
	}
	if result.ReceiptPath == "" {
		t.Fatal("result omitted the receipt path")
	}
	data, err := os.ReadFile(filepath.Join(outputDir, ".download-receipt.json"))
	if err != nil {
		t.Fatal(err)
	}
	var receipt map[string]any
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatalf("decode receipt: %v", err)
	}
	provenance, ok := receipt["provenance"].(map[string]any)
	if !ok || provenance["repo_id"] != "LiquidAI/LFM3-1.7B" || provenance["resolved_commit"] != "abc123" {
		t.Fatalf("receipt provenance = %#v", receipt["provenance"])
	}
}

func invokeDownload(ctx context.Context, input map[string]any) (DownloadResult, error) {
	raw, err := action.InvokeAny(ctx, Download, input)
	if err != nil {
		return DownloadResult{}, err
	}
	result, ok := raw.(DownloadResult)
	if !ok {
		return DownloadResult{}, fmt.Errorf("result type = %T, want fs.DownloadResult", raw)
	}
	return result, nil
}
