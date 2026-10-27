package lang

import (
	"context"
	"fmt"
	"strings"
)

// resolveRunCmd selects the ordinary interpreter unless the source opts into
// a runner's dependency tool by containing its configured marker. The file
// path remains a single argument even when it contains spaces.
func resolveRunCmd(ctx context.Context, lang *Language, block *LangBlock, filePath string) (cmd string, args []string, err error) {
	if depCmd, depArgs, handled, depErr := resolveDepsCmd(lang, block.Source, filePath); handled {
		return depCmd, depArgs, depErr
	}

	cmd, args, err = resolveContext(ctx, lang, block.Version)
	if err != nil {
		return "", nil, err
	}
	return cmd, append(args, filePath), nil
}

// resolveDepsCmd reports handled=true only when the source uses a configured
// dependency marker. It deliberately does not cache arguments: each execution
// gets its own materialized file path.
func resolveDepsCmd(lang *Language, source, filePath string) (cmd string, args []string, handled bool, err error) {
	if lang == nil || lang.spec == nil || lang.spec.Deps == "" || lang.spec.DepsMarker == "" || !strings.Contains(source, lang.spec.DepsMarker) {
		return "", nil, false, nil
	}

	parts := strings.Fields(lang.spec.Deps)
	if len(parts) < 2 || !strings.Contains(strings.Join(parts[1:], " "), "{file}") {
		return "", nil, true, fmt.Errorf("lang.%s: invalid dependency command template", lang.ID)
	}
	cmd, err = lookPathCmd(parts[0])
	if err != nil {
		return "", nil, true, &NotInstalledError{
			ID:      lang.ID,
			Version: lang.Version(),
			Cause:   fmt.Errorf("dependency runner %q not found on PATH; install it to run scripts with %q", parts[0], lang.spec.DepsMarker),
		}
	}

	args = make([]string, len(parts)-1)
	for i, part := range parts[1:] {
		args[i] = strings.ReplaceAll(part, "{file}", filePath)
	}
	return cmd, args, true, nil
}
