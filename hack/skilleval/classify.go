package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
)

// bucket is where a tool call's result came from, from the eval's
// point of view: the skill and the docs are what an agent is meant to
// read; source and examples are what the skill failed to spare it.
type bucket string

const (
	bSkill    bucket = "skill"
	bDocs     bucket = "docs"
	bExamples bucket = "examples"
	bSource   bucket = "source"
	bGoDoc    bucket = "godoc"
	bWeb      bucket = "web"
	bApp      bucket = "app"
	bFWWrite  bucket = "fwwrite"
	bOther    bucket = "other"
)

// bucketOrder is the report's column order, and also the precedence
// when one shell command touches several: a cat of SKILL.md and a
// source file still read source.
var bucketOrder = []bucket{bSource, bExamples, bDocs, bSkill, bGoDoc, bWeb, bFWWrite, bApp, bOther}

type classifier struct {
	roots []string // the variant's export: the framework the run is meant to use
	stale []string // module-cache copies: framework too, but not the variant under test
	app   string   // the scaffolded app, and every call's starting directory
}

var writeTools = map[string]bool{"Write": true, "Edit": true, "MultiEdit": true, "NotebookEdit": true}

// classify is the bucket a call's result bytes are charged to: the
// most expensive one it touched, since one command's output cannot be
// split between the files it printed.
func (c classifier) classify(tool string, in map[string]any) bucket {
	return c.touches(tool, in)[0]
}

// framework reports p's path inside any framework copy.
func (c classifier) framework(p string) (string, bool) {
	for _, set := range [][]string{{fwMark}, c.roots, c.stale} {
		for _, r := range set {
			if rel, ok := under(p, r); ok {
				return rel, true
			}
		}
	}
	return "", false
}

// abs resolves p the way the tool would: relative to the app, where
// every call starts.
func (c classifier) abs(p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(c.app, p)
}

// touches is every bucket a call reached, most expensive first, so
// that a cat of SKILL.md beside a grep of the source still counts as
// reading the skill.
func (c classifier) touches(tool string, in map[string]any) []bucket {
	str := func(k string) string { s, _ := in[k].(string); return s }
	switch tool {
	case "Bash":
		return c.bashTouches(str("command"))
	case "Skill":
		if strings.Contains(str("skill"), "rastrillo") {
			return []bucket{bSkill}
		}
		return []bucket{bOther}
	case "WebFetch", "WebSearch":
		if tool == "WebSearch" || strings.Contains(str("url"), "rastrillo.org") {
			return []bucket{bWeb}
		}
		return []bucket{bOther}
	}
	p := str("file_path")
	if p == "" {
		p = str("path")
	}
	if p == "" {
		p = str("notebook_path")
	}
	if p == "" && tool == "Glob" && filepath.IsAbs(str("pattern")) {
		p = str("pattern") // an absolute pattern is its own directory
	}
	if p == "" {
		if tool == "Grep" || tool == "Glob" || tool == "LS" {
			return []bucket{bApp} // they default to the working directory, which is the app
		}
		return []bucket{bOther}
	}
	p = c.abs(p)
	if rel, ok := c.framework(p); ok {
		if writeTools[tool] {
			return []bucket{bFWWrite}
		}
		return []bucket{relBucket(rel)}
	}
	if _, ok := under(p, c.app); ok {
		return []bucket{bApp}
	}
	return []bucket{bOther}
}

// under reports p's path below root, so that a sibling directory
// sharing root's name as a prefix (rastrillo-old) is not mistaken for it.
func under(p, root string) (string, bool) {
	if root == "" {
		return "", false
	}
	p = filepath.Clean(p)
	if p == root {
		return "", true
	}
	if strings.HasPrefix(p, root+"/") {
		return p[len(root)+1:], true
	}
	return "", false
}

func relBucket(rel string) bucket {
	switch {
	case rel == "SKILL.md" || strings.HasPrefix(rel, "skill/"):
		return bSkill
	case strings.HasPrefix(rel, "docs/"):
		return bDocs
	case strings.HasPrefix(rel, "examples/"):
		return bExamples
	}
	return bSource // a bare root is a grep or a cd over the whole tree
}

// goListDir matches the usual way an agent finds the framework from a
// shell: a command substitution of go list's module directory, with
// the module path sometimes cut short and stderr sometimes redirected.
var goListDir = regexp.MustCompile(`\$\(\s*go list -m -f\s+['"]?\{\{\.Dir\}\}['"]?\s+amadan\.net/rastrillo(?:/rastrillo)?[^)]*\)`)

// fwMark stands in for a framework directory the command computes
// rather than names.
const fwMark = "/\x00rastrillo"

var (
	assignment = regexp.MustCompile(`(?:^|[;&|(\s])([A-Za-z_][A-Za-z0-9_]*)=["']?([^\s;&|"']+)`)
	pathExt    = map[string]bool{".md": true, ".go": true, ".html": true, ".css": true, ".js": true, ".toml": true, ".sql": true, ".json": true, ".txt": true}
)

// bashTouches walks the command's words the way the shell would reach
// files: go list substitutions and variables holding a framework path
// are expanded, and relative words resolve against a working directory
// that starts at the app and follows each cd.
func (c classifier) bashTouches(cmd string) []bucket {
	found := map[bucket]bool{}
	if strings.Contains(cmd, "go doc ") {
		found[bGoDoc] = true
	}
	if strings.Contains(cmd, "rastrillo.org") {
		found[bWeb] = true
	}
	cmd = goListDir.ReplaceAllString(cmd, fwMark)
	for _, m := range assignment.FindAllStringSubmatch(cmd, -1) {
		if _, ok := c.framework(m[2]); ok {
			cmd = strings.ReplaceAll(cmd, "${"+m[1]+"}", m[2])
			cmd = regexp.MustCompile(`\$`+m[1]+`\b`).ReplaceAllString(cmd, m[2])
		}
	}
	words := strings.FieldsFunc(cmd, func(r rune) bool {
		return strings.ContainsRune(" \t\n;|&()<>'\"`", r)
	})
	cwd := c.app
	for i := 0; i < len(words); i++ {
		w := words[i]
		if w == "cd" && i+1 < len(words) {
			i++
			if t := words[i]; filepath.IsAbs(t) {
				cwd = filepath.Clean(t)
			} else if !strings.HasPrefix(t, "$") && !strings.HasPrefix(t, "~") {
				cwd = filepath.Join(cwd, t)
			}
			continue
		}
		if !strings.HasPrefix(w, "/") && !pathish(w) {
			continue
		}
		p := w
		if !filepath.IsAbs(p) {
			p = filepath.Join(cwd, p)
		}
		if rel, ok := c.framework(p); ok {
			found[relBucket(rel)] = true
		}
	}
	var out []bucket
	for _, b := range bucketOrder {
		if found[b] {
			out = append(out, b)
		}
	}
	if len(out) > 0 {
		return out
	}
	if strings.Contains(cmd, "go list -m") && strings.Contains(cmd, "amadan.net/rastrillo") {
		return []bucket{bOther}
	}
	return []bucket{bApp}
}

// pathish is a word that names a file or directory, rather than a flag,
// a pattern or an assignment.
func pathish(w string) bool {
	if strings.HasPrefix(w, "-") || strings.HasPrefix(w, "$") || strings.Contains(w, "=") {
		return false
	}
	return w == "." || w == "*" || strings.Contains(w, "/") || pathExt[filepath.Ext(w)]
}

// mentionsStale reports whether any of the call's arguments names a
// module-cache copy of the framework.
func (c classifier) mentionsStale(in map[string]any) bool {
	for _, v := range in {
		s, ok := v.(string)
		if !ok {
			continue
		}
		for _, r := range c.stale {
			if strings.Contains(s, r) {
				return true
			}
		}
	}
	return false
}

type count struct {
	Calls int `json:"calls"`
	Bytes int `json:"bytes"`
}

type tally struct {
	Buckets       map[bucket]count `json:"buckets"` // bytes and calls, each call charged to the most expensive bucket it touched
	Touched       map[bucket]int   `json:"touched"` // calls that reached each bucket at all
	StaleCalls    int              `json:"stale_calls"`
	SourceReads   []string         `json:"source_reads"`
	Model         string           `json:"model"`
	ClaudeVersion string           `json:"claude_version"`
	MemoryPaths   json.RawMessage  `json:"memory_paths,omitempty"` // what instructions the session loaded besides the task
	Turns         int              `json:"turns"`
	DurationMS    int              `json:"duration_ms"`
	CostUSD       float64          `json:"cost_usd"`
	Tokens        int              `json:"tokens"` // every input token the model processed, cached or not, plus output
	OutputTokens  int              `json:"output_tokens"`
	Partial       bool             `json:"partial"` // no result event: tokens are summed from assistant events and cost is unknown
	Finished      bool             `json:"finished"`
	Subtype       string           `json:"subtype"`
}

type usage struct {
	Input         int `json:"input_tokens"`
	CacheCreation int `json:"cache_creation_input_tokens"`
	CacheRead     int `json:"cache_read_input_tokens"`
	Output        int `json:"output_tokens"`
}

func (u usage) total() int { return u.Input + u.CacheCreation + u.CacheRead + u.Output }

type streamEvent struct {
	Type          string          `json:"type"`
	Subtype       string          `json:"subtype"`
	Model         string          `json:"model"`
	ClaudeVersion string          `json:"claude_code_version"`
	MemoryPaths   json.RawMessage `json:"memory_paths"`
	Message       struct {
		ID      string          `json:"id"`
		Content json.RawMessage `json:"content"`
		Usage   *usage          `json:"usage"`
	} `json:"message"`
	IsError    bool    `json:"is_error"`
	NumTurns   int     `json:"num_turns"`
	DurationMS int     `json:"duration_ms"`
	CostUSD    float64 `json:"total_cost_usd"`
	Usage      usage   `json:"usage"`
}

type contentBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     map[string]any  `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
}

// tallyTranscript reads claude -p's stream-json output.
func tallyTranscript(r io.Reader, c classifier) (tally, error) {
	t := tally{Buckets: map[bucket]count{}, Touched: map[bucket]int{}}
	pending := map[string]bucket{}
	perMessage := map[string]usage{} // assistant events repeat a message's usage once per content block
	var order []string
	sawResult := false
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<20), 256<<20) // one tool result can be a whole file
	for sc.Scan() {
		var e streamEvent
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			continue // stderr noise or a truncated last line
		}
		switch e.Type {
		case "system":
			if e.Subtype == "init" {
				t.Model, t.ClaudeVersion, t.MemoryPaths = e.Model, e.ClaudeVersion, e.MemoryPaths
			}
		case "assistant", "user":
			if e.Type == "assistant" && e.Message.Usage != nil && e.Message.ID != "" {
				if _, seen := perMessage[e.Message.ID]; !seen {
					order = append(order, e.Message.ID)
				}
				perMessage[e.Message.ID] = *e.Message.Usage
			}
			var blocks []contentBlock
			if json.Unmarshal(e.Message.Content, &blocks) != nil {
				continue // a plain-string user message
			}
			for _, b := range blocks {
				switch b.Type {
				case "tool_use":
					bs := c.touches(b.Name, b.Input)
					bk := bs[0]
					pending[b.ID] = bk
					n := t.Buckets[bk]
					n.Calls++
					t.Buckets[bk] = n
					for _, x := range bs {
						t.Touched[x]++
					}
					if c.mentionsStale(b.Input) {
						t.StaleCalls++
					}
					if bk == bSource || bk == bExamples {
						t.SourceReads = append(t.SourceReads, describe(b.Name, b.Input))
					}
				case "tool_result":
					bk, ok := pending[b.ToolUseID]
					if !ok {
						continue
					}
					n := t.Buckets[bk]
					n.Bytes += resultBytes(b.Content)
					t.Buckets[bk] = n
				}
			}
		case "result":
			sawResult = true
			t.Subtype = e.Subtype
			t.Finished = e.Subtype == "success" && !e.IsError
			t.Turns = e.NumTurns
			t.DurationMS = e.DurationMS
			t.CostUSD = e.CostUSD
			t.OutputTokens = e.Usage.Output
			t.Tokens = e.Usage.total()
		}
	}
	if !sawResult && len(order) > 0 {
		t.Partial = true
		for _, id := range order {
			u := perMessage[id]
			t.Tokens += u.total()
			t.OutputTokens += u.Output
		}
		t.Turns = len(order)
	}
	return t, sc.Err()
}

// resultBytes counts the text a tool result put into context. Content
// is a string or a list of blocks; images are not text and the eval's
// tasks do not produce them.
func resultBytes(raw json.RawMessage) int {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return len(s)
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		n := 0
		for _, b := range blocks {
			n += len(b.Text)
		}
		return n
	}
	return len(raw)
}

func describe(tool string, in map[string]any) string {
	var arg string
	for _, k := range []string{"command", "file_path", "path", "pattern"} {
		if s, ok := in[k].(string); ok && s != "" {
			arg = s
			break
		}
	}
	if tool == "Grep" || tool == "Glob" {
		p, _ := in["pattern"].(string)
		d, _ := in["path"].(string)
		arg = strings.TrimSpace(p + " " + d)
	}
	if len(arg) > 200 {
		arg = arg[:200] + "…"
	}
	return fmt.Sprintf("%s %s", tool, arg)
}
