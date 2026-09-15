package runner

import "github.com/nexssp/ai/llm"

// WrapProvider wraps inner so every Complete call is reported to
// observer. The real implementation lives in ai/llm; this shim keeps
// runner call sites free of the llm package's wrapper type and keeps
// the observer type out of ai/llm.
//
// If either argument is nil the function returns inner unchanged, so
// callers can chain it unconditionally:
//
//	provider := runner.WrapProvider(baseProvider, obs)
func WrapProvider(inner llm.Provider, observer *RunnerObserver) llm.Provider {
	if inner == nil || observer == nil {
		return inner
	}

	return llm.NewObservableProvider(inner, observer.ProviderTrace)
}
