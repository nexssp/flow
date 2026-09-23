package fsio

import (
	"bufio"
	"bytes"
	"fmt"
	"iter"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/nexssp/kernel/action"
)

const defaultBufferSize = 64 * 1024

var bufferPool = sync.Pool{
	New: func() any {
		b := make([]byte, defaultBufferSize)
		return &b
	},
}

type FileContent struct {
	FileMeta
	Content []byte
}

type ReadConfig struct {
	Lines string `json:"lines"`
}

type lineRange struct {
	start int
	end   int
}

func Read(cfg ReadConfig) action.StreamOp[FileMeta, FileContent] {
	ranges, _ := parseLineRanges(cfg.Lines)

	return func(up iter.Seq2[FileMeta, error]) iter.Seq2[FileContent, error] {
		return func(yield func(FileContent, error) bool) {
			bufPtr := bufferPool.Get().(*[]byte)
			buf := *bufPtr
			defer bufferPool.Put(bufPtr)

			for meta, err := range up {
				if err != nil {
					var zero FileContent
					yield(zero, err)
					return
				}

				content, readErr := readFileContent(meta.Path, ranges, buf)
				if readErr != nil {
					var zero FileContent
					if !yield(zero, fmt.Errorf("read %q: %w", meta.RelPath, readErr)) {
						return
					}
					continue
				}

				item := FileContent{
					FileMeta: meta,
					Content:  content,
				}

				if !yield(item, nil) {
					return
				}
			}
		}
	}
}

func ReadOperator() action.NamedOperator {
	return action.NewOperator("fs.read", Read)
}

func readFileContent(path string, ranges []lineRange, scratch []byte) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	if len(ranges) == 0 {
		stat, serr := file.Stat()
		if serr == nil && stat.Size() <= int64(len(scratch)) {
			n, rerr := file.Read(scratch)
			if rerr == nil || n > 0 {
				return scratch[:n], nil
			}
		}
		return os.ReadFile(path)
	}

	var result bytes.Buffer
	scanner := bufio.NewScanner(file)
	currentLine := 0

	for scanner.Scan() {
		currentLine++
		if isLineIncluded(currentLine, ranges) {
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

func parseLineRanges(spec string) ([]lineRange, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, nil
	}

	var ranges []lineRange
	parts := strings.Split(spec, ",")

	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		if dash := strings.IndexByte(p, '-'); dash >= 0 {
			startStr := strings.TrimSpace(p[:dash])
			endStr := strings.TrimSpace(p[dash+1:])

			start, err := strconv.Atoi(startStr)
			if err != nil || start < 1 {
				return nil, fmt.Errorf("invalid line start: %q", startStr)
			}

			end := -1
			if endStr != "" {
				var err error
				end, err = strconv.Atoi(endStr)
				if err != nil || end < start {
					return nil, fmt.Errorf("invalid line end in range %q", p)
				}
			}

			ranges = append(ranges, lineRange{start: start, end: end})
		} else {
			line, err := strconv.Atoi(p)
			if err != nil || line < 1 {
				return nil, fmt.Errorf("invalid line number: %q", p)
			}
			ranges = append(ranges, lineRange{start: line, end: line})
		}
	}

	return ranges, nil
}
