package core

// ── Native DSL keywords ──────────────────────────────────────────────
//
// Native keywords are grammar-level conveniences that compile to
// canonical action IDs at parse time. They are part of the language,
// not registry aliases: bundles cannot extend or override them, and
// the mapping is validated at boot against the live resolver.

// KeywordMapping pairs a native DSL keyword with the canonical action
// name it compiles to.
type KeywordMapping struct {
	Keyword string
	Target  string
}

// keywordList is the canonical, ordered list of native keywords. It is
// the source of truth; nativeKeywords is the O(1) lookup index derived
// from it.
var keywordList = []KeywordMapping{
	{Keyword: "const", Target: "runtime.const"},
	{Keyword: "with", Target: "runtime.with"},
	{Keyword: "pick", Target: "runtime.pick"},
	{Keyword: "wrap", Target: "runtime.wrap"},
	{Keyword: "fail", Target: "runtime.fail"},
	{Keyword: "noop", Target: "runtime.noop"},
	{Keyword: "sleep", Target: "runtime.sleep"},
	{Keyword: "env", Target: "runtime.env"},
	{Keyword: "uuid", Target: "runtime.uuid"},
	{Keyword: "json", Target: "json.clean"},
}

var nativeKeywords = func() map[string]string {
	m := make(map[string]string, len(keywordList))
	for _, km := range keywordList {
		m[km.Keyword] = km.Target
	}
	return m
}()

// KeywordMappings returns the canonical keyword list. The returned
// slice is owned by core and must not be mutated.
func KeywordMappings() []KeywordMapping {
	return keywordList
}

// TranslateKeyword resolves a parsed atom name to its canonical action
// name. It returns ("", false) when name is not a native keyword.
func TranslateKeyword(name string) (string, bool) {
	target, ok := nativeKeywords[name]
	return target, ok
}

// ── Reserved action names ────────────────────────────────────────────

// reservedActionNames are bare action names a bundle may not register.
// They are the native keywords: the parser translates those words into
// canonical actions before resolution, so a bundle that registered an
// action named "const" would be unreachable and would shadow grammar.
var reservedActionNames = map[string]bool{
	"const": true, "with": true, "pick": true, "wrap": true,
	"fail": true, "noop": true, "sleep": true,
	"env": true, "uuid": true, "json": true,
}

// IsReservedActionName reports whether name is reserved by the nflow
// grammar.
func IsReservedActionName(name string) bool {
	return reservedActionNames[name]
}

// ── Modifier ownership ───────────────────────────────────────────────

// ModifierOwner identifies which subsystem owns a modifier name.
type ModifierOwner uint8

const (
	// OwnerBundle means the modifier is owned by the bundle that
	// declares it. Any bundle may declare a modifier with this owner.
	OwnerBundle ModifierOwner = iota
	// OwnerKernel means the modifier is a standard Kernel policy
	// modifier. Only the modifiers_core bundle may declare it.
	OwnerKernel
	// OwnerGrammar means the modifier is owned by the nflow grammar.
	// No bundle may declare it.
	OwnerGrammar
)

// kernelOwnedModifiers are the standard Kernel policy modifiers. They
// may only be declared with OwnerKernel, by the modifiers_core bundle.
var kernelOwnedModifiers = map[string]bool{
	"timeout":     true,
	"retry":       true,
	"cache":       true,
	"dedup":       true,
	"coalesce":    true,
	"rate_limit":  true,
	"breaker":     true,
	"concurrency": true,
	"idempotent":  true,
}

// grammarOwnedModifiers are modifier names owned by the nflow grammar.
// Phase 1 declares the set but does not register any of them yet; using
// one today produces "unknown modifier".
var grammarOwnedModifiers = map[string]bool{
	"profile": true,
}

// RequiredModifierOwner returns the owner required by a modifier name.
// A name not in any reserved set requires OwnerBundle.
func RequiredModifierOwner(name string) ModifierOwner {
	switch {
	case kernelOwnedModifiers[name]:
		return OwnerKernel
	case grammarOwnedModifiers[name]:
		return OwnerGrammar
	default:
		return OwnerBundle
	}
}

// ModifierOwnerName renders an owner for use in error messages.
func ModifierOwnerName(owner ModifierOwner) string {
	switch owner {
	case OwnerKernel:
		return "the Kernel policy provider"
	case OwnerGrammar:
		return "the nflow grammar"
	case OwnerBundle:
		return "the declaring bundle"
	default:
		return "an unknown owner"
	}
}
