package selftest

import "errors"

type skippableError interface {
	SelfTestSkipReason() string
}

func selfTestSkipReason(err error) string {
	var skippable skippableError
	if errors.As(err, &skippable) {
		return skippable.SelfTestSkipReason()
	}
	return ""
}
