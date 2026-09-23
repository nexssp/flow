package compiler

type TokenType int

const (
	TokenEOF TokenType = iota
	TokenIdent
	TokenString
	TokenNumber
	TokenArrow
	TokenPipe
	TokenOr
	TokenAmpersand
	TokenLParen
	TokenRParen
	TokenLBrace
	TokenRBrace
	TokenQuestion
	TokenColon
	TokenAssign
	TokenAtPrompt // @text  — free-form prompt annotation
	TokenAtBrace  // @{...} — structural inline arguments
	TokenHash
	TokenTilde
	TokenComma
	TokenLBracket // [
	TokenRBracket // ]
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
	TokenAtPrompt:  "@",
	TokenAtBrace:   "@{",
	TokenHash:      "#",
	TokenTilde:     "~",
	TokenComma:     ",",
	TokenLBracket:  "[",
	TokenRBracket:  "]",
}

func (t TokenType) String() string { return tokenNames[t] }

type Token struct {
	Type   TokenType
	Lit    string
	Line   int
	Col    int
	Offset int
}
