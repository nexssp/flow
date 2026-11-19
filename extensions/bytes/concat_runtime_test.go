package bytes_test

import (
	"bytes"
	"context"
	"iter"
	"testing"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/fs"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

type fileChunk = fs.FileContent

func TestNFlowStreamMapCollectConcatPreservesBinary(t *testing.T) {
	t.Parallel()

	allBytes := make([]byte, 256)
	for i := range allBytes {
		allBytes[i] = byte(i)
	}
	chunks := [][]byte{
		allBytes[:71],
		{0x00, 0xff, 0xfe},
		allBytes[71:193],
		allBytes[193:],
	}
	want := make([]byte, 0, 259)
	for _, chunk := range chunks {
		want = append(want, chunk...)
	}

	files := action.NewStream("probe.files", func(context.Context, struct{}) (iter.Seq2[fileChunk, error], error) {
		return func(yield func(fileChunk, error) bool) {
			for _, content := range chunks {
				if !yield(fs.FileContent{Content: content}, nil) {
					return
				}
			}
		}, nil
	})
	probe := compiler.Bundle{
		ID: "bytes_runtime_probe",
		Libraries: []action.Library{{
			Name:    "probe",
			Sources: []action.AnyStreamAction{files},
		}},
	}
	bundles := append(native.Bundles(), probe)
	cfg, err := runner.BuildConfig(bundles)
	if err != nil {
		t.Fatalf("BuildConfig() error = %v", err)
	}

	const source = "@schema ByteOutput {\n" +
		"  Content string `json:\"content\"`\n" +
		"}\n" +
		"@pipeline concat(): ByteOutput\n" +
		"  probe.files\n" +
		"    -> stream.map ( expression: \"item.Content\" )\n" +
		"    -> collect\n" +
		"    -> bytes.concat\n" +
		"    -> { content: . }\n" +
		"@end\n" +
		"pipeline.concat\n"

	result, err := runner.Execute(t.Context(), cfg, source, "bytes-stream-runtime.nflow", nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	output, ok := result.Output.(map[string]any)
	if !ok {
		t.Fatalf("output type = %T, want map[string]any", result.Output)
	}
	got, ok := output["content"].(string)
	if !ok {
		t.Fatalf("output content type = %T, want string", output["content"])
	}
	if !bytes.Equal([]byte(got), want) {
		t.Fatalf("output bytes = % x, want % x", []byte(got), want)
	}
}
