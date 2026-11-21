package lexical

import "strings"

// IsHashLineComment distinguishes a line-leading expression comment from an
// iterator placeholder followed by a selector or operator.
func IsHashLineComment(source string, index int) bool {
	if index < 0 || index >= len(source) || source[index] != '#' {
		return false
	}
	lineStart := strings.LastIndexAny(source[:index], "\r\n") + 1
	for _, character := range source[lineStart:index] {
		if character != ' ' && character != '\t' {
			return false
		}
	}
	return !HashStartsIteratorExpression(source, index)
}

// HashStartsIteratorExpression checks for the expression forms supported by
// Flow's projection hash placeholder without parsing or allocating.
func HashStartsIteratorExpression(source string, index int) bool {
	if index < 0 || index >= len(source) || source[index] != '#' {
		return false
	}
	return hashStartsIteratorExpressionAt(source, index+1)
}

func hashStartsIteratorExpressionAt(source string, next int) bool {
	if next >= len(source) {
		return false
	}
	if source[next] == '.' || source[next] == '[' {
		return true
	}
	if source[next] == '?' {
		return hashStartsOptionalOrTernary(source, next+1)
	}
	for next < len(source) && (source[next] == ' ' || source[next] == '\t') {
		next++
	}
	if next >= len(source) || source[next] == '\n' || source[next] == '\r' {
		return false
	}
	return hashStartsOperator(source, next)
}

func hashStartsOperator(source string, index int) bool {
	switch source[index] {
	case '!', '=':
		return index+1 < len(source) && source[index+1] == '='
	case '<', '>', '+', '-', '*', '/', '%', '^', '|':
		return true
	case '&':
		return index+1 < len(source) && source[index+1] == '&'
	case '?':
		return hashStartsCoalesceOrTernary(source, index+1)
	default:
		return hashStartsWordOperator(source, index)
	}
}

func hashStartsOptionalOrTernary(source string, index int) bool {
	if index < len(source) && (source[index] == '.' || source[index] == '[') {
		return true
	}
	if index < len(source) && source[index] == '?' {
		return true
	}
	return hashHasTernarySeparator(source, index)
}

func hashStartsCoalesceOrTernary(source string, index int) bool {
	if index < len(source) && source[index] == '?' {
		return true
	}
	return hashHasTernarySeparator(source, index)
}

func hashHasTernarySeparator(source string, index int) bool {
	var quote byte
	depth := 0
	for ; index < len(source) && source[index] != '\n' && source[index] != '\r'; index++ {
		character := source[index]
		if quote != 0 {
			if character == '\\' && quote != '`' && index+1 < len(source) {
				index++
				continue
			}
			if character == quote {
				quote = 0
			}
			continue
		}
		switch character {
		case '"', '\'', '`':
			quote = character
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case ':':
			if depth == 0 {
				return true
			}
		}
	}
	return false
}

func hashStartsWordOperator(source string, index int) bool {
	var operator string
	switch source[index] {
	case 'a':
		operator = "and"
	case 'c':
		operator = "contains"
	case 'i':
		operator = "in"
	case 'm':
		operator = "matches"
	case 'o':
		operator = "or"
	case 'e':
		operator = "endsWith"
	case 's':
		operator = "startsWith"
	case 'n':
		if !hashWordAt(source, index, "not") {
			return false
		}
		next := index + len("not")
		if next >= len(source) || source[next] != ' ' && source[next] != '\t' {
			return false
		}
		for next < len(source) && (source[next] == ' ' || source[next] == '\t') {
			next++
		}
		return hashWordAt(source, next, "in")
	default:
		return false
	}
	return hashWordAt(source, index, operator)
}

func hashWordAt(source string, index int, word string) bool {
	if index < 0 || index+len(word) > len(source) || !strings.HasPrefix(source[index:], word) {
		return false
	}
	end := index + len(word)
	return end == len(source) || !hashIdentifierContinue(source[end])
}

func hashIdentifierContinue(character byte) bool {
	return character == '_' || character >= 'a' && character <= 'z' ||
		character >= 'A' && character <= 'Z' || character >= '0' && character <= '9'
}
