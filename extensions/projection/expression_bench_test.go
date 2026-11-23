package projection

import "testing"

const benchmarkProjectionShorthandSource = `let profile = .user.profile; display: profile.name, user_id, .account.owner.id, state: { status, code: .result.code }, values: [name, { label: "literal, name", active: enabled }], note: "kept as text" # trailing comment`

var (
	projectionShorthandFieldSpansBenchmarkSink []projectionIdentifierSpan
	projectionShorthandRewriteBenchmarkSink    string
)

func BenchmarkProjectionShorthandFieldSpans(b *testing.B) {
	source := benchmarkProjectionShorthandSource
	b.ReportAllocs()
	b.SetBytes(int64(len(source)))
	for range b.N {
		projectionShorthandFieldSpansBenchmarkSink = projectionShorthandFieldSpans(source)
	}
}

func BenchmarkProjectionShorthandRewrite(b *testing.B) {
	source := benchmarkProjectionShorthandSource
	b.ReportAllocs()
	b.SetBytes(int64(len(source)))
	for range b.N {
		projectionShorthandRewriteBenchmarkSink = expandProjectionShorthand(source)
	}
}
