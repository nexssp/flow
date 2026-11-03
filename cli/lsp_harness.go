package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/nexssp/flow/builder"
	"github.com/nexssp/flow/extensions/require"
)

type lspHarnessEnsurer func(
	context.Context,
	[]require.Requirement,
	...string,
) (string, error)
type lspHarnessExecutor func(context.Context, string, io.Reader, io.Writer) int

func serveLSPWithProjectRequirements(ctx context.Context, inv *invocation, input io.Reader, output io.Writer) int {
	return runLSPWithProjectRequirements(ctx, inv, input, output, builder.EnsureHarnessCLIContext, executeLSPHarness)
}

// runLSPWithProjectRequirements reads and retains the initialize frame so it can
// select a project-linked process before the server answers the editor. The
// child then receives the original request and the rest of the same stdio stream.
func runLSPWithProjectRequirements(
	ctx context.Context,
	inv *invocation,
	input io.Reader,
	output io.Writer,
	ensure lspHarnessEnsurer,
	execute lspHarnessExecutor,
) int {
	reader := bufio.NewReader(input)
	firstBody, readErr := readLSPFrame(reader)
	if errors.Is(readErr, io.EOF) {
		readErr = nil
	}
	if readErr != nil {
		return fatalf("nflow lsp: %v", readErr)
	}

	var firstFrame []byte
	if firstBody != nil {
		firstFrame = encodeLSPFrame(firstBody)
	}
	if len(firstFrame) > 0 && inv != nil && os.Getenv(builder.HarnessEnv) == "" {
		if root := lspInitializeRootPath(firstBody); root != "" {
			files, err := discoverLSPProjectFiles(root)
			if err != nil {
				slog.Warn("nflow lsp: could not scan workspace requirements", "root", root, "error", err)
			} else {
				requirements, requirementErrors := inspectLintRequirements(ctx, inv, files)
				usableFiles := make([]string, 0, len(files))
				for _, path := range files {
					if _, invalid := requirementErrors[path]; !invalid {
						usableFiles = append(usableFiles, path)
					}
				}
				for path, requirementErr := range requirementErrors {
					slog.Debug("nflow lsp: skipping requirements from an unreadable or invalid workspace source", "file", path, "error", requirementErr)
				}
				if requirementsNeedExternalHarness(inv, requirements) {
					if ensure == nil {
						ensure = builder.EnsureHarnessCLIContext
					}
					if execute == nil {
						execute = executeLSPHarness
					}
					binary, ensureErr := ensure(ctx, requirements, usableFiles...)
					if ensureErr != nil {
						slog.Warn("nflow lsp: could not prepare project @require bundles; continuing with linked bundles", "error", ensureErr)
					} else {
						forwardedInput := io.MultiReader(bytes.NewReader(firstFrame), reader)
						return execute(ctx, binary, forwardedInput, output)
					}
				}
			}
		}
	}

	server := newFlowLanguageServer(ctx, inv)
	forwardedInput := io.MultiReader(bytes.NewReader(firstFrame), reader)
	if err := server.serve(forwardedInput, output); err != nil {
		return fatalf("nflow lsp: %v", err)
	}
	return server.exitCode
}

func encodeLSPFrame(body []byte) []byte {
	var frame bytes.Buffer
	_, _ = fmt.Fprintf(&frame, "Content-Length: %d\r\n\r\n", len(body))
	_, _ = frame.Write(body)
	return frame.Bytes()
}

func lspInitializeRootPath(body []byte) string {
	message, err := decodeLSPMessage(body)
	if err != nil || message.Method != lspInitialize {
		return ""
	}
	var params struct {
		RootURI          string `json:"rootUri"`
		WorkspaceFolders []struct {
			URI string `json:"uri"`
		} `json:"workspaceFolders"`
	}
	if err := json.Unmarshal(message.Params, &params); err != nil {
		return ""
	}
	if root := lspFileURIPath(params.RootURI); root != "" {
		return root
	}
	for _, folder := range params.WorkspaceFolders {
		if root := lspFileURIPath(folder.URI); root != "" {
			return root
		}
	}
	return ""
}

// discoverLSPProjectFiles uses the same source exclusions as project lint, but
// an empty editor workspace is valid and must not prevent the LSP from starting.
func discoverLSPProjectFiles(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		if filepath.Ext(info.Name()) == ".nflow" {
			return []string{filepath.Clean(root)}, nil
		}
		return nil, nil
	}

	seen := make(map[string]struct{})
	files := make([]string, 0)
	add := func(path string) {
		clean := filepath.Clean(path)
		identity, absErr := filepath.Abs(clean)
		if absErr != nil {
			identity = clean
		}
		if _, exists := seen[identity]; exists {
			return
		}
		seen[identity] = struct{}{}
		files = append(files, clean)
	}
	if _, err := walkLintSources(root, add); err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func executeLSPHarness(ctx context.Context, binary string, input io.Reader, output io.Writer) int {
	cmd := exec.CommandContext(ctx, binary, "lsp")
	cmd.Stdin = input
	cmd.Stdout = output
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), builder.HarnessEnv+"=1")
	if err := cmd.Run(); err != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			return exitErr.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "nflow lsp: harness execution: %v\n", err)
		return 1
	}
	return 0
}
