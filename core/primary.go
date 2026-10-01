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
			tt := tp.TokenType()
			if _, dup := t.byToken[tt]; dup {
				panic("core: duplicate primary extension for token " + tt.String())
			}
			t.byToken[tt] = ext
		}
		if kp, ok := ext.(KeywordPrimary); ok {
			kw := kp.Keyword()
			if kw == "" {
				panic("core: keyword primary extension " + ext.Name() + " with empty Keyword()")
			}
			if _, dup := t.byKeyword[kw]; dup {
				panic("core: duplicate primary extension for keyword " + kw)
			}
			t.byKeyword[kw] = ext
		}
	}
	return t
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
