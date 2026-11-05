package identifier

import "strings"

// IsStartByte reports whether b is an ASCII identifier-start byte.
func IsStartByte(b byte) bool {
	return b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// IsContinueByte reports whether b is an ASCII identifier-continuation byte.
func IsContinueByte(b byte) bool {
	return IsStartByte(b) || b >= '0' && b <= '9'
}

// IsValid reports whether name is a non-empty ASCII identifier.
func IsValid(name string) bool {
	if name == "" || !IsStartByte(name[0]) {
		return false
	}
	for i := 1; i < len(name); i++ {
		if !IsContinueByte(name[i]) {
			return false
		}
	}
	return true
}

// IsQualified reports whether name contains at least two dot-separated valid identifiers.
func IsQualified(name string) bool {
	parts := strings.Split(name, ".")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if !IsValid(part) {
			return false
		}
	}
	return true
}
