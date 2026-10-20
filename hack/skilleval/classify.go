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
	roots []string // framework copies the agent can reach: the replace target and any module-cache versions
	app   string
}

var writeTools = map[string]bool{"Write": true, "Edit": true, "MultiEdit": true, "NotebookEdit": true}

func (c classifier) classify(tool string, in map[string]any) bucket {
	str := func(k string) string { s, _ := in[k].(string); return s }
	switch tool {
	case "Bash":
		return c.classifyBash(str("command"))
	case "Skill":
		if strings.Contains(str("skill"), "rastrillo") {
			return bSkill
		}
		return bOther
	case "WebFetch", "WebSearch":
		if tool == "WebSearch" || strings.Contains(str("url"), "rastrillo.org") {
			return bWeb
		}
		return bOther
	}
	p := str("file_path")
	if p == "" {
		p = str("path")
	}
	if p == "" {
		p = str("notebook_path")
	}
	if p == "" {
		return bApp // Grep and Glob default to the working directory, which is the app
	}
	for _, root := range c.roots {
		if rel, ok := under(p, root); ok {
			if writeTools[tool] {
				return bFWWrite
			}
			return relBucket(rel)
		}
	}
	if _, ok := under(p, c.app); ok {
		return bApp
	}
	return bOther
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
// shell: a command substitution of go list's module directory. What
// follows the closing paren is the path inside the module.
var goListDir = regexp.MustCompile(`\$\(\s*go list -m -f\s+['"]?\{\{\.Dir\}\}['"]?\s+amadan\.net/rastrillo/rastrillo\s*\)`)

func (c classifier) classifyBash(cmd string) bucket {
	if strings.Contains(cmd, "go doc ") {
		return bGoDoc
	}
	if strings.Contains(cmd, "rastrillo.org") {
		return bWeb
	}
	var found []bucket
	for _, root := range c.roots {
		for rest := cmd; ; {
			i := strings.Index(rest, root)
			if i < 0 {
				break
			}
			rest = rest[i+len(root):]
			found = append(found, relBucket(strings.TrimPrefix(pathPrefix(rest), "/")))
		}
	}
	for _, loc := range goListDir.FindAllStringIndex(cmd, -1) {
		found = append(found, relBucket(strings.TrimPrefix(pathPrefix(cmd[loc[1]:]), "/")))
	}
	for _, b := range bucketOrder {
		for _, f := range found {
			if f == b {
				return b
			}
		}
	}
	if strings.Contains(cmd, "go list -m") && strings.Contains(cmd, "amadan.net/rastrillo") {
		return bOther
	}
	return bApp
}

// pathPrefix is the path that continues at the start of s, ending where
// the shell word does.
func pathPrefix(s string) string {
	if i := strings.IndexAny(s, " \t\n'\"`);|&><"); i >= 0 {
		return s[:i]
	}
	return s
}

type count struct {
	Calls int `json:"calls"`
	Bytes int `json:"bytes"`
}

type tally struct {
	Buckets      map[bucket]count `json:"buckets"`
	SourceReads  []string         `json:"source_reads"`
	Model        string           `json:"model"`
	Turns        int              `json:"turns"`
	DurationMS   int              `json:"duration_ms"`
	CostUSD      float64          `json:"cost_usd"`
	Tokens       int              `json:"tokens"` // every input token the model processed, cached or not, plus output
	OutputTokens int              `json:"output_tokens"`
	Finished     bool             `json:"finished"`
	Subtype      string           `json:"subtype"`
}

type streamEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Model   string `json:"model"`
	Message struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	IsError    bool    `json:"is_error"`
	NumTurns   int     `json:"num_turns"`
	DurationMS int     `json:"duration_ms"`
	CostUSD    float64 `json:"total_cost_usd"`
	Usage      struct {
		Input         int `json:"input_tokens"`
		CacheCreation int `json:"cache_creation_input_tokens"`
		CacheRead     int `json:"cache_read_input_tokens"`
		Output        int `json:"output_tokens"`
	} `json:"usage"`
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
	t := tally{Buckets: map[bucket]count{}}
	pending := map[string]bucket{}
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
				t.Model = e.Model
			}
		case "assistant", "user":
			var blocks []contentBlock
			if json.Unmarshal(e.Message.Content, &blocks) != nil {
				continue // a plain-string user message
			}
			for _, b := range blocks {
				switch b.Type {
				case "tool_use":
					bk := c.classify(b.Name, b.Input)
					pending[b.ID] = bk
					n := t.Buckets[bk]
					n.Calls++
					t.Buckets[bk] = n
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
			t.Subtype = e.Subtype
			t.Finished = e.Subtype == "success" && !e.IsError
			t.Turns = e.NumTurns
			t.DurationMS = e.DurationMS
			t.CostUSD = e.CostUSD
			t.OutputTokens = e.Usage.Output
			t.Tokens = e.Usage.Input + e.Usage.CacheCreation + e.Usage.CacheRead + e.Usage.Output
		}
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
