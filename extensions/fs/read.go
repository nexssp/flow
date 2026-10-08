package fs

import (
	"bufio"
	"bytes"
	"errors"
	"iter"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

const readBufferSize = 64 * 1024

// readBufferPool amortizes the read buffer across Read invocations.
var readBufferPool = sync.Pool{
	New: func() any {
		buf := make([]byte, readBufferSize)
		return &buf
	},
}

// FileContent is FileMeta plus the file body. fs.read replaces the
// stream item type from FileMeta to FileContent.
type FileContent struct {
	FileMeta
	Content []byte
}

// ReadConfig controls fs.read.
type ReadConfig struct {
	// Lines restricts the content to line ranges. Accepted forms:
	//
	//	"10"       single line
	//	"10-20"    inclusive range
	//	"10-"      from line 10 to end
	//	"1,3,5-7"  union of ranges
	//
	// Empty means the whole file.
	Lines string `json:"lines" cli:"lines"`
}

type lineRange struct {
	start int
	end   int // -1 = to end of file
}

// Read returns a stream operator that loads each file's bytes (or a
// selected subset of lines) into FileContent.
func Read(cfg ReadConfig) action.StreamOp[FileMeta, FileContent] {
	ranges, parseErr := parseLineRanges(cfg.Lines)
	if parseErr != nil {
		return func(iter.Seq2[FileMeta, error]) iter.Seq2[FileContent, error] {
			return func(yield func(FileContent, error) bool) {
				yield(FileContent{}, xerr.Validation("fs.read: "+parseErr.Error(), parseErr))
			}
		}
	}

	return func(up iter.Seq2[FileMeta, error]) iter.Seq2[FileContent, error] {
		return func(yield func(FileContent, error) bool) {
			bufPtr, ok := readBufferPool.Get().(*[]byte)
			if !ok {
				// Unreachable: readBufferPool.New always returns
				// *[]byte. Defensive guard so a future pool change
				// cannot panic here.
				fallback := make([]byte, readBufferSize)
				bufPtr = &fallback
			}
			buf := *bufPtr
			defer readBufferPool.Put(bufPtr)

			for meta, err := range up {
				if err != nil {
					yield(FileContent{}, err)
					return
				}

				content, readErr := readFileContent(meta.Path, ranges, buf)
				if readErr != nil {
					if !yield(FileContent{}, xerr.Internal("fs.read: "+meta.RelPath, readErr)) {
						return
					}
					continue
				}

				if !yield(FileContent{FileMeta: meta, Content: content}, nil) {
					return
				}
			}
		}
	}
}

func ReadOperator() action.NamedOperator {
	return action.NewOperator("fs.read", Read)
}

// readFileContent loads either the full file or the requested line
// ranges. Full reads reuse scratch when the file fits; line-range
// reads always allocate the result buffer.
func readFileContent(path string, ranges []lineRange, scratch []byte) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	if len(ranges) == 0 {
		if info, statErr := file.Stat(); statErr == nil && info.Size() <= int64(len(scratch)) {
			n, readErr := file.Read(scratch)
			if readErr == nil || n > 0 {
				return scratch[:n], nil
			}
		}
		return os.ReadFile(path)
	}

	var result bytes.Buffer
	scanner := bufio.NewScanner(file)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		if isLineIncluded(lineNo, ranges) {
			result.Write(scanner.Bytes())
			result.WriteByte('\n')
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result.Bytes(), nil
}

func isLineIncluded(line int, ranges []lineRange) bool {
	for _, r := range ranges {
		if line >= r.start && (r.end == -1 || line <= r.end) {
			return true
		}
	}
	return false
}

// parseLineRanges parses the "10,20-30,40-" spec into a slice of
// lineRange. Invalid entries are reported as an error.
func parseLineRanges(spec string) ([]lineRange, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, nil
	}

	var ranges []lineRange
	for part := range strings.SplitSeq(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		dash := strings.IndexByte(part, '-')
		if dash < 0 {
			line, err := strconv.Atoi(part)
			if err != nil || line < 1 {
				return nil, errors.New("invalid line number: " + part)
			}
			ranges = append(ranges, lineRange{start: line, end: line})
			continue
		}

		startText := strings.TrimSpace(part[:dash])
		endText := strings.TrimSpace(part[dash+1:])

		start, err := strconv.Atoi(startText)
		if err != nil || start < 1 {
			return nil, errors.New("invalid line start: " + startText)
		}

		end := -1
		if endText != "" {
			end, err = strconv.Atoi(endText)
			if err != nil || end < start {
				return nil, errors.New("invalid line end in range " + part)
			}
		}
		ranges = append(ranges, lineRange{start: start, end: end})
	}
	return ranges, nil
}
