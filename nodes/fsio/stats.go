package fsio

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Włączamy kolory domyślnie (chyba że użytkownik ustawi NO_COLOR=1)
var useColor = os.Getenv("NO_COLOR") == ""

const (
	cReset  = "\x1b[0m"
	cGreen  = "\x1b[32m"
	cLime   = "\x1b[38;5;154m"
	cYellow = "\x1b[33m"
	cOrange = "\x1b[38;5;208m"
	cRed    = "\x1b[1;31m"

	statsBarWidth   = 20
	statsLabelWidth = 36
)

type StatEntry struct {
	RelPath   string
	Size      int64
	FuncCount int
}

func colorFor(tokens, maxTokens int64) string {
	if maxTokens <= 0 {
		return cGreen
	}
	ratio := float64(tokens) / float64(maxTokens)
	switch {
	case ratio >= 0.80:
		return cRed
	case ratio >= 0.55:
		return cOrange
	case ratio >= 0.30:
		return cYellow
	case ratio >= 0.12:
		return cLime
	default:
		return cGreen
	}
}

func paint(s, color string) string {
	if !useColor {
		return s
	}
	return color + s + cReset
}

func renderBar(tokens, maxTokens int64, color string) string {
	if maxTokens <= 0 {
		return strings.Repeat("░", statsBarWidth)
	}
	filled := int(float64(tokens) / float64(maxTokens) * float64(statsBarWidth))
	if filled > statsBarWidth {
		filled = statsBarWidth
	}
	if filled < 0 {
		filled = 0
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", statsBarWidth-filled)
	return paint(bar, color)
}

type statNode struct {
	name     string
	isDir    bool
	tokens   int64
	funcs    int
	children map[string]*statNode
}

func buildStatTree(entries []StatEntry) *statNode {
	root := &statNode{name: ".", isDir: true, children: map[string]*statNode{}}
	for _, e := range entries {
		curr := root
		parts := strings.Split(filepath.ToSlash(e.RelPath), "/")
		for i, part := range parts {
			child, ok := curr.children[part]
			if !ok {
				child = &statNode{name: part, isDir: i < len(parts)-1, children: map[string]*statNode{}}
				curr.children[part] = child
			}
			curr = child
		}
		curr.tokens = int64(float64(e.Size) / 3.5)
		curr.funcs = e.FuncCount
	}
	return root
}

func (n *statNode) rollup() (files int, tokens int64, funcs int) {
	if !n.isDir {
		return 1, n.tokens, n.funcs
	}
	for _, c := range n.children {
		f, t, fn := c.rollup()
		files += f
		tokens += t
		funcs += fn
	}
	return
}

func humanTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 10_000:
		return fmt.Sprintf("%dk", n/1000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func GenerateStatsSummary(entries []StatEntry, maxDepth int) string {
	if len(entries) == 0 {
		return ""
	}
	if maxDepth <= 0 {
		maxDepth = 1
	}

	root := buildStatTree(entries)
	_, totalTokens, _ := root.rollup()
	if totalTokens == 0 {
		return ""
	}

	var maxFolderTokens int64
	var findMax func(n *statNode)
	findMax = func(n *statNode) {
		for _, c := range n.children {
			if !c.isDir {
				continue
			}
			_, t, _ := c.rollup()
			if t > maxFolderTokens {
				maxFolderTokens = t
			}
			findMax(c)
		}
	}
	findMax(root)

	sep := strings.Repeat("─", statsLabelWidth+statsBarWidth+32)

	var sb strings.Builder
	sb.WriteString("\n📊 Token Distribution by Folder\n")
	sb.WriteString(sep)
	sb.WriteByte('\n')
	fmt.Fprintf(&sb, "%-*s %6s %9s %7s  %s\n",
		statsLabelWidth, "Folder", "Files", "Tokens", "Share", "Distribution")

	var walk func(n *statNode, prefix string, depth int)
	walk = func(n *statNode, prefix string, depth int) {
		if depth >= maxDepth {
			return
		}

		kids := make([]*statNode, 0, len(n.children))
		for _, c := range n.children {
			if c.isDir {
				kids = append(kids, c)
			}
		}
		sort.Slice(kids, func(i, j int) bool {
			_, ti, _ := kids[i].rollup()
			_, tj, _ := kids[j].rollup()
			if ti != tj {
				return ti > tj
			}
			return kids[i].name < kids[j].name
		})

		for i, c := range kids {
			connector, sub := "├─ ", "│  "
			if i == len(kids)-1 {
				connector, sub = "└─ ", "   "
			}

			f, t, _ := c.rollup()
			pct := float64(t) / float64(totalTokens) * 100
			color := colorFor(t, maxFolderTokens)

			label := prefix + connector + c.name + "/"
			if len(label) > statsLabelWidth {
				label = label[:statsLabelWidth-1] + "…"
			}

			tokensStr := paint(fmt.Sprintf("%8s", humanTokens(t)), color)
			pctStr := paint(fmt.Sprintf("%6.1f%%", pct), color)
			bar := renderBar(t, maxFolderTokens, color)

			fmt.Fprintf(&sb, "%-*s %6d %s %s  %s\n",
				statsLabelWidth, label, f, tokensStr, pctStr, bar)

			walk(c, prefix+sub, depth+1)
		}
	}
	walk(root, "", 0)

	sb.WriteString(sep)
	sb.WriteByte('\n')

	f, t, _ := root.rollup()
	fmt.Fprintf(&sb, "%-*s %6d %9s %6s  %s\n",
		statsLabelWidth, "TOTAL", f, humanTokens(t), "100%",
		paint(strings.Repeat("█", statsBarWidth), cRed))

	return sb.String()
}
