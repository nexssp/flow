package dslparse

import (
	"strings"
	"time"
)

func scanTopLevelArrows(line string) []int {
	var positions []int
	depth := 0
	inQuotes := false
	var quoteCh byte

	for i := 0; i < len(line)-1; i++ {
		ch := line[i]
		if inQuotes {
			if ch == quoteCh && (i == 0 || line[i-1] != '\\') {
				inQuotes = false
			}
			continue
		}
		switch ch {
		case '"', '\'', '`':
			inQuotes = true
			quoteCh = ch
		case '{':
			depth++
		case '}':
			depth--
		case '-':
			if depth == 0 && line[i+1] == '>' {
				positions = append(positions, i)
			}
		}
	}
	return positions
}

func extractTargetAtomFromLine(line string) (string, bool) {
	arrows := scanTopLevelArrows(line)
	if len(arrows) == 0 {
		return strings.TrimSpace(line), false
	}
	last := arrows[len(arrows)-1]
	return strings.TrimSpace(line[last+2:]), true
}

func splitModifiers(modsStr string) []string {
	var mods []string
	var sb strings.Builder
	inQuotes := false

	for i := 0; i < len(modsStr); i++ {
		ch := modsStr[i]
		if ch == '"' {
			inQuotes = !inQuotes
			sb.WriteByte(ch)
		} else if ch == ':' && !inQuotes {
			if sb.Len() > 0 {
				mods = append(mods, sb.String())
				sb.Reset()
			}
		} else {
			sb.WriteByte(ch)
		}
	}
	if sb.Len() > 0 {
		mods = append(mods, sb.String())
	}
	return mods
}

func buildDSLTransportBinding(key, value string) *TransportBinding {
	v := trimValue(value)
	if v == "" {
		return nil
	}
	switch key {
	case "sse":
		return &TransportBinding{
			Kind: "sse", Protocol: "thttp",
			Target: "SSE " + v, Method: "SSE", Path: v, Stream: true,
		}
	case "raw":
		method, path := parseRouteValue(v, "GET")
		if method == "" || path == "" {
			return nil
		}
		return &TransportBinding{
			Kind: "raw", Protocol: "thttp",
			Target: method + " " + path, Method: method, Path: path, Raw: true,
		}
	case "cli":
		return &TransportBinding{Kind: "cli", Protocol: "tcli", Target: v}
	case "cron":
		lower := strings.ToLower(v)
		if strings.HasPrefix(lower, "every ") {
			d, err := time.ParseDuration(strings.TrimSpace(v[len("every "):]))
			if err != nil {
				return nil
			}
			return &TransportBinding{
				Kind: "cron", Protocol: "cron",
				Target: "every " + d.String(), Interval: d,
			}
		}
		return &TransportBinding{
			Kind: "cron", Protocol: "cron",
			Target: "cron " + v, Schedule: v,
		}
	case "worker":
		raw := strings.TrimPrefix(strings.ToLower(v), "every ")
		d, err := time.ParseDuration(raw)
		if err != nil {
			return nil
		}
		return &TransportBinding{
			Kind: "worker", Protocol: "tworker",
			Target: "every " + d.String(), Interval: d,
		}
	case "topic":
		return &TransportBinding{Kind: "topic", Protocol: "tbus", Target: v, Subject: v}
	case "nats":
		return &TransportBinding{Kind: "nats-pubsub", Protocol: "tnats", Target: v, Subject: v}
	case "nats_rpc":
		return &TransportBinding{Kind: "nats-rpc", Protocol: "tnats", Target: v, Subject: v}
	case "nats_kv":
		parts := strings.SplitN(v, "/", 2)
		if len(parts) != 2 {
			return nil
		}
		return &TransportBinding{
			Kind: "nats-kv", Protocol: "tnats", Target: v,
			Subject: parts[0] + "." + parts[1],
			Meta:    map[string]string{"bucket": parts[0], "key": parts[1]},
		}
	case "nats_durable":
		parts := strings.SplitN(v, "/", 4)
		if len(parts) < 3 {
			return nil
		}
		b := &TransportBinding{
			Kind: "nats-durable", Protocol: "tnats", Target: v, Subject: parts[1],
			Meta: map[string]string{"stream": parts[0], "durable": parts[2]},
		}
		if len(parts) == 4 {
			b.Meta["dlq"] = parts[3]
		}
		return b
	case "nats_consumer":
		parts := strings.SplitN(v, "/", 3)
		if len(parts) < 3 {
			return nil
		}
		return &TransportBinding{
			Kind: "nats-consumer", Protocol: "tnats", Target: v, Subject: parts[1],
			Meta: map[string]string{"stream": parts[0], "durable": parts[2]},
		}
	case "nats_obj":
		parts := strings.SplitN(v, "/", 2)
		if len(parts) != 2 {
			return nil
		}
		return &TransportBinding{
			Kind: "nats-objectstore", Protocol: "tnats", Target: v,
			Subject: parts[0] + "." + parts[1],
			Meta:    map[string]string{"bucket": parts[0], "pattern": parts[1]},
		}
	case "nats_svc":
		parts := strings.SplitN(v, "/", 4)
		if len(parts) < 4 {
			return nil
		}
		return &TransportBinding{
			Kind: "nats-service", Protocol: "tnats",
			Target: parts[0] + "/" + parts[2], Subject: parts[3],
			Meta: map[string]string{"service": parts[0], "version": parts[1], "endpoint": parts[2]},
		}
	case "a2a":
		return &TransportBinding{Kind: "a2a", Protocol: "ta2a", Target: v, Subject: v}
	}
	return nil
}
