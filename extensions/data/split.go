package data

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"iter"
	"strconv"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

type splitConfig struct {
	Partition  string `json:"partition" validate:"required"`
	Train      int    `json:"train"`
	Validation int    `json:"validation"`
	Test       int    `json:"test"`
	Seed       int64  `json:"seed"`
	Key        string `json:"key"`
}

// SplitOperator exposes a deterministic, bounded-memory partition selector.
func SplitOperator() action.NamedOperator {
	return action.NewOperator("data.split", Split)
}

// Split assigns each object to a partition by hashing its stable key and seed.
// It retains no rows and emits selected values as they arrive, so partition
// counts are approximate and independent of input order.
func Split(cfg splitConfig) action.StreamOp[any, any] {
	partition, keyField, err := validateSplitConfig(cfg)
	if err != nil {
		return streamError(err)
	}
	seedPrefix := strconv.AppendInt(nil, cfg.Seed, 10)
	seedPrefix = append(seedPrefix, 0)
	seedPrefix = append(seedPrefix, keyField...)
	seedPrefix = append(seedPrefix, 0)

	return func(upstream iter.Seq2[any, error]) iter.Seq2[any, error] {
		return func(yield func(any, error) bool) {
			streamSplitRows(upstream, yield, cfg, partition, keyField, seedPrefix)
		}
	}
}

func validateSplitConfig(cfg splitConfig) (partition, keyField string, err error) {
	partition = strings.ToLower(strings.TrimSpace(cfg.Partition))
	if partition != "train" && partition != "validation" && partition != "test" {
		return "", "", xerr.Validation("data.split: partition must be train, validation, or test")
	}
	if cfg.Train < 0 || cfg.Train > 100 || cfg.Validation < 0 || cfg.Validation > 100 || cfg.Test < 0 || cfg.Test > 100 || cfg.Train+cfg.Validation+cfg.Test != 100 {
		return "", "", xerr.Validation("data.split: train, validation, and test percentages must be between 0 and 100 and sum to 100")
	}
	keyField = strings.TrimSpace(cfg.Key)
	if keyField == "" {
		keyField = "id"
	}
	return partition, keyField, nil
}

func streamSplitRows(
	upstream iter.Seq2[any, error],
	yield func(any, error) bool,
	cfg splitConfig,
	partition, keyField string,
	seedPrefix []byte,
) {
	for value, upstreamErr := range upstream {
		if upstreamErr != nil {
			yield(nil, upstreamErr)
			return
		}
		row, ok := value.(map[string]any)
		if !ok || row == nil {
			yield(nil, xerr.BadRequest("data.split: input row is not a JSON object"))
			return
		}
		keyValue, ok := row[keyField]
		if !ok || keyValue == nil {
			yield(nil, xerr.BadRequest("data.split: stable key field "+keyField+" is missing or null"))
			return
		}
		encodedKey, err := json.Marshal(keyValue)
		if err != nil {
			yield(nil, xerr.BadRequest("data.split: stable key cannot be encoded as JSON: "+err.Error()))
			return
		}
		hashInput := make([]byte, 0, len(seedPrefix)+len(encodedKey))
		hashInput = append(hashInput, seedPrefix...)
		hashInput = append(hashInput, encodedKey...)
		digest := sha256.Sum256(hashInput)
		// % 100 yields [0, 99], so the cast to int is always safe.
		bucket := int(binary.BigEndian.Uint64(digest[:8]) % 100)
		if splitPartitionSelected(partition, bucket, cfg) && !yield(row, nil) {
			return
		}
	}
}

func splitPartitionSelected(partition string, bucket int, cfg splitConfig) bool {
	switch {
	case bucket < cfg.Train:
		return partition == "train"
	case bucket < cfg.Train+cfg.Validation:
		return partition == "validation"
	default:
		return partition == "test"
	}
}
