package cli

import (
	"errors"
	"fmt"
	"os"
)

// readSourceFile reads a .nflow source and returns an actionable error
// when the path exists but is not a readable regular file.
//
// os.ReadFile on a directory produces a platform-specific errno that
// does not name the problem:
//
//   - Windows: "Incorrect function." (ERROR_INVALID_FUNCTION)
//   - Unix:    "Is a directory"
//
// This helper replaces that with a plain message naming the path and
// what nflow expected.
func readSourceFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("read %s: file not found", path)
		}
		return nil, fmt.Errorf("read %s: %w", path, unwrapIOError(err))
	}
	if info.IsDir() {
		return nil, fmt.Errorf(
			"read %s: is a directory; nflow expects a .nflow file "+
				"(did you mean a file inside it?)",
			path,
		)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("read %s: file not found", path)
		}
		return nil, fmt.Errorf("read %s: %w", path, unwrapIOError(err))
	}
	return data, nil
}

// unwrapIOError strips the redundant path from an *os.PathError so the
// caller's message does not print the same path twice.
func unwrapIOError(err error) error {
	var pathErr *os.PathError
	if errors.As(err, &pathErr) && pathErr.Err != nil {
		return pathErr.Err
	}
	return err
}
