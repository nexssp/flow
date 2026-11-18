package pipeline

import (
	"reflect"
	"strconv"
	"strings"
	"time"

	compiler "github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/compiler/secret"
	schemaext "github.com/nexssp/flow/extensions/schema"
)

func pipelineAnalysisInput(contract *schemaBoundaryContract) compiler.TypeDescriptor {
	if contract == nil {
		return compiler.TypeDescriptor{}
	}
	if contract.array {
		var itemType reflect.Type
		if contract.schema != nil {
			itemType = schemaAnalysisType(*contract.schema, make(map[string]bool))
		} else {
			itemType = schemaNamedAnalysisType(contract.itemType)
		}
		return compiler.TypeDescriptor{Type: reflect.SliceOf(itemType)}
	}
	if contract.schema == nil {
		return pipelineSecretDescriptor(contract)
	}
	return compiler.TypeDescriptor{Type: schemaAnalysisType(*contract.schema, make(map[string]bool))}
}

func pipelineSecretDescriptor(contract *schemaBoundaryContract) compiler.TypeDescriptor {
	if contract == nil {
		return compiler.TypeDescriptor{}
	}
	var payloadType reflect.Type
	switch {
	case contract.array:
		var itemType reflect.Type
		if contract.schema != nil {
			itemType = schemaAnalysisType(*contract.schema, make(map[string]bool))
		} else {
			itemType = schemaNamedAnalysisType(contract.itemType)
		}
		payloadType = reflect.SliceOf(itemType)
	case contract.schema != nil:
		payloadType = schemaAnalysisType(*contract.schema, make(map[string]bool))
	case contract.itemType != "":
		payloadType = schemaNamedAnalysisType(contract.itemType)
	}
	if !secret.TypeContains(payloadType) {
		return compiler.TypeDescriptor{}
	}
	return compiler.TypeDescriptor{Type: payloadType, ContainsSecret: true}
}

func schemaAnalysisType(schema schemaext.Schema, visiting map[string]bool) reflect.Type {
	if visiting[schema.Name] {
		return reflect.TypeFor[any]()
	}
	visiting[schema.Name] = true
	defer delete(visiting, schema.Name)

	fields := make([]reflect.StructField, 0, len(schema.Fields))
	for index := range schema.Fields {
		field := &schema.Fields[index]
		if field.JSONName == "-" {
			continue
		}
		jsonName := field.JSONName
		if jsonName == "" && field.Name != "" {
			jsonName = strings.ToLower(field.Name[:1]) + field.Name[1:]
		}
		if jsonName == "" {
			continue
		}
		fields = append(fields, reflect.StructField{
			Name: fmtAnalysisFieldName(len(fields)),
			Type: schemaFieldAnalysisType(*field, visiting),
			Tag:  reflect.StructTag("json:" + strconv.Quote(jsonName) + " flow_input_schema:\"true\""),
		})
	}
	if len(fields) == 0 {
		return reflect.TypeFor[struct{}]()
	}
	return reflect.StructOf(fields)
}

func schemaFieldAnalysisType(field schemaext.Field, visiting map[string]bool) reflect.Type {
	var fieldType reflect.Type
	switch {
	case field.Map:
		keyType := schemaMapKeyAnalysisType(field.Type)
		elementType := schemaElementAnalysisType(field, visiting)
		fieldType = reflect.MapOf(keyType, elementType)
	case field.Slice:
		fieldType = reflect.SliceOf(schemaElementAnalysisType(field, visiting))
	default:
		fieldType = schemaElementAnalysisType(field, visiting)
	}
	if field.Pointer {
		fieldType = reflect.PointerTo(fieldType)
	}
	return fieldType
}

func schemaElementAnalysisType(field schemaext.Field, visiting map[string]bool) reflect.Type {
	if field.NestedSchema != nil {
		return schemaAnalysisType(*field.NestedSchema, visiting)
	}
	if field.Slice || field.Map {
		return schemaNamedAnalysisType(field.ElemType)
	}
	typeName := strings.TrimSpace(strings.TrimPrefix(field.Type, "*"))
	return schemaNamedAnalysisType(typeName)
}

func schemaMapKeyAnalysisType(typeName string) reflect.Type {
	typeName = strings.TrimPrefix(strings.TrimSpace(typeName), "*")
	if !strings.HasPrefix(typeName, "map[") {
		return reflect.TypeFor[string]()
	}
	keyName, _, ok := strings.Cut(strings.TrimPrefix(typeName, "map["), "]")
	if !ok {
		return reflect.TypeFor[string]()
	}
	keyType := schemaNamedAnalysisType(strings.TrimSpace(keyName))
	if keyType == reflect.TypeFor[any]() || !keyType.Comparable() {
		return reflect.TypeFor[string]()
	}
	return keyType
}

func schemaNamedAnalysisType(typeName string) reflect.Type {
	switch strings.TrimSpace(typeName) {
	case "string":
		return reflect.TypeFor[string]()
	case "bool":
		return reflect.TypeFor[bool]()
	case "int":
		return reflect.TypeFor[int]()
	case "int8":
		return reflect.TypeFor[int8]()
	case "int16":
		return reflect.TypeFor[int16]()
	case "int32":
		return reflect.TypeFor[int32]()
	case "int64":
		return reflect.TypeFor[int64]()
	case "uint":
		return reflect.TypeFor[uint]()
	case "uint8":
		return reflect.TypeFor[uint8]()
	case "uint16":
		return reflect.TypeFor[uint16]()
	case "uint32":
		return reflect.TypeFor[uint32]()
	case "uint64":
		return reflect.TypeFor[uint64]()
	case "float32":
		return reflect.TypeFor[float32]()
	case "float64":
		return reflect.TypeFor[float64]()
	case "duration", "time.Duration":
		return reflect.TypeFor[time.Duration]()
	case "secret":
		return reflect.TypeFor[secret.Value]()
	case "any", "interface{}":
		return reflect.TypeFor[any]()
	default:
		return reflect.TypeFor[any]()
	}
}

func fmtAnalysisFieldName(index int) string {
	return "Field" + strconv.Itoa(index)
}
