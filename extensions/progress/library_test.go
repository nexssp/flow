package progress

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestBundle_WiresLibraryAndFixtures(t *testing.T) {
	b := Bundle(nil)
	ktest.RequireEqual(t, b.ID, ID)
	ktest.RequireLen(t, b.Libraries, 1)
	ktest.RequireNotNil(t, b.WrapPipeline)
	ktest.RequireNotNil(t, b.Fixtures)

	fields := b.ArgSchemas["progress.wrap"]
	ktest.RequireLen(t, fields, 1)
	ktest.RequireEqual(t, fields[0].Name, "action")

	sections := b.AllSelfTests()
	ktest.RequireLen(t, sections, 1)
	ktest.RequireEqual(t, sections[0].Name, ID)
	ktest.RequireLen(t, sections[0].Features, 1)
	ktest.RequireEqual(t, sections[0].Features[0].Name, "Progress step and wrap")
	ktest.RequireCondition(t, sections[0].Features[0].DSL != "", "progress fixture has no DSL")
}

func TestReadMessageOption(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		values     map[string]any
		capability string
		want       string
		wantErr    bool
	}{
		{name: "absent", values: map[string]any{}, capability: "progress.step"},
		{name: "valid", values: map[string]any{"message": "Loading"}, capability: "progress.step", want: "Loading"},
		{name: "invalid step", values: map[string]any{"message": 42}, capability: "progress.step", wantErr: true},
		{name: "invalid wrap", values: map[string]any{"message": true}, capability: "progress.wrap", wantErr: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := readMessageOption(testCase.values, testCase.capability)
			if testCase.wantErr {
				ktest.RequireErrorContains(t, err, testCase.capability)
				ktest.RequireErrorContains(t, err, "must be a string")
				return
			}
			ktest.RequireNoError(t, err)
			ktest.RequireEqual(t, got, testCase.want)
		})
	}
}

func TestExtractStepInputRejectsNonStringMessage(t *testing.T) {
	_, _, err := extractStepInput(map[string]any{"message": 42, "x": 1})
	ktest.RequireErrorContains(t, err, "progress.step")
	ktest.RequireErrorContains(t, err, "must be a string")
}

func TestStripWrapKeys(t *testing.T) {
	root := map[string]any{"name": "Ada"}
	for _, testCase := range []struct {
		name  string
		input map[string]any
		want  any
	}{
		{
			name: "unwraps root after removing wrapper keys",
			input: map[string]any{
				"action":   "runtime.noop",
				"message":  "Running",
				"__root__": root,
			},
			want: root,
		},
		{
			name: "preserves explicit payload fields",
			input: map[string]any{
				"action":  "runtime.noop",
				"message": "Running",
				"query":   "hello",
			},
			want: map[string]any{"query": "hello"},
		},
		{
			name: "preserves payload beside root",
			input: map[string]any{
				"action":   "runtime.noop",
				"__root__": root,
				"query":    "hello",
			},
			want: map[string]any{"query": "hello"},
		},
		{
			name:  "returns empty object when no payload exists",
			input: map[string]any{"action": "runtime.noop"},
			want:  map[string]any{},
		},
		{
			name:  "unwraps nil root",
			input: map[string]any{"action": "runtime.noop", "__root__": nil},
			want:  nil,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ktest.RequireEqual(t, stripWrapKeys(testCase.input), testCase.want)
		})
	}
}
