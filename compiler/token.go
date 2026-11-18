package compiler

const (
	stringKindName = "string"
	literalFalse   = "false"
)

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
	TokLBracket
	TokRBracket
	TokColon
	TokEquals
	TokComma
	TokQuestion
	TokHash
	TokTilde
	TokAtBrace
	TokAtPrompt
	TokInvalid
)

var tokenNames = [23]string{
	TokEOF:       "eof",
	TokIdent:     "ident",
	TokString:    stringKindName,
	TokNumber:    "number",
	TokArrow:     "->",
	TokPipe:      "|",
	TokAmpersand: "&",
	TokOrOr:      "||",
	TokLParen:    "(",
	TokRParen:    ")",
	TokLBrace:    "{",
	TokRBrace:    "}",
	TokLBracket:  "[",
	TokRBracket:  "]",
	TokColon:     ":",
	TokEquals:    "=",
	TokComma:     ",",
	TokQuestion:  "?",
	TokHash:      "#",
	TokTilde:     "~",
	TokAtBrace:   "@{",
	TokAtPrompt:  "@",
	TokInvalid:   "invalid",
}

func (t TokenType) String() string {
	if int(t) < len(tokenNames) {
		return tokenNames[t]
	}
	return "?"
}

type Token struct {
	Type         TokenType
	Lit          string
	Line         int
	Offset       int
	EndOffset    int
	Unterminated bool
}
