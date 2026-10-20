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
	roots     []string // the variant's export: the framework the run is meant to use
	staleBase string   // <GOMODCACHE>/amadan.net/rastrillo/rastrillo: every version under it is framework too, but not the variant under test
	app       string   // the scaffolded app: the project, where a session starts
	cwd       string   // where the next Bash call starts; Claude Code carries it while it stays inside the project
}

var writeTools = map[string]bool{"Write": true, "Edit": true, "MultiEdit": true, "NotebookEdit": true}

// classify is the bucket a call's result bytes are charged to: the
// most expensive one it touched, since one command's output cannot be
// split between the files it printed.
func (c classifier) classify(tool string, in map[string]any) bucket {
	b, _ := c.touches(tool, in)
	return b[0]
}

// framework reports p's path inside any framework copy, and whether the
// copy is a stale one rather than the variant under test.
func (c classifier) framework(p string) (rel string, ok, stale bool) {
	for _, r := range append([]string{fwMark}, c.roots...) {
		if rel, ok := under(p, r); ok {
			return rel, true, false
		}
	}
	// A cache version is matched by its name's shape, not by a listing,
	// so one the agent downloads mid-run, or one since deleted from the
	// cache, still counts.
	if c.staleBase != "" && strings.HasPrefix(p, c.staleBase+"@") {
		rest := p[len(c.staleBase)+1:]
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			return rest[i+1:], true, true
		}
		return "", true, true
	}
	return "", false, false
}

func (c classifier) start() string {
	if c.cwd != "" {
		return c.cwd
	}
	return c.app
}

// abs resolves p the way the tool would: relative to where the call
// starts.
func (c classifier) abs(p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(c.start(), p)
}

// touches is every bucket a call reached, most expensive first, so
// that a cat of SKILL.md beside a grep of the source still counts as
// reading the skill; stale reports a read of a module-cache copy.
func (c classifier) touches(tool string, in map[string]any) (bs []bucket, stale bool) {
	if tool == "Bash" {
		bs, stale, _ = c.bashTouches(in["command"].(string))
		return bs, stale
	}
	return c.fileTouches(tool, in)
}

func (c classifier) fileTouches(tool string, in map[string]any) ([]bucket, bool) {
	str := func(k string) string { s, _ := in[k].(string); return s }
	switch tool {
	case "Skill":
		if strings.Contains(str("skill"), "rastrillo") {
			return []bucket{bSkill}, false
		}
		return []bucket{bOther}, false
	case "WebFetch", "WebSearch":
		if tool == "WebSearch" || strings.Contains(str("url"), "rastrillo.org") {
			return []bucket{bWeb}, false
		}
		return []bucket{bOther}, false
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
			p = "." // they default to the working directory
		} else {
			return []bucket{bOther}, false
		}
	}
	p = c.abs(p)
	if rel, ok, stale := c.framework(p); ok {
		if writeTools[tool] {
			return []bucket{bFWWrite}, stale
		}
		return []bucket{relBucket(rel)}, stale
	}
	if _, ok := under(p, c.app); ok {
		return []bucket{bApp}, false
	}
	return []bucket{bOther}, false
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
	case rel == "SKILL.md" || rel == "skill" || strings.HasPrefix(rel, "skill/"):
		return bSkill
	case rel == "docs" || strings.HasPrefix(rel, "docs/"):
		return bDocs
	case rel == "examples" || strings.HasPrefix(rel, "examples/"):
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

// searchVerbs read the directory they run in when given no path, so
// inside the framework they read it even with no path-like word. Their
// words are read by role: rg, grep, ag and fd take a pattern first
// (which may look like a file name and is not one), and every other
// non-option word is a path, bare names included.
var searchVerbs = map[string]bool{"rg": true, "ag": true, "grep": true, "find": true, "ls": true, "tree": true, "fd": true}

var patternFirst = map[string]bool{"rg": true, "ag": true, "grep": true, "fd": true}

// optArg are the options whose value is the next word. -e and -f name
// the pattern, so the first plain word after them is a path.
var optArg = map[string]bool{"-e": true, "--regexp": true, "-f": true, "--file": true, "-g": true, "--glob": true,
	"-t": true, "--type": true, "-T": true, "--type-not": true, "-A": true, "-B": true, "-C": true, "-m": true,
	"--max-count": true, "-M": true, "-j": true, "-d": true, "-D": true, "-L": true, "--include": true, "--exclude": true}

// bashTouches walks the command the way the shell would reach files:
// go list substitutions and variables holding a framework path are
// expanded, quoted words stay whole, and relative words resolve against
// a working directory that starts where the call does, follows each cd
// and is restored when a subshell closes. It also reports where the
// call leaves the shell, for the next call to start from.
func (c classifier) bashTouches(cmd string) (bs []bucket, stale bool, end string) {
	found := map[bucket]bool{}
	if strings.Contains(cmd, "go doc ") {
		found[bGoDoc] = true
	}
	if strings.Contains(cmd, "rastrillo.org") {
		found[bWeb] = true
	}
	cmd = goListDir.ReplaceAllString(cmd, fwMark)
	for _, m := range assignment.FindAllStringSubmatch(cmd, -1) {
		if _, ok, _ := c.framework(m[2]); ok {
			cmd = strings.ReplaceAll(cmd, "${"+m[1]+"}", m[2])
			cmd = regexp.MustCompile(`\$`+m[1]+`\b`).ReplaceAllString(cmd, m[2])
		}
	}
	touch := func(p string) {
		if rel, ok, st := c.framework(p); ok {
			found[relBucket(rel)] = true
			stale = stale || st
		}
	}
	cwd := c.start()
	var stack []string
	atStart, afterPipe, cdNext := true, false, false
	verb, patternPending, skipNext, findExpr := "", false, false, false
	quiet := false // the rest of this command reads no files
	// implicit is the directory a search verb reads when the command
	// names no path of its own; it is settled when the command ends.
	implicit := ""
	settle := func() {
		if implicit != "" {
			touch(implicit)
			implicit = ""
		}
	}
	for _, tk := range lex(cmd) {
		w := tk.s
		switch {
		case !tk.op:
		case w == "(":
			settle()
			stack = append(stack, cwd)
			atStart, afterPipe, verb = true, false, ""
			continue
		case w == ")":
			settle()
			if n := len(stack); n > 0 {
				cwd, stack = stack[n-1], stack[:n-1]
			}
			atStart, afterPipe, verb = true, false, ""
			continue
		case w == ";":
			settle()
			atStart, afterPipe, verb = true, false, ""
			continue
		case w == "|":
			settle()
			atStart, afterPipe, verb = true, true, ""
			continue
		}
		if quiet && !atStart {
			continue
		}
		if cdNext {
			cdNext = false
			if filepath.IsAbs(w) {
				cwd = filepath.Clean(w)
			} else if !strings.HasPrefix(w, "$") && !strings.HasPrefix(w, "~") && !strings.HasPrefix(w, "-") {
				cwd = filepath.Join(cwd, w)
			}
			continue
		}
		if atStart {
			atStart = false
			verb, patternPending, skipNext, findExpr, quiet = w, patternFirst[w], false, false, false
			if w == "cd" {
				cdNext = true
				continue
			}
			if searchVerbs[w] && !afterPipe {
				implicit = cwd
			}
			continue
		}
		if searchVerbs[verb] {
			switch {
			case skipNext:
				skipNext = false
			case strings.HasPrefix(w, "-") && w != "-":
				if verb == "find" {
					// -H, -L, -P and -O<n> come before find's paths; any
					// other dash word starts its expression.
					if !(w == "-H" || w == "-L" || w == "-P" || strings.HasPrefix(w, "-O")) {
						findExpr = true
					}
				} else if w == "--files" {
					patternPending = false // rg --files lists paths and takes no pattern
				} else if w == "--type-list" {
					implicit, quiet = "", true // prints rg's built-in types; reads nothing
				} else if optArg[w] {
					skipNext = true
					if w == "-e" || w == "--regexp" || w == "-f" || w == "--file" {
						patternPending = false
					}
				}
			case findExpr:
			case patternPending:
				patternPending = false
			default:
				p := w
				if !filepath.IsAbs(p) {
					p = filepath.Join(cwd, p)
				}
				touch(filepath.Clean(p))
				implicit = ""
			}
			continue
		}
		if filepath.IsAbs(w) {
			touch(filepath.Clean(w))
			implicit = ""
		} else if pathish(w) {
			touch(filepath.Join(cwd, w))
			implicit = ""
		}
	}
	settle()
	// Claude Code keeps a cd for the next call only while it stays in
	// the project; leaving it sends the shell back to the project root.
	end = c.app
	if _, ok := under(cwd, c.app); ok && len(stack) == 0 {
		end = cwd
	}
	for _, b := range bucketOrder {
		if found[b] {
			bs = append(bs, b)
		}
	}
	if len(bs) > 0 {
		return bs, stale, end
	}
	if strings.Contains(cmd, "go list -m") && strings.Contains(cmd, "amadan.net/rastrillo") {
		return []bucket{bOther}, false, end
	}
	return []bucket{bApp}, false, end
}

// token is a word or an operator. They are told apart by type, not by
// text: a quoted '(' is a word, and a grep for it is not a subshell.
type token struct {
	s  string
	op bool
}

// lex splits a command into words and the operators that change how
// they are read: ";" ends a command (so do && and ||), "|" ends one and
// feeds the next, "(" and ")" open and close a subshell ($( included).
// Quotes keep a word whole; redirections only end a word.
func lex(cmd string) []token {
	var out []token
	var cur strings.Builder
	inWord := false
	flush := func() {
		if inWord {
			out = append(out, token{s: cur.String()})
			cur.Reset()
			inWord = false
		}
	}
	op := func(s string) { out = append(out, token{s: s, op: true}) }
	rs := []rune(cmd)
	var quote rune
	// parens records, for each open subshell, whether it opened inside
	// double quotes, where its close resumes the quoted word.
	var parens []bool
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		next := rune(0)
		if i+1 < len(rs) {
			next = rs[i+1]
		}
		if quote != 0 {
			switch {
			case r == quote:
				quote = 0
			case quote == '"' && r == '$' && next == '(':
				// A substitution runs even inside double quotes.
				flush()
				i++
				op("(")
				parens = append(parens, true)
				quote = 0
			case r == '\\' && quote == '"' && i+1 < len(rs):
				i++
				cur.WriteRune(rs[i])
			default:
				cur.WriteRune(r)
			}
			continue
		}
		switch {
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case r == '\\' && next != 0:
			i++
			cur.WriteRune(next)
			inWord = true
		case r == '\n' || r == ';':
			flush()
			op(";")
		case r == '&':
			flush()
			if i > 0 && (rs[i-1] == '>' || rs[i-1] == '<') {
				continue // the & of 2>&1 is a redirection, not a separator
			}
			if next == '&' {
				i++
			}
			op(";")
		case r == '|':
			flush()
			if next == '|' {
				i++
				op(";")
			} else {
				op("|")
			}
		case r == '$' && next == '(':
			flush()
			i++
			op("(")
			parens = append(parens, false)
		case r == '(':
			flush()
			op("(")
			parens = append(parens, false)
		case r == ')':
			flush()
			op(")")
			if n := len(parens); n > 0 {
				if parens[n-1] {
					quote, inWord = '"', true
				}
				parens = parens[:n-1]
			}
		case r == ' ' || r == '\t' || r == '<' || r == '>' || r == '`':
			flush()
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	flush()
	return out
}

// pathish is a word that names a file or directory, rather than a flag,
// a pattern or an assignment.
func pathish(w string) bool {
	if strings.HasPrefix(w, "-") || strings.HasPrefix(w, "$") || strings.Contains(w, "=") {
		return false
	}
	return w == "." || w == "*" || strings.Contains(w, "/") || pathExt[filepath.Ext(w)]
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
					var bs []bucket
					var stale bool
					if cmd, ok := b.Input["command"].(string); ok && b.Name == "Bash" {
						bs, stale, c.cwd = c.bashTouches(cmd)
					} else {
						bs, stale = c.fileTouches(b.Name, b.Input)
					}
					bk := bs[0]
					pending[b.ID] = bk
					n := t.Buckets[bk]
					n.Calls++
					t.Buckets[bk] = n
					for _, x := range bs {
						t.Touched[x]++
					}
					if stale {
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
	if !sawResult {
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
