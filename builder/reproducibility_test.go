package builder

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSourceDateEpochTime(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{name: "epoch zero", value: "0", want: "1970-01-01T00:00:00Z"},
		{name: "empty", value: "", wantErr: true},
		{name: "not a timestamp", value: "now", wantErr: true},
		{name: "negative", value: "-1", wantErr: true},
		{name: "integer overflow", value: "9223372036854775808", wantErr: true},
		{name: "year out of range", value: "9223372036854775807", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SOURCE_DATE_EPOCH", tc.value)
			got, set, err := sourceDateEpochTime()
			if !set {
				t.Fatal("SOURCE_DATE_EPOCH was not recognized as set")
			}
			if (err != nil) != tc.wantErr {
				t.Fatalf("sourceDateEpochTime() error = %v, wantErr %v", err, tc.wantErr)
			}
			if err == nil && got != tc.want {
				t.Fatalf("sourceDateEpochTime() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveBuildVersionHonorsSourceDateEpoch(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", "0")
	originalBuiltAt := BuiltAt
	BuiltAt = "different-from-epoch"
	t.Cleanup(func() { BuiltAt = originalBuiltAt })

	_, _, builtAt, err := resolveBuildVersion(context.Background(), "")
	if err != nil {
		t.Fatalf("resolveBuildVersion() error = %v", err)
	}
	if builtAt != "1970-01-01T00:00:00Z" {
		t.Fatalf("resolveBuildVersion() builtAt = %q, want SOURCE_DATE_EPOCH value", builtAt)
	}
}

func TestBuildReproducibleWithSourceDateEpoch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping standalone reproducibility integration test in short mode")
	}
	t.Setenv("SOURCE_DATE_EPOCH", "0")

	workDir := t.TempDir()
	flowPath := filepath.Join(workDir, "repro.nflow")
	if err := os.WriteFile(flowPath, []byte("@flag.key\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	originalBuiltAt := BuiltAt
	t.Cleanup(func() { BuiltAt = originalBuiltAt })
	var first []byte
	for i, builtAt := range []string{"first-cli-build", "second-cli-build"} {
		BuiltAt = builtAt
		binaryPath := filepath.Join(workDir, "repro-"+string(rune('1'+i))+exeSuffix())
		if code := runBuild([]string{flowPath, "-o", binaryPath}); code != 0 {
			t.Fatalf("standalone build %d returned %d", i+1, code)
		}
		binary, err := os.ReadFile(binaryPath)
		if err != nil {
			t.Fatalf("read standalone build %d: %v", i+1, err)
		}
		if i == 0 {
			first = binary
			continue
		}
		if !bytes.Equal(first, binary) {
			t.Fatal("standalone binaries differ despite identical source, build inputs, and SOURCE_DATE_EPOCH")
		}
	}
}
