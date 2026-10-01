package schema

import "strings"

// classifyFieldType fills Pointer, Slice, Map, ElemType, and Kind from
// the raw Type string.
func classifyFieldType(field *Field) {
	t := strings.TrimSpace(field.Type)
	if strings.HasPrefix(t, "*") {
		field.Pointer = true
		t = strings.TrimSpace(t[1:])
	}
	if strings.HasPrefix(t, "[]") {
		field.Slice = true
		field.ElemType = strings.TrimSpace(t[2:])
		field.Kind = classifyScalar(field.ElemType)
		return
	}
	if strings.HasPrefix(t, "map[") {
		field.Map = true
		if end := strings.IndexByte(t, ']'); end > 0 && end < len(t)-1 {
			field.ElemType = strings.TrimSpace(t[end+1:])
			field.Kind = classifyScalar(field.ElemType)
		} else {
			field.Kind = KindAny
		}
		return
	}
	field.Kind = classifyScalar(t)
	if field.Kind == KindStruct {
		field.ElemType = t
	}
}

func classifyScalar(t string) Kind {
	switch t {
	case "string":
		return KindString
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64":
		return KindInt
	case "float32", "float64":
		return KindFloat
	case "bool":
		return KindBool
	case "any", "interface{}":
		return KindAny
	default:
		return KindStruct
	}
}
