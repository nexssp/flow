package fs

import (
	"bufio"
	"context"
	"errors"
	"os"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

// ReadLinesRequest identifies a text file and an optional inline comment marker.
type ReadLinesRequest struct {
	Path          string `json:"path" validate:"required"`
	CommentPrefix string `json:"comment_prefix,omitempty"`
}

// ReadLinesAction reads non-empty, trimmed lines from a local text file.
var ReadLinesAction = action.New("fs.read_lines", readLines).
	Description("Read trimmed, non-empty lines from a text file (maximum 16 MiB per line), optionally stripping comments").
	Tag("fs", "read").
	Build()

func readLines(ctx context.Context, req ReadLinesRequest) (out []string, err error) {
	path := strings.TrimSpace(req.Path)
	if path == "" {
		return nil, xerr.BadRequest("fs.read_lines: path is required")
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}

	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, xerr.NotFound("fs.read_lines: file not found", err)
		}
		return nil, xerr.Internal("fs.read_lines: could not open file", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, xerr.Internal("fs.read_lines: could not close file", closeErr))
		}
	}()

	lines := make([]string, 0)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, readBufferSize), maxTextScannerBytes)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := checkTextLineLimit(scanner.Bytes()); err != nil {
			return nil, xerr.Internal("fs.read_lines: could not scan file", err)
		}
		line := scanner.Text()
		if req.CommentPrefix != "" {
			if commentAt := strings.Index(line, req.CommentPrefix); commentAt >= 0 {
				line = line[:commentAt]
			}
		}
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return nil, xerr.Internal("fs.read_lines: could not scan file", scanErr)
	}
	return lines, nil
}
