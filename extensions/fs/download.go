package fs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xctx"
	"github.com/nexssp/kernel/xerr"

	flowfiles "github.com/nexssp/flow/extensions/files"
)

// DownloadFile is one HTTP resource in a generic download manifest. Path is
// a slash-separated relative path below DownloadRequest.OutputDir.
type DownloadFile struct {
	URL           string            `json:"url"`
	Path          string            `json:"path"`
	ExpectedBytes int64             `json:"expected_bytes,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
}

// DownloadRequest downloads one URL or a manifest of URLs without buffering
// response bodies in memory. A .part file is retained on transfer failure and is
// resumed with HTTP Range on the next run.
type DownloadRequest struct {
	URL           string            `json:"url,omitempty"`
	Path          string            `json:"path,omitempty"`
	ExpectedBytes int64             `json:"expected_bytes,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	OutputDir     string            `json:"output_dir,omitempty"`
	Files         []DownloadFile    `json:"files,omitempty"`
	ReceiptPath   string            `json:"receipt_path,omitempty"`
	Provenance    map[string]any    `json:"provenance,omitempty"`
}

// DownloadFileResult describes the verified final file. TransferredBytes is
// only bytes received during this invocation; Bytes is the complete file size.
type DownloadFileResult struct {
	Path             string `json:"path"`
	Bytes            int64  `json:"bytes"`
	TransferredBytes int64  `json:"transferred_bytes"`
	Resumed          bool   `json:"resumed"`
	Skipped          bool   `json:"skipped,omitempty"`
}

// DownloadResult summarizes one action invocation and is also the payload
// stored in the optional receipt (with caller-supplied provenance).
type DownloadResult struct {
	Files            []DownloadFileResult `json:"files"`
	TotalBytes       int64                `json:"total_bytes"`
	TransferredBytes int64                `json:"transferred_bytes"`
	ReceiptPath      string               `json:"receipt_path,omitempty"`
}

var downloadHTTPClient = &http.Client{}

const downloadProgressInterval int64 = 1 << 20

// Download streams HTTP bodies to disk, reporting real byte progress through
// Kernel's supported xctx.ReportProgress API. It accepts either url/path fields
// for one file or a files array for a generic manifest.
var Download = action.New[DownloadRequest, DownloadResult]("fs.download", downloadHTTP).
	Description("Stream HTTP response bodies to safe relative file paths with byte progress and resumable Range requests").
	Tag("fs", "network").
	Build()

func downloadHTTP(ctx context.Context, req DownloadRequest) (DownloadResult, error) {
	if err := ctx.Err(); err != nil {
		return DownloadResult{}, err
	}
	if req.OutputDir == "" {
		req.OutputDir = "."
	}
	files, err := normalizeDownloadFiles(req)
	if err != nil {
		return DownloadResult{}, err
	}

	root, err := resolveDownloadRoot(req.OutputDir)
	if err != nil {
		return DownloadResult{}, xerr.Internal("fs.download: prepare output directory", err)
	}

	total, totalKnown, err := expectedDownloadBytes(files)
	if err != nil {
		return DownloadResult{}, err
	}
	progressTotal := total
	if !totalKnown {
		progressTotal = 0
	}
	result, err := downloadFiles(ctx, files, root, total, progressTotal)
	if err != nil {
		return result, err
	}

	if req.ReceiptPath != "" {
		receiptTarget, err := safeDownloadTarget(root, req.ReceiptPath)
		if err != nil {
			return result, xerr.Validation("fs.download receipt: " + err.Error())
		}
		if err := writeDownloadReceipt(receiptTarget, result, req.Provenance); err != nil {
			return result, xerr.Internal("fs.download: write receipt", err)
		}
		result.ReceiptPath = receiptTarget
	}
	reportDownloadProgress(ctx, result.TotalBytes, result.TotalBytes, "HTTP download manifest complete", "")
	return result, nil
}

func normalizeDownloadFiles(req DownloadRequest) ([]DownloadFile, error) {
	files := req.Files
	if len(files) == 0 {
		if req.URL == "" || req.Path == "" {
			return nil, xerr.BadRequest("fs.download: provide url and path, or a non-empty files manifest")
		}
		files = []DownloadFile{{URL: req.URL, Path: req.Path, ExpectedBytes: req.ExpectedBytes, Headers: req.Headers}}
	}
	for i, file := range files {
		if file.URL == "" || file.Path == "" {
			return nil, xerr.BadRequest(fmt.Sprintf("fs.download: files[%d] requires url and path", i))
		}
		if file.ExpectedBytes < 0 {
			return nil, xerr.Validation(fmt.Sprintf("fs.download: files[%d] expected_bytes cannot be negative", i))
		}
		if err := validateDownloadURL(file.URL); err != nil {
			return nil, xerr.Validation(fmt.Sprintf("fs.download: files[%d]: %v", i, err))
		}
		if _, err := validateRelativeDownloadPath(file.Path); err != nil {
			return nil, xerr.Validation(fmt.Sprintf("fs.download: files[%d]: %v", i, err))
		}
	}
	return files, nil
}

func expectedDownloadBytes(files []DownloadFile) (total int64, totalKnown bool, err error) {
	totalKnown = true
	for _, file := range files {
		if file.ExpectedBytes == 0 {
			totalKnown = false
			continue
		}
		if file.ExpectedBytes > int64(^uint64(0)>>1)-total {
			return 0, false, xerr.Validation("fs.download: total expected byte count overflows int64")
		}
		total += file.ExpectedBytes
	}
	return total, totalKnown, nil
}

func downloadFiles(ctx context.Context, files []DownloadFile, root string, total, progressTotal int64) (DownloadResult, error) {
	result := DownloadResult{Files: make([]DownloadFileResult, 0, len(files)), TotalBytes: total}
	var completedBefore int64
	reportDownloadProgress(ctx, 0, progressTotal, "Starting HTTP download manifest", "")
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		target, err := safeDownloadTarget(root, file.Path)
		if err != nil {
			return result, xerr.Validation("fs.download: " + err.Error())
		}
		fileResult, err := downloadOne(ctx, file, target, completedBefore, progressTotal)
		if err != nil {
			return result, fmt.Errorf("fs.download: %s: %w", file.Path, err)
		}
		result.Files = append(result.Files, fileResult)
		result.TransferredBytes += fileResult.TransferredBytes
		completedBefore += fileResult.Bytes
	}
	result.TotalBytes = completedBefore
	return result, nil
}

func validateDownloadURL(raw string) error {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return errors.New("URL must be an http or https URL without embedded credentials")
	}
	return nil
}

func validateRelativeDownloadPath(name string) (string, error) {
	if err := flowfiles.ValidatePath(name); err != nil {
		return "", err
	}
	rel := filepath.FromSlash(name)
	if !filepath.IsLocal(rel) {
		return "", fmt.Errorf("unsafe relative path %q", name)
	}
	return rel, nil
}

func resolveDownloadRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

func safeDownloadTarget(root, relative string) (string, error) {
	rel, err := validateRelativeDownloadPath(relative)
	if err != nil {
		return "", err
	}
	components := strings.Split(rel, string(os.PathSeparator))
	parent := root
	for _, component := range components[:len(components)-1] {
		parent = filepath.Join(parent, component)
		info, err := os.Lstat(parent)
		if errors.Is(err, os.ErrNotExist) {
			if mkdirErr := os.Mkdir(parent, 0o755); mkdirErr != nil && !errors.Is(mkdirErr, os.ErrExist) {
				return "", fmt.Errorf("create parent directory: %w", mkdirErr)
			}
			info, err = os.Lstat(parent)
		}
		if err != nil {
			return "", fmt.Errorf("inspect parent directory: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("path %q crosses a symbolic link", relative)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("parent component %q is not a directory", component)
		}
	}
	return filepath.Join(parent, components[len(components)-1]), nil
}

func downloadOne(ctx context.Context, file DownloadFile, target string, completedBefore, total int64) (DownloadFileResult, error) {
	if result, complete := completeDownloadTarget(file, target); complete {
		reportDownloadProgress(ctx, completedBefore+result.Bytes, total, "Already complete: "+file.Path, file.Path)
		return result, nil
	}

	partial := target + ".part"
	result, offset, recovered, err := preparePartialDownload(file, target, partial)
	if err != nil {
		return result, err
	}
	if recovered {
		reportDownloadProgress(ctx, completedBefore+result.Bytes, total, "Recovered complete temporary file: "+file.Path, file.Path)
		return result, nil
	}

	request, err := newDownloadRequest(ctx, file, offset)
	if err != nil {
		return result, err
	}
	response, err := downloadHTTPClient.Do(request)
	if err != nil {
		return result, fmt.Errorf("request failed: %w", err)
	}
	defer response.Body.Close()

	details, err := inspectDownloadResponse(response, file, offset)
	if err != nil {
		return result, err
	}
	return streamDownloadResponse(ctx, response, file, target, partial, details, completedBefore, total)
}

func completeDownloadTarget(file DownloadFile, target string) (DownloadFileResult, bool) {
	result := DownloadFileResult{Path: target}
	if file.ExpectedBytes <= 0 {
		return result, false
	}
	info, err := os.Lstat(target)
	if err != nil || !info.Mode().IsRegular() || info.Size() != file.ExpectedBytes {
		return result, false
	}
	result.Bytes = info.Size()
	result.Skipped = true
	return result, true
}

func preparePartialDownload(file DownloadFile, target, partial string) (result DownloadFileResult, offset int64, recovered bool, err error) {
	result = DownloadFileResult{Path: target}
	partialInfo, err := os.Lstat(partial)
	if err == nil {
		if partialInfo.Mode()&os.ModeSymlink != 0 || !partialInfo.Mode().IsRegular() {
			return result, 0, false, fmt.Errorf("temporary path %q is not a regular file", partial)
		}
		if file.ExpectedBytes > 0 && partialInfo.Size() >= file.ExpectedBytes {
			if partialInfo.Size() == file.ExpectedBytes {
				if renameErr := os.Rename(partial, target); renameErr != nil {
					return result, 0, false, fmt.Errorf("commit complete temporary file: %w", renameErr)
				}
				result.Bytes = partialInfo.Size()
				result.Resumed = true
				return result, 0, true, nil
			}
			if removeErr := os.Remove(partial); removeErr != nil {
				return result, 0, false, fmt.Errorf("remove oversized temporary file: %w", removeErr)
			}
		} else if partialInfo.Size() > 0 {
			return result, partialInfo.Size(), false, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, 0, false, fmt.Errorf("inspect temporary file: %w", err)
	}
	return result, 0, false, nil
}

func newDownloadRequest(ctx context.Context, file DownloadFile, offset int64) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, file.URL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	for key, value := range file.Headers {
		request.Header.Set(key, value)
	}
	if offset > 0 {
		request.Header.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
	}
	return request, nil
}

type downloadResponseDetails struct {
	offset        int64
	bodyBytes     int64
	expectedTotal int64
}

func inspectDownloadResponse(response *http.Response, file DownloadFile, offset int64) (downloadResponseDetails, error) {
	details := downloadResponseDetails{offset: offset, bodyBytes: response.ContentLength, expectedTotal: file.ExpectedBytes}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		body, err := io.ReadAll(io.LimitReader(response.Body, 4096))
		if err != nil {
			return details, fmt.Errorf("HTTP %d: read error response body: %w", response.StatusCode, err)
		}
		return details, fmt.Errorf("HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	if response.StatusCode == http.StatusPartialContent {
		if offset == 0 {
			return details, errors.New("server returned partial content without a Range request")
		}
		bodyBytes, declaredTotal, err := validateDownloadContentRange(response.Header.Get("Content-Range"), offset, file.ExpectedBytes)
		if err != nil {
			return details, err
		}
		details.bodyBytes = bodyBytes
		if declaredTotal >= 0 {
			details.expectedTotal = declaredTotal
		}
	} else {
		// A server may ignore Range and return the complete representation. In
		// that case restart from byte zero rather than append duplicate content.
		details.offset = 0
	}
	if response.ContentLength >= 0 && details.bodyBytes >= 0 && response.ContentLength != details.bodyBytes {
		return details, fmt.Errorf("response Content-Length %d does not match range body length %d", response.ContentLength, details.bodyBytes)
	}
	if details.expectedTotal == 0 && response.StatusCode == http.StatusOK && response.ContentLength >= 0 {
		details.expectedTotal = response.ContentLength
	}
	return details, nil
}

func validateDownloadContentRange(header string, offset, expectedTotal int64) (bodyBytes, declaredTotal int64, err error) {
	start, end, rangeTotal, parseErr := parseContentRange(header)
	if parseErr != nil {
		return 0, 0, parseErr
	}
	if start != offset {
		return 0, 0, fmt.Errorf("unexpected Content-Range start %d for resume offset %d", start, offset)
	}
	if rangeTotal >= 0 && expectedTotal > 0 && rangeTotal != expectedTotal {
		return 0, 0, fmt.Errorf("Content-Range total %d does not match expected size %d", rangeTotal, expectedTotal)
	}
	return end - start + 1, rangeTotal, nil
}

func streamDownloadResponse(ctx context.Context, response *http.Response, file DownloadFile, target, partial string, details downloadResponseDetails, completedBefore, total int64) (DownloadFileResult, error) {
	result := DownloadFileResult{Path: target}
	flags := os.O_CREATE | os.O_WRONLY
	if details.offset > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	out, err := os.OpenFile(partial, flags, 0o644)
	if err != nil {
		return result, fmt.Errorf("open temporary output: %w", err)
	}
	progress := &downloadProgressReader{
		reader: response.Body, ctx: ctx, filename: file.Path,
		completedBefore: completedBefore, initialBytes: details.offset,
		fileExpected: details.expectedTotal, total: total,
	}
	var copySource io.Reader = progress
	if details.bodyBytes >= 0 {
		copySource = io.LimitReader(progress, details.bodyBytes+1)
	} else if details.expectedTotal > 0 {
		copySource = io.LimitReader(progress, details.expectedTotal-details.offset+1)
	}
	written, copyErr := io.Copy(out, copySource)
	closeErr := out.Close()
	if copyErr != nil {
		return result, fmt.Errorf("stream response body: %w", copyErr)
	}
	if closeErr != nil {
		return result, fmt.Errorf("close temporary output: %w", closeErr)
	}
	if details.bodyBytes >= 0 && written != details.bodyBytes {
		return result, fmt.Errorf("incomplete range response: received %d bytes, expected %d (partial file retained)", written, details.bodyBytes)
	}
	finalBytes := details.offset + written
	if details.expectedTotal > 0 && finalBytes != details.expectedTotal {
		return result, fmt.Errorf("incomplete transfer: received %d of %d bytes (partial file retained)", finalBytes, details.expectedTotal)
	}
	if err := os.Rename(partial, target); err != nil {
		return result, fmt.Errorf("commit downloaded file: %w", err)
	}
	result.Bytes = finalBytes
	result.TransferredBytes = written
	result.Resumed = details.offset > 0
	reportDownloadProgress(ctx, completedBefore+finalBytes, total, "Downloaded: "+file.Path, file.Path)
	return result, nil
}

func parseContentRange(value string) (start, end, total int64, err error) {
	if !strings.HasPrefix(value, "bytes ") {
		return 0, 0, 0, fmt.Errorf("invalid Content-Range %q", value)
	}
	positions, totalText, ok := strings.Cut(strings.TrimPrefix(value, "bytes "), "/")
	if !ok {
		return 0, 0, 0, fmt.Errorf("invalid Content-Range %q", value)
	}
	startText, endText, ok := strings.Cut(positions, "-")
	if !ok {
		return 0, 0, 0, fmt.Errorf("invalid Content-Range %q", value)
	}
	start, err = strconv.ParseInt(startText, 10, 64)
	if err != nil || start < 0 {
		return 0, 0, 0, fmt.Errorf("invalid Content-Range start in %q", value)
	}
	end, err = strconv.ParseInt(endText, 10, 64)
	if err != nil || end < start {
		return 0, 0, 0, fmt.Errorf("invalid Content-Range end in %q", value)
	}
	total = -1
	if totalText != "*" {
		total, err = strconv.ParseInt(totalText, 10, 64)
		if err != nil || total <= end {
			return 0, 0, 0, fmt.Errorf("invalid Content-Range total in %q", value)
		}
	}
	return start, end, total, nil
}

type downloadProgressReader struct {
	reader          io.Reader
	ctx             context.Context
	filename        string
	completedBefore int64
	initialBytes    int64
	currentBytes    int64
	fileExpected    int64
	total           int64
	nextReport      int64
}

func (r *downloadProgressReader) Read(buffer []byte) (int, error) {
	n, err := r.reader.Read(buffer)
	if n > 0 {
		r.currentBytes += int64(n)
		fileCurrent := r.initialBytes + r.currentBytes
		if r.nextReport == 0 {
			r.nextReport = downloadProgressInterval
		}
		if fileCurrent >= r.nextReport || (r.fileExpected > 0 && fileCurrent >= r.fileExpected) {
			overall := r.completedBefore + fileCurrent
			if r.total > 0 && overall > r.total {
				overall = r.total
			}
			reportDownloadProgress(r.ctx, overall, r.total, fmt.Sprintf("Downloading %s: %d bytes", r.filename, fileCurrent), r.filename)
			for r.nextReport <= fileCurrent {
				r.nextReport += downloadProgressInterval
			}
		}
	}
	return n, err
}

func reportDownloadProgress(ctx context.Context, current, total int64, message, filename string) {
	metadata := map[string]any{"kind": "step", "source": "fs.download"}
	if filename != "" {
		metadata["path"] = filename
	}
	xctx.ReportProgress(ctx, xctx.Progress{Current: current, Total: total, Unit: "bytes", Message: message, Metadata: metadata})
}

func writeDownloadReceipt(target string, result DownloadResult, provenance map[string]any) error {
	payload := map[string]any{
		"downloads":         result.Files,
		"total_bytes":       result.TotalBytes,
		"transferred_bytes": result.TransferredBytes,
	}
	if provenance != nil {
		payload["provenance"] = provenance
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	tmp := target + ".tmp"
	if removeErr := os.Remove(tmp); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return removeErr
	}
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(append(data, '\n'))
	closeErr := file.Close()
	if writeErr != nil {
		_ = os.Remove(tmp)
		return writeErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	return os.Rename(tmp, target)
}
