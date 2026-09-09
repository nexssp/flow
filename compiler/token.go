// nexssp/flow/compiler/token.go
package compiler

type TokenType int

const (
	TokenEOF TokenType = iota
	TokenIdent
	TokenString
	TokenNumber
	TokenArrow     // ->
	TokenPipe      // |
	TokenOr        // ||
	TokenAmpersand // &
	TokenLParen
	TokenRParen
	TokenLBrace
	TokenRBrace
	TokenQuestion // ?
	TokenColon    // :
	TokenAssign   // =
	TokenAt       // @
	TokenHash     // #
	TokenTilde    // ~
	TokenComma
	TokenInvalid
)

var tokenNames = map[TokenType]string{
	TokenEOF:       "EOF",
	TokenIdent:     "identifier",
	TokenString:    "string",
	TokenNumber:    "number",
	TokenArrow:     "->",
	TokenPipe:      "|",
	TokenOr:        "||",
	TokenAmpersand: "&",
	TokenLParen:    "(",
	TokenRParen:    ")",
	TokenLBrace:    "{",
	TokenRBrace:    "}",
	TokenQuestion:  "?",
	TokenColon:     ":",
	TokenAssign:    "=",
	TokenAt:        "@",
	TokenHash:      "#",
	TokenTilde:     "~",
	TokenComma:     ",",
}

func (t TokenType) String() string { return tokenNames[t] }

type Token struct {
	Type   TokenType
	Lit    string
	Line   int
	Col    int
	Offset int
}
