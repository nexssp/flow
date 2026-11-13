package runner

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler"
)

const (
	diagnosticANSIRed    = "\x1b[31m"
	diagnosticANSIYellow = "\x1b[33m"
	diagnosticANSIReset  = "\x1b[0m"
)

// Diagnostic carries stable, user-facing error metadata while the original
// cause remains available through errors.Is and errors.As.
type Diagnostic struct {
	Code     string
	Message  string
	Stage    string
	Position compiler.Position
	Hint     string
	Frames   []CallFrame
}

// CallFrame identifies a Flow action in the failed call path.
type CallFrame struct {
	Action   string
	Phase    string
	Position compiler.Position
}

type flowDiagnosticCarrier interface {
	FlowDiagnostic() Diagnostic
}

// DiagnosticHintProvider lets a host attach domain-specific advice without
// teaching Flow about that host's selector or command syntax.
type DiagnosticHintProvider interface {
	FlowDiagnosticHint() string
}

// DiagnosticStageProvider lets a host identify the stage of its own boundary.
type DiagnosticStageProvider interface {
	FlowDiagnosticStage() string
}

// DiagnosticError adds structured metadata without replacing or mutating the
// wrapped error. Its Error string remains the original error string for callers
// that need legacy detail; user-facing Flow output should use FormatErrorDetail.
type DiagnosticError struct {
	detail Diagnostic
	cause  error
}

func (e *DiagnosticError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.cause != nil {
		return e.cause.Error()
	}
	return formatDiagnosticHeadline(e.detail)
}

func (e *DiagnosticError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// FlowDiagnostic returns a defensive copy of the structured diagnostic.
func (e *DiagnosticError) FlowDiagnostic() Diagnostic {
	if e == nil {
		return Diagnostic{}
	}
	return cloneDiagnostic(e.detail)
}

func (e *DiagnosticError) FlowDiagnosticHint() string {
	if e == nil {
		return ""
	}
	return e.detail.Hint
}

func (e *DiagnosticError) FlowDiagnosticStage() string {
	if e == nil {
		return ""
	}
	return e.detail.Stage
}

// DiagnosticOf extracts Flow diagnostic metadata from an error chain.
func DiagnosticOf(err error) (Diagnostic, bool) {
	if err == nil {
		return Diagnostic{}, false
	}
	var carrier flowDiagnosticCarrier
	hasCarrier := errors.As(err, &carrier) && carrier != nil
	var diagnostic Diagnostic
	if hasCarrier {
		diagnostic = cloneDiagnostic(carrier.FlowDiagnostic())
	} else {
		var hintProvider DiagnosticHintProvider
		var stageProvider DiagnosticStageProvider
		hasHint := errors.As(err, &hintProvider) && hintProvider != nil
		hasStage := errors.As(err, &stageProvider) && stageProvider != nil
		if !hasHint && !hasStage {
			return Diagnostic{}, false
		}
		diagnostic = diagnosticFromError(err, "")
		diagnostic.Frames = collectActionFrames(err)
	}
	var hintProvider DiagnosticHintProvider
	if errors.As(err, &hintProvider) && hintProvider != nil && diagnostic.Hint == "" {
		diagnostic.Hint = strings.TrimSpace(hintProvider.FlowDiagnosticHint())
	}
	var stageProvider DiagnosticStageProvider
	if errors.As(err, &stageProvider) && stageProvider != nil && diagnostic.Stage == "" {
		diagnostic.Stage = strings.TrimSpace(stageProvider.FlowDiagnosticStage())
	}
	if !hasCarrier && diagnostic.Hint == "" && diagnostic.Stage == "" {
		return Diagnostic{}, false
	}
	return diagnostic, true
}

// WithDiagnosticHint adds one actionable hint while preserving the original
// error chain and any diagnostic fields already attached to it.
func WithDiagnosticHint(err error, hint string) error {
	if err == nil || strings.TrimSpace(hint) == "" {
		return err
	}
	diagnostic, ok := DiagnosticOf(err)
	if !ok {
		diagnostic = diagnosticFromError(err, "")
		diagnostic.Frames = collectActionFrames(err)
	}
	if diagnostic.Hint == "" {
		diagnostic.Hint = strings.TrimSpace(hint)
	}
	return &DiagnosticError{detail: diagnostic, cause: err}
}

func newDiagnosticError(err error, stage string) *DiagnosticError {
	if err == nil {
		return nil
	}
	diagnostic, ok := DiagnosticOf(err)
	if !ok {
		diagnostic = diagnosticFromError(err, stage)
		diagnostic.Frames = collectActionFrames(err)
	} else {
		if diagnostic.Stage == "" {
			diagnostic.Stage = stage
		}
		diagnostic.Frames = deduplicateFrames(append(collectOuterActionFrames(err), diagnostic.Frames...))
	}
	return &DiagnosticError{detail: diagnostic, cause: err}
}

func diagnosticFromError(err error, stage string) Diagnostic {
	diagnostic := Diagnostic{
		Code:  "FLOW_ERROR",
		Stage: stage,
	}
	if position, ok := compiler.PositionOf(err); ok {
		diagnostic.Position = position
	}
	for current := err; current != nil; current = unwrapOne(current) {
		var appErr *xerr.AppError
		if !errors.As(current, &appErr) || appErr == nil {
			continue
		}
		diagnostic.Code = string(appErr.Kind)
		if diagnostic.Message == "" {
			diagnostic.Message = appErr.Message
		}
	}
	if diagnostic.Message == "" {
		if root := rootError(err); root != nil {
			diagnostic.Message = root.Error()
		}
	}
	return diagnostic
}

func rootError(err error) error {
	for err != nil {
		next := unwrapOne(err)
		if next == nil {
			return err
		}
		err = next
	}
	return nil
}

func unwrapOne(err error) error {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) > 0 {
			return causes[0]
		}
		return nil
	}
	return errors.Unwrap(err)
}

func collectActionFrames(err error) []CallFrame {
	var frames []CallFrame
	var visit func(error)
	visit = func(current error) {
		if current == nil {
			return
		}
		if joined, ok := current.(interface{ Unwrap() []error }); ok {
			for _, cause := range joined.Unwrap() {
				if frame, ok := actionFrame(current, cause); ok {
					frames = append(frames, frame)
				}
				visit(cause)
			}
			return
		}
		next := errors.Unwrap(current)
		if next == nil {
			return
		}
		if frame, ok := actionFrame(current, next); ok {
			frames = append(frames, frame)
		}
		visit(next)
	}
	visit(err)
	return deduplicateFrames(frames)
}

func collectOuterActionFrames(err error) []CallFrame {
	var frames []CallFrame
	var visit func(error)
	visit = func(current error) {
		if current == nil {
			return
		}
		var diagnosticErr *DiagnosticError
		if errors.As(current, &diagnosticErr) && diagnosticErr != nil {
			return
		}
		if joined, ok := current.(interface{ Unwrap() []error }); ok {
			for _, cause := range joined.Unwrap() {
				if frame, ok := actionFrame(current, cause); ok {
					frames = append(frames, frame)
				}
				visit(cause)
			}
			return
		}
		next := errors.Unwrap(current)
		if next == nil {
			return
		}
		if frame, ok := actionFrame(current, next); ok {
			frames = append(frames, frame)
		}
		visit(next)
	}
	visit(err)
	return deduplicateFrames(frames)
}

func actionFrame(outer, inner error) (CallFrame, bool) {
	prefix, ok := strings.CutSuffix(outer.Error(), inner.Error())
	if !ok {
		return CallFrame{}, false
	}
	prefix = strings.TrimSpace(prefix)
	if !strings.HasPrefix(prefix, "action ") {
		return CallFrame{}, false
	}
	for _, phase := range []string{"execution failed:", "before-hook failed:", "configuration:"} {
		if !strings.HasSuffix(prefix, phase) {
			continue
		}
		name := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(prefix, "action "), phase))
		if name == "" {
			return CallFrame{}, false
		}
		frame := CallFrame{Action: name, Phase: strings.TrimSuffix(phase, ":")}
		if position, ok := compiler.PositionOf(outer); ok {
			frame.Position = position
		}
		return frame, true
	}
	return CallFrame{}, false
}

func deduplicateFrames(frames []CallFrame) []CallFrame {
	if len(frames) < 2 {
		return frames
	}
	out := make([]CallFrame, 0, len(frames))
	seen := make(map[string]struct{}, len(frames))
	for _, frame := range frames {
		key := frame.Action + "\x00" + frame.Phase
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, frame)
	}
	return out
}

func cloneDiagnostic(diagnostic Diagnostic) Diagnostic {
	diagnostic.Frames = append([]CallFrame(nil), diagnostic.Frames...)
	return diagnostic
}

// FormatErrorDetail returns a concise root-cause diagnostic, including stage,
// the best available source location/snippet, and one actionable hint.
func FormatErrorDetail(err error, source, name string, useColor bool) string {
	if err == nil {
		return ""
	}
	diagnostic, ok := DiagnosticOf(err)
	if !ok {
		diagnostic = diagnosticFromError(err, "")
	}

	var builder strings.Builder
	builder.WriteString(formatDiagnosticHeadline(diagnostic))
	if diagnostic.Position.File != "" {
		fmt.Fprintf(&builder, "\n  at %s", diagnostic.Position.String())
		if text, ok := diagnosticSource(diagnostic.Position, source, name); ok {
			if snippet := renderDiagnosticSnippet(diagnostic.Position, text, useColor); snippet != "" {
				builder.WriteString(snippet)
			}
		}
	}
	if diagnostic.Hint != "" {
		fmt.Fprintf(&builder, "\n  hint: %s", diagnostic.Hint)
	}
	return builder.String()
}

func formatDiagnosticHeadline(diagnostic Diagnostic) string {
	message := diagnostic.Message
	if message == "" {
		message = "unknown Flow error"
	}
	code := diagnostic.Code
	if code == "" {
		code = "FLOW_ERROR"
	}
	headline := code + ": " + message
	if diagnostic.Stage != "" {
		headline += " (stage: " + diagnostic.Stage + ")"
	}
	return headline
}

func formatErrorSummary(err error) string {
	if diagnostic, ok := DiagnosticOf(err); ok {
		return formatDiagnosticHeadline(diagnostic)
	}
	return formatDiagnosticHeadline(diagnosticFromError(err, ""))
}

// FormatErrorTrace returns detailed, payload-free call-frame information for
// verbose output. The original causes remain reachable through the error chain.
func FormatErrorTrace(err error) string {
	diagnostic, ok := DiagnosticOf(err)
	if !ok {
		return ""
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "  debug: code=%s stage=%s\n", diagnostic.Code, diagnostic.Stage)
	if len(diagnostic.Frames) == 0 {
		return strings.TrimSuffix(builder.String(), "\n")
	}
	builder.WriteString("  call frames:\n")
	for i, frame := range diagnostic.Frames {
		fmt.Fprintf(&builder, "    %d. %s", i+1, frame.Action)
		if frame.Phase != "" {
			fmt.Fprintf(&builder, " (%s)", frame.Phase)
		}
		if frame.Position.File != "" {
			fmt.Fprintf(&builder, " at %s", frame.Position.String())
		}
		builder.WriteByte('\n')
	}
	return strings.TrimSuffix(builder.String(), "\n")
}

func diagnosticSource(position compiler.Position, source, name string) (string, bool) {
	if position.File == "" {
		return "", false
	}
	if position.File == name {
		return source, source != ""
	}
	if strings.HasPrefix(position.File, "<") {
		return "", false
	}
	data, err := os.ReadFile(position.File)
	if err != nil {
		return "", false
	}
	return string(data), true
}

func renderDiagnosticSnippet(position compiler.Position, source string, useColor bool) string {
	lines := strings.Split(source, "\n")
	if position.Line < 1 || position.Line > len(lines) {
		return ""
	}

	target := lines[position.Line-1]
	var builder strings.Builder
	builder.WriteString("\n")

	if position.Line > 1 {
		fmt.Fprintf(&builder, "  %4d │ %s\n", position.Line-1, lines[position.Line-2])
	}
	fmt.Fprintf(&builder, "  %4d │ %s\n", position.Line, target)
	builder.WriteString("       │ ")

	column := position.Col
	if column < 1 {
		column = diagnosticLeadingWhitespace(target) + 1
	}
	for i := 0; i < column-1 && i < len(target); i++ {
		if target[i] == '\t' {
			builder.WriteByte('\t')
		} else {
			builder.WriteByte(' ')
		}
	}
	builder.WriteString(diagnosticPaint("^", diagnosticANSIYellow, useColor))

	if position.Line < len(lines) && strings.TrimSpace(lines[position.Line]) != "" {
		fmt.Fprintf(&builder, "\n  %4d │ %s", position.Line+1, lines[position.Line])
	}
	return builder.String()
}

func diagnosticLeadingWhitespace(line string) int {
	for i := range len(line) {
		if line[i] != ' ' && line[i] != '\t' {
			return i
		}
	}
	return 0
}

func diagnosticPaint(text, color string, enabled bool) string {
	if !enabled {
		return text
	}
	return color + text + diagnosticANSIReset
}
