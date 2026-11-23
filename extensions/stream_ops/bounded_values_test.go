package stream_ops

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xerr"
)

type marshaledStreamValue struct{}

type namedStreamByte uint8

type customStreamByte uint8

type unexportedScalarStreamFields struct {
	ID int
}

type streamValueWithUnexportedScalarEmbed struct {
	unexportedScalarStreamFields
}

func (marshaledStreamValue) MarshalJSON() ([]byte, error) {
	return []byte(`"opaque"`), nil
}

func (customStreamByte) MarshalJSON() ([]byte, error) {
	return []byte(`0`), nil
}

func TestPrepareStreamItemEnforcesExactAccountedBoundary(t *testing.T) {
	item := map[string]any{
		"label":  "bounded",
		"values": []int{1, 2, 3},
	}
	copied, required, err := prepareStreamItem(item, uint64(maxStreamBufferBytes))
	if err != nil {
		t.Fatalf("prepare item within default budget: %v", err)
	}
	if required == 0 {
		t.Fatal("accounted size is zero")
	}
	if _, exact, err := prepareStreamItem(item, required); err != nil || exact != required {
		t.Fatalf("exact budget result = (%d, %v), want (%d, nil)", exact, err, required)
	}
	if _, _, err := prepareStreamItem(item, required-1); !errors.Is(err, errStreamBudgetExceeded) {
		t.Fatalf("one-byte-short budget error = %v, want %v", err, errStreamBudgetExceeded)
	}
	encoded, err := json.Marshal(copied)
	if err != nil {
		t.Fatalf("marshal detached item: %v", err)
	}
	if uint64(len(encoded)) > required {
		t.Fatalf("encoded size %d exceeds accounted size %d", len(encoded), required)
	}
}

func TestPrepareStreamItemAccountsJSONFloatFormatting(t *testing.T) {
	for _, item := range []any{1e-6, -1e-6, 1e20, 1e21, float32(1e-6), math.SmallestNonzeroFloat64} {
		copied, accounted, err := prepareStreamItem(item, uint64(maxStreamBufferBytes))
		if err != nil {
			t.Fatalf("prepare %v (%T): %v", item, item, err)
		}
		encoded, err := json.Marshal(copied)
		if err != nil {
			t.Fatalf("marshal %v (%T): %v", item, item, err)
		}
		if uint64(len(encoded)) > accounted {
			t.Fatalf("JSON size %d exceeds accounted size %d for %v (%T)", len(encoded), accounted, item, item)
		}
	}
}

func TestPrepareStreamItemHandlesNamedByteSliceEncoding(t *testing.T) {
	item := []namedStreamByte{1, 2, 255}
	copied, accounted, err := prepareStreamItem(item, uint64(maxStreamBufferBytes))
	if err != nil {
		t.Fatalf("prepare named-byte slice: %v", err)
	}
	encoded, err := json.Marshal(copied)
	if err != nil {
		t.Fatalf("marshal named-byte slice: %v", err)
	}
	if uint64(len(encoded)) > accounted {
		t.Fatalf("JSON size %d exceeds accounted size %d", len(encoded), accounted)
	}
	if _, _, err := prepareStreamItem([]customStreamByte{1, 2}, uint64(maxStreamBufferBytes)); !errors.Is(err, errUnsupportedStreamValue) {
		t.Fatalf("custom byte-element marshaler error = %v, want %v", err, errUnsupportedStreamValue)
	}
}

func TestPrepareStreamItemDetachesSliceCapacityAndMapStorage(t *testing.T) {
	largeCapacity := make([]int, 2, 4096)
	largeCapacity[0], largeCapacity[1] = 7, 9
	largeMap := make(map[string]any, 4096)
	largeMap["values"] = largeCapacity

	copied, _, err := prepareStreamItem(largeMap, uint64(maxStreamBufferBytes))
	if err != nil {
		t.Fatalf("prepare item with spare capacity: %v", err)
	}
	copiedMap, ok := copied.(map[string]any)
	if !ok {
		t.Fatalf("detached map type = %T, want map[string]any", copied)
	}
	copiedSlice, ok := copiedMap["values"].([]int)
	if !ok {
		t.Fatalf("detached slice type = %T, want []int", copiedMap["values"])
	}
	if cap(copiedSlice) != len(copiedSlice) {
		t.Fatalf("detached slice capacity = %d, want length %d", cap(copiedSlice), len(copiedSlice))
	}
	if copiedSlice[0] != 7 || copiedSlice[1] != 9 {
		t.Fatalf("detached slice contents = %v", copiedSlice)
	}
	if copiedMap["values"] == nil && largeMap["values"] != nil {
		t.Fatal("detached map lost its slice value")
	}
}

func TestPrepareStreamItemAccountsCopiedMapKeys(t *testing.T) {
	shortKey := map[string]int{"a": 1}
	longKey := map[string]int{strings.Repeat("k", 257): 1}
	_, shortSize, err := prepareStreamItem(shortKey, uint64(maxStreamBufferBytes))
	if err != nil {
		t.Fatalf("prepare short-key map: %v", err)
	}
	_, longSize, err := prepareStreamItem(longKey, uint64(maxStreamBufferBytes))
	if err != nil {
		t.Fatalf("prepare long-key map: %v", err)
	}
	const minimumDelta uint64 = 512
	if longSize-shortSize < minimumDelta {
		t.Fatalf("long-key accounting delta = %d, want at least %d", longSize-shortSize, minimumDelta)
	}
}

func TestPrepareStreamItemAccountsOmittedEmptyMapStorage(t *testing.T) {
	type valueWithOptionalMap struct {
		Values map[string]any `json:"values,omitempty"`
	}

	_, emptySize, err := prepareStreamItem(valueWithOptionalMap{Values: map[string]any{}}, uint64(maxStreamBufferBytes))
	if err != nil {
		t.Fatalf("prepare value with empty map: %v", err)
	}
	_, nilSize, err := prepareStreamItem(valueWithOptionalMap{}, uint64(maxStreamBufferBytes))
	if err != nil {
		t.Fatalf("prepare value with nil map: %v", err)
	}
	if emptySize-nilSize < mapHeaderAllowance-2 {
		t.Fatalf("empty-map accounting delta = %d, want at least %d", emptySize-nilSize, mapHeaderAllowance-2)
	}
}

func TestPrepareStreamItemRejectsUnsupportedOrCyclicValues(t *testing.T) {
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	cases := []struct {
		name string
		item any
	}{
		{name: "non-string map key", item: map[int]string{1: "value"}},
		{name: "custom JSON marshaler", item: marshaledStreamValue{}},
		{name: "cyclic map", item: cyclic},
		{name: "non-finite number", item: math.NaN()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := prepareStreamItem(tc.item, uint64(maxStreamBufferBytes)); !errors.Is(err, errUnsupportedStreamValue) {
				t.Fatalf("prepare unsupported item error = %v, want %v", err, errUnsupportedStreamValue)
			}
		})
	}
}

func TestPrepareStreamItemRejectsUnexportedScalarEmbeddedStruct(t *testing.T) {
	item := streamValueWithUnexportedScalarEmbed{
		unexportedScalarStreamFields{ID: 123},
	}
	_, _, err := prepareStreamItem(item, uint64(maxStreamBufferBytes))
	if !errors.Is(err, errUnsupportedStreamValue) {
		t.Fatalf("prepare embedded scalar struct error = %v, want %v", err, errUnsupportedStreamValue)
	}
	if kind := xerr.KindFrom(streamBufferError("stream.collect", err)); kind != xerr.KindValidation {
		t.Fatalf("embedded scalar stream error kind = %s, want %s", kind, xerr.KindValidation)
	}
}

func TestPrepareStreamItemRejectsOversizedStringsBeforeCopy(t *testing.T) {
	item := strings.Repeat("x", 1024)
	if _, _, err := prepareStreamItem(item, 128); !errors.Is(err, errStreamBudgetExceeded) {
		t.Fatalf("oversized string error = %v, want %v", err, errStreamBudgetExceeded)
	}
}

func TestStreamBufferErrorPreservesValidationCause(t *testing.T) {
	cause := fmt.Errorf("measure item: %w", errStreamBudgetExceeded)
	err := streamBufferError("stream.collect", cause)
	if !errors.Is(err, errStreamBudgetExceeded) {
		t.Fatalf("stream error %v does not preserve budget cause", err)
	}
	if kind := xerr.KindFrom(err); kind != xerr.KindValidation {
		t.Fatalf("stream error kind = %s, want %s", kind, xerr.KindValidation)
	}
}
