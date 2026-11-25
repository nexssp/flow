package events

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/nexssp/flow/compiler"
)

const routesMetadataKey = "flow.event.routes"

// Route is one explicit, single-destination NFlow event route.
type Route struct {
	EventName    string
	SinkName     string
	Position     compiler.Position
	SinkPosition compiler.Position
}

// Directive parses newline-separated routes to direct typed event sinks or
// declared NFlow pipeline handlers.
var Directive = compiler.Directive{
	Name:        "events",
	Example:     "@events {\n  observe thttp.events.listener_ready -> pipeline.report_listener_ready\n}",
	Handler:     handleDirective,
	OpaqueBlock: true,
}

func handleDirective(_ context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
	block, err := compiler.ReadBlockWithSpans(req.Lines, req.I)
	if err != nil {
		pos := compiler.Position{File: req.File, Line: req.I + 1, Col: 1}
		return compiler.DirectiveRes{Next: req.I + 1}, compiler.SourceError(pos, "@events: %v", err)
	}

	source := strings.Join(req.Lines, "\n")
	if block.BodySpan.Start < 0 || block.BodySpan.End < block.BodySpan.Start || block.BodySpan.End > len(source) {
		pos := compiler.Position{File: req.File, Line: req.I + 1, Col: 1}
		return compiler.DirectiveRes{Next: block.Next}, compiler.SourceError(pos, "@events: invalid source span")
	}

	routes, err := parseRoutes(source[block.BodySpan.Start:block.BodySpan.End], block.BodySpan.Start, req)
	if err != nil {
		return compiler.DirectiveRes{Next: block.Next}, err
	}
	all := RoutesFromMeta(req.Out)
	seen := make(map[string]struct{}, len(all)+len(routes))
	for _, route := range all {
		seen[route.EventName] = struct{}{}
	}
	for _, route := range routes {
		if _, duplicate := seen[route.EventName]; duplicate {
			return compiler.DirectiveRes{Next: block.Next}, compiler.SourceError(route.Position,
				"@events: event %q already has a route; multiple destinations are not supported", route.EventName)
		}
		seen[route.EventName] = struct{}{}
	}
	if len(routes) > 0 {
		req.Out[routesMetadataKey] = append(all, routes...)
	}

	blankLines := make([]int, 0, block.Next-req.I)
	for line := req.I; line < block.Next; line++ {
		blankLines = append(blankLines, line)
	}
	return compiler.DirectiveRes{Next: block.Next, BlankLines: blankLines}, nil
}

// RoutesFromMeta returns the immutable route declarations parsed from @events.
func RoutesFromMeta(meta map[string]any) []Route {
	routes, _ := meta[routesMetadataKey].([]Route)
	return append([]Route(nil), routes...)
}

type routeSegment struct {
	start int
	end   int
}

func parseRoutes(source string, sourceOffset int, req compiler.DirectiveReq) ([]Route, error) {
	segments, err := splitRouteSegments(source)
	if err != nil {
		return nil, compiler.SourceError(req.PositionAt(sourceOffset), "@events: %v", err)
	}
	var routes []Route
	for _, segment := range segments {
		raw := source[segment.start:segment.end]
		leading := len(raw) - len(strings.TrimLeft(raw, " \t\r\n"))
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.HasPrefix(raw, "#") || strings.HasPrefix(raw, "//") {
			continue
		}
		routeOffset := sourceOffset + segment.start + leading
		route, sinkOffset, parseErr := parseRoute(raw)
		if parseErr != nil {
			return nil, compiler.SourceError(req.PositionAt(routeOffset), "@events: %v", parseErr)
		}
		route.Position = req.PositionAt(routeOffset)
		route.SinkPosition = req.PositionAt(routeOffset + sinkOffset)
		routes = append(routes, route)
	}
	return routes, nil
}

func parseRoute(source string) (Route, int, error) {
	arrow := strings.Index(source, "->")
	if arrow < 0 {
		return Route{}, 0, errors.New("expected `observe <event> -> <typed-sink-or-pipeline>`")
	}
	header := strings.Fields(strings.TrimSpace(source[:arrow]))
	if len(header) != 2 || header[0] != "observe" {
		return Route{}, 0, errors.New("expected `observe <event> -> <typed-sink-or-pipeline>`")
	}
	if !validEventSymbol(header[1]) {
		return Route{}, 0, fmt.Errorf("invalid event source symbol %q", header[1])
	}
	tail := source[arrow+2:]
	leading := len(tail) - len(strings.TrimLeft(tail, " \t\r\n"))
	sinkName := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(tail), ","))
	if sinkName == "" {
		return Route{}, 0, errors.New("event route destination is required")
	}
	if !validEventSymbol(sinkName) {
		return Route{}, 0, errors.New("event route destination must be a direct action or pipeline symbol; expressions and arguments are not supported")
	}
	return Route{EventName: header[1], SinkName: sinkName}, arrow + 2 + leading, nil
}

func validEventSymbol(name string) bool {
	if name == "" {
		return false
	}
	for part := range strings.SplitSeq(name, ".") {
		if part == "" || !eventIdentifierStart(part[0]) {
			return false
		}
		for index := 1; index < len(part); index++ {
			if !eventIdentifierContinue(part[index]) {
				return false
			}
		}
	}
	return true
}

func eventIdentifierStart(char byte) bool {
	return char == '_' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z'
}

func eventIdentifierContinue(char byte) bool {
	return eventIdentifierStart(char) || char >= '0' && char <= '9'
}

// splitRouteSegments separates only at top-level newlines or commas, leaving
// nested calls, collections, quoted strings, and their internal newlines intact.
//
//nolint:gocyclo // Quote, comment, and delimiter state must stay aligned in one source pass.
func splitRouteSegments(source string) ([]routeSegment, error) {
	var segments []routeSegment
	start := 0
	parenDepth, braceDepth, bracketDepth := 0, 0, 0
	var quote byte
	for index := 0; index < len(source); index++ {
		char := source[index]
		if quote != 0 {
			if quote != '`' && char == '\\' && index+1 < len(source) {
				index++
				continue
			}
			if char == quote {
				quote = 0
			}
			continue
		}
		if char == '"' || char == '\'' || char == '`' {
			quote = char
			continue
		}
		if char == '#' && (index == 0 || source[index-1] == ' ' || source[index-1] == '\t' || source[index-1] == '\r' || source[index-1] == '\n') ||
			char == '/' && index+1 < len(source) && source[index+1] == '/' {
			commentStart := index
			for index < len(source) && source[index] != '\n' {
				index++
			}
			if index == len(source) {
				if parenDepth == 0 && braceDepth == 0 && bracketDepth == 0 {
					segments = append(segments, routeSegment{start: start, end: commentStart})
					start = len(source)
				}
				break
			}
			if parenDepth == 0 && braceDepth == 0 && bracketDepth == 0 {
				segments = append(segments, routeSegment{start: start, end: commentStart})
				start = index + 1
			}
			continue
		}
		switch char {
		case '(':
			parenDepth++
		case ')':
			parenDepth--
			if parenDepth < 0 {
				return nil, errors.New("unexpected ')' in event route block")
			}
		case '{':
			braceDepth++
		case '}':
			braceDepth--
			if braceDepth < 0 {
				return nil, errors.New("unexpected '}' in event route block")
			}
		case '[':
			bracketDepth++
		case ']':
			bracketDepth--
			if bracketDepth < 0 {
				return nil, errors.New("unexpected ']' in event route block")
			}
		case ',':
			if parenDepth == 0 && braceDepth == 0 && bracketDepth == 0 {
				segments = append(segments, routeSegment{start: start, end: index})
				start = index + 1
			}
		case '\n':
			if parenDepth == 0 && braceDepth == 0 && bracketDepth == 0 {
				segments = append(segments, routeSegment{start: start, end: index})
				start = index + 1
			}
		}
	}
	if quote != 0 {
		return nil, errors.New("unterminated quoted string in event route block")
	}
	if parenDepth != 0 || braceDepth != 0 || bracketDepth != 0 {
		return nil, errors.New("unclosed delimiter in event route block")
	}
	segments = append(segments, routeSegment{start: start, end: len(source)})
	return segments, nil
}
