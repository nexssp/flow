package bytes

import (
	"context"
	"fmt"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

// ConcatAction creates bytes.concat, which joins collected []byte chunks
// without separators. Each item is checked rather than coerced from text or
// numeric arrays.
func ConcatAction() action.AnyAction {
	return action.New("bytes.concat", concatChunks).
		Description("Concatenate []byte chunks without changing their bytes").
		Build()
}

func concatChunks(_ context.Context, chunks []any) (string, error) {
	total := 0
	byteChunks := make([][]byte, len(chunks))
	for i, value := range chunks {
		chunk, ok := value.([]byte)
		if !ok {
			return "", xerr.BadRequest(fmt.Sprintf(
				"bytes.concat: chunk %d must be []byte, got %T", i, value))
		}
		byteChunks[i] = chunk
		total += len(chunk)
	}

	var joined strings.Builder
	joined.Grow(total)
	for _, chunk := range byteChunks {
		_, _ = joined.Write(chunk)
	}
	return joined.String(), nil
}
