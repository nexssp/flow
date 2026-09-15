package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

	"github.com/nexssp/flow"
	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/runner/capability"
	"github.com/nexssp/kernel/action"
)

type FieldDoc struct {
	Name       string `json:"name"`
	Key        string `json:"key"`
	Type       string `json:"type"`
	Required   bool   `json:"required"`
	Validation string `json:"validation,omitempty"`
}

type FlowStep struct {
	Name   string
	Prompt string
}

// PrintFlowInfo inspects a .flow file and prints its pipeline
// topography, entry payload shape, and per-node action metadata.
//
// reg is the registry used to resolve each node name. Pass the same
// registry the flow will execute against so the info output matches
// what the flow would actually call. Pass nil to fall back to the
// domainless standard library.
func PrintFlowInfo(ctx context.Context, out io.Writer, req Request, reg *flow.MapRegistry) int {
	manifest, err := os.ReadFile(req.Path)
	if err != nil {
		fmt.Fprintf(out, "❌ failed to read flow file %q: %v\n", req.Path, err)

		return 1
	}

	dsl := flow.SanitizeDSL(string(manifest))
	if dsl == "" {
		fmt.Fprintf(out, "❌ no executable pipeline found in %s\n", req.Path)

		return 1
	}

	if reg == nil {
		fallback, ferr := flow.BuildRegistry(flow.StandardLibrary())
		if ferr != nil {
			fmt.Fprintf(out, "❌ failed to build fallback registry: %v\n", ferr)

			return 1
		}

		reg = fallback
	}

	resolver, err := capability.NewResolver(ctx, reg, string(manifest))
	if err == nil {
		defer resolver.Close()

		resolver.Install()
	}

	parser := compiler.NewParser(dsl)

	ast, err := parser.ParseExpression()
	if err != nil {
		fmt.Fprintf(out, "❌ failed to parse DSL: %v\n", err)

		return 1
	}

	steps := extractAtomSequence(ast)

	var atomNames []string
	for _, s := range steps {
		atomNames = append(atomNames, s.Name)
	}

	fmt.Fprintf(out, "\n📋 FLOW SCRIPT INSPECTION: %s\n", req.Path)
	fmt.Fprintln(out, strings.Repeat("═", 80))

	fmt.Fprintf(out, "⚡ Pipeline Topography:\n   %s\n\n", strings.Join(atomNames, " ➜ "))

	var entryFields []FieldDoc

	samplePayload := make(map[string]any)

	if len(steps) > 0 {
		firstAtom := steps[0].Name
		if act, found := reg.Get(firstAtom); found {
			entryFields = extractActionFields(act, true)
			if ex := act.Describe().Example; ex != nil {
				payloadFromExample(ex, samplePayload)
			} else {
				for _, f := range entryFields {
					samplePayload[f.Key] = defaultExampleValue(f.Type, f.Key)
				}
			}
		}
	}

	fmt.Fprintln(out, "📥 Expected Input (Initial Payload):")

	if len(entryFields) == 0 {
		fmt.Fprintln(out, "   (Accepts empty payload: {})")
	} else {
		for _, f := range entryFields {
			reqBadge := "optional"
			if f.Required {
				reqBadge = "REQUIRED"
			}

			valRule := ""
			if f.Validation != "" {
				valRule = fmt.Sprintf(" [%s]", f.Validation)
			}

			fmt.Fprintf(out, "   • %-16s %-12s (json: %-14s) %s%s\n",
				f.Name, f.Type, `"`+f.Key+`"`, reqBadge, valRule)
		}
	}

	fmt.Fprintln(out, "\n🧩 Action Sequence Details:")

	for i, step := range steps {
		act, found := reg.Get(step.Name)
		desc := "(custom or dynamic capability)"
		reqT, resT := "any", "any"

		if found && act != nil && act.Describe() != nil {
			if act.Describe().Description != "" {
				desc = act.Describe().Description
			}

			if typed, ok := act.(action.TypedPayload); ok {
				if rp := typed.ReqPayload(); rp != nil {
					reqT = reflect.TypeOf(rp).String()
				}

				if sp := typed.ResPayload(); sp != nil {
					resT = reflect.TypeOf(sp).String()
				}
			}
		}

		fmt.Fprintf(out, "   [%d] %-24s │ In: %-18s │ Out: %-18s\n", i+1, step.Name, reqT, resT)
		fmt.Fprintf(out, "       └── %s\n", desc)

		if step.Prompt != "" {
			fmt.Fprintf(out, "       └── Prompt: %q\n", step.Prompt)
		}
	}

	sampleJSON, _ := json.Marshal(samplePayload)

	fmt.Fprintln(out, "\n🚀 Quick Run Command (PowerShell):")

	if len(samplePayload) == 0 {
		fmt.Fprintf(out, "   go run ./cmd/jumalu flow %s\n", req.Path)
	} else {
		fmt.Fprintf(out, "   go run ./cmd/jumalu flow %s '%s'\n", req.Path, string(sampleJSON))
	}

	fmt.Fprintln(out, strings.Repeat("═", 80))

	return 0
}

func extractAtomSequence(expr compiler.Expr) []FlowStep {
	var (
		steps []FlowStep
		walk  func(compiler.Expr)
	)

	walk = func(e compiler.Expr) {
		switch n := e.(type) {
		case *compiler.PipelineExpr:
			walk(n.Left)
			walk(n.Right)
		case *compiler.ParallelExpr:
			for _, c := range n.Children {
				walk(c)
			}
		case *compiler.AtomExpr:
			steps = append(steps, FlowStep{Name: n.Name, Prompt: n.Prompt})
		case *compiler.ProjectionExpr:
			steps = append(steps, FlowStep{Name: "{ projection }"})
		case *compiler.ConditionalExpr:
			walk(n.Gate)
			walk(n.Target)
		case *compiler.FallbackExpr:
			walk(n.Left)
			walk(n.Right)
		case *compiler.LoopExpr:
			walk(n.Body)
		}
	}
	walk(expr)

	return steps
}

func extractActionFields(act action.AnyAction, isReq bool) []FieldDoc {
	typed, ok := act.(action.TypedPayload)
	if !ok {
		return nil
	}

	var val any
	if isReq {
		val = typed.ReqPayload()
	} else {
		val = typed.ResPayload()
	}

	if val == nil {
		return nil
	}

	t := reflect.TypeOf(val)
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return nil
	}

	var fields []FieldDoc

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}

		jsonKey := strings.Split(f.Tag.Get("json"), ",")[0]
		if jsonKey == "-" {
			continue
		}

		if jsonKey == "" {
			jsonKey = f.Name
		}

		valTag := f.Tag.Get("validate")
		fields = append(fields, FieldDoc{
			Name:       f.Name,
			Key:        jsonKey,
			Type:       f.Type.String(),
			Required:   strings.Contains(valTag, "required"),
			Validation: valTag,
		})
	}

	return fields
}

func defaultExampleValue(typeName, keyName string) any {
	k := strings.ToLower(keyName)
	switch {
	case strings.Contains(k, "goal"), strings.Contains(k, "prompt"):
		return "Execute autonomous verification task"
	case strings.Contains(k, "budget"):
		return 1.0
	case strings.Contains(k, "id"):
		return "sess_1001"
	case typeName == "string":
		return "sample_" + keyName
	case typeName == "int", typeName == "int64":
		return 1
	case typeName == "float64":
		return 0.5
	case typeName == "bool":
		return true
	default:
		return "value"
	}
}

func payloadFromExample(ex any, dst map[string]any) {
	if m, ok := ex.(map[string]any); ok {
		for k, v := range m {
			dst[k] = v
		}

		return
	}

	b, err := json.Marshal(ex)
	if err != nil {
		return
	}

	_ = json.Unmarshal(b, &dst)
}
