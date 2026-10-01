package core

type TokenType uint8

const (
	TokEOF TokenType = iota
	TokIdent
	TokString
	TokNumber
	TokArrow
	TokPipe
	TokAmpersand
	TokOrOr
	TokLParen
	TokRParen
	TokLBrace
	TokRBrace
	TokColon
	TokEquals
	TokComma
	TokQuestion
	TokHash
	TokTilde
	TokAtBrace
	TokAtPrompt
)

var tokenNames = [20]string{
	TokEOF:       "eof",
	TokIdent:     "ident",
	TokString:    "string",
	TokNumber:    "number",
	TokArrow:     "->",
	TokPipe:      "|",
	TokAmpersand: "&",
	TokOrOr:      "||",
	TokLParen:    "(",
	TokRParen:    ")",
	TokLBrace:    "{",
	TokRBrace:    "}",
	TokColon:     ":",
	TokEquals:    "=",
	TokComma:     ",",
	TokQuestion:  "?",
	TokHash:      "#",
	TokTilde:     "~",
	TokAtBrace:   "@{",
	TokAtPrompt:  "@",
}

func (t TokenType) String() string {
	if int(t) < len(tokenNames) {
		return tokenNames[t]
	}
	return "?"
}

type Token struct {
	Type   TokenType
	Lit    string
	Line   int
	Offset int
}
