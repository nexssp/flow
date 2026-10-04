package core

// PrimaryExtension przechwytuje jeden token-form i zamienia go w węzeł
// AST. W parsePrimary extensions mają pierwszeństwo przed built-inami.
//
// Kontrakt jest celowo minimalny: rozszerzenie implementuje Name i
// Parse, oraz — w zależności od tego, czym rozszerza parser — jedno z:
//
//	TokenType() TokenType  — dispatch po tokenie (np. TokAtPrompt)
//	Keyword()   string     — dispatch po nazwie identu (np. "loop")
//
// Rozszerzenie może implementować jedno, drugie, albo oba.
type PrimaryExtension interface {
	Name() string
	Parse(p *Parser) (Expr, error)
}

// TokenPrimary is the token-dispatch contract.
type TokenPrimary interface {
	PrimaryExtension
	TokenType() TokenType
}

// KeywordPrimary is the keyword-dispatch contract.
type KeywordPrimary interface {
	PrimaryExtension
	Keyword() string
}

type PrimaryExtensionTable struct {
	byToken   map[TokenType]PrimaryExtension
	byKeyword map[string]PrimaryExtension
	ordered   []PrimaryExtension
}

func NewPrimaryExtensionTable(exts ...PrimaryExtension) *PrimaryExtensionTable {
	t := &PrimaryExtensionTable{
		byToken:   make(map[TokenType]PrimaryExtension, len(exts)),
		byKeyword: make(map[string]PrimaryExtension, len(exts)),
		ordered:   append([]PrimaryExtension(nil), exts...),
	}
	for _, ext := range exts {
		if ext == nil {
			panic("core: nil primary extension")
		}
		if ext.Name() == "" {
			panic("core: primary extension with empty Name()")
		}
		if tp, ok := ext.(TokenPrimary); ok {
			t.byToken[tp.TokenType()] = mergeOrPanic(
				t.byToken[tp.TokenType()], ext,
				"duplicate primary extension for token "+tp.TokenType().String(),
			)
		}
		if kp, ok := ext.(KeywordPrimary); ok {
			kw := kp.Keyword()
			if kw == "" {
				panic("core: keyword primary extension " + ext.Name() + " with empty Keyword()")
			}
			t.byKeyword[kw] = mergeOrPanic(
				t.byKeyword[kw], ext,
				"duplicate primary extension for keyword "+kw,
			)
		}
	}
	return t
}

// mergeOrPanic returns newer when existing is zero, calls MergeWith when
// newer opts in, and panics otherwise. The zero-value check is what lets
// the caller use a single line per token/keyword slot.
func mergeOrPanic(existing, newer PrimaryExtension, panicMsg string) PrimaryExtension {
	if existing == nil {
		return newer
	}
	if m, ok := newer.(MergeablePrimary); ok {
		return m.MergeWith(existing)
	}
	panic("core: " + panicMsg)
}

func (t *PrimaryExtensionTable) ByToken(tt TokenType) (PrimaryExtension, bool) {
	if t == nil {
		return nil, false
	}
	ext, ok := t.byToken[tt]
	return ext, ok
}

func (t *PrimaryExtensionTable) ByKeyword(kw string) (PrimaryExtension, bool) {
	if t == nil {
		return nil, false
	}
	ext, ok := t.byKeyword[kw]
	return ext, ok
}

func (t *PrimaryExtensionTable) All() []PrimaryExtension {
	if t == nil {
		return nil
	}
	return append([]PrimaryExtension(nil), t.ordered...)
}

// DefaultPrimaryExtensions zwraca PUSTĄ tabelę. Core nie zna żadnego
// konkretnego rozszerzenia.
func DefaultPrimaryExtensions() *PrimaryExtensionTable {
	return NewPrimaryExtensionTable()
}

// MergeablePrimary is implemented by primary extensions that know how to
// combine themselves with an already-installed primary handling the same
// token or keyword.
//
// NewPrimaryExtensionTable calls MergeWith instead of panicking when it
// encounters a duplicate and the newer extension implements this
// interface. The older extension is passed as the argument; the newer
// decides how to combine them.
//
// This is what lets a bundle whose primary carries per-compilation
// state — the macros bundle is the canonical case — be installed more
// than once during a sub-pipeline compile without triggering the
// "duplicates fail loudly" panic. A genuine cross-bundle conflict (two
// unrelated bundles claiming the same token) still panics, because
// neither implements the interface.
type MergeablePrimary interface {
	PrimaryExtension
	MergeWith(older PrimaryExtension) PrimaryExtension
}
