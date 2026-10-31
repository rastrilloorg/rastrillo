// Package transcript reads the stream-json a `claude -p` run writes, and
// counts it the way a bill does: once per message, not once per event.
package transcript

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
)

type Usage struct{ Input, Output, CacheRead, CacheWrite int }

type Tool struct {
	Name      string
	Input     map[string]any
	ResultLen int // characters of the matching tool_result
}

type Message struct {
	ID      string
	Context int // input + cache_read + cache_creation at this message
	Output  int
	Tools   []Tool
	Text    string
}

type Init struct {
	SessionID                   string
	Version, Model, OutputStyle string
	Plugins                     []string // installed: the config's
	BuiltinPlugins              []string // shipped with Claude Code itself
}

type Result struct {
	NumTurns int
	CostUSD  float64 // claude's own total_cost_usd: the whole session's
	// PhaseCostUSD is this prompt's cost alone. A resumed session's
	// total_cost_usd carries every earlier prompt's spend, which once
	// billed a $2.38 follow-up as $9.42.
	PhaseCostUSD float64
	DurationMS   int64
	IsError      bool
	Subtype      string
	APIError     string
	Text         string // the agent's final answer
	Usage        Usage
}

type Transcript struct {
	Init     Init
	Messages []Message
	Result   *Result // nil when the run never finished
}

type usageLine struct {
	Input      int `json:"input_tokens"`
	Output     int `json:"output_tokens"`
	CacheRead  int `json:"cache_read_input_tokens"`
	CacheWrite int `json:"cache_creation_input_tokens"`
}

type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     map[string]any  `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
}

type line struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`

	SessionID   string            `json:"session_id"`
	Version     string            `json:"claude_code_version"`
	Model       string            `json:"model"`
	OutputStyle string            `json:"output_style"`
	Plugins     []json.RawMessage `json:"plugins"`

	Message struct {
		ID      string          `json:"id"`
		Content json.RawMessage `json:"content"`
		Usage   usageLine       `json:"usage"`
	} `json:"message"`

	NumTurns   int     `json:"num_turns"`
	CostUSD    float64 `json:"total_cost_usd"`
	DurationMS int64   `json:"duration_ms"`
	IsError    bool    `json:"is_error"`
	APIError   any     `json:"api_error_status"`
	Text       string  `json:"result"`
	SinceLast  map[string]struct {
		CostUSD float64 `json:"costUSD"`
	} `json:"model_usage_since_last_result"`
	Usage usageLine `json:"usage"`
}

// Parse reads a whole run. Stream-json sends each content block of a
// message as its own event, repeating the message's id and usage, so
// usage is taken from the first event of an id; adding them up would
// bill a three-block message three times.
func Parse(r io.Reader) (Transcript, error) {
	var transcript Transcript
	indexOf := map[string]int{}
	toolAt := map[string][2]int{} // tool_use id -> message, tool

	reader := bufio.NewReader(r)
	for {
		raw, err := reader.ReadBytes('\n')
		if len(raw) > 0 {
			readLine(raw, &transcript, indexOf, toolAt)
		}
		if errors.Is(err, io.EOF) {
			return transcript, nil
		}
		if err != nil {
			return transcript, err
		}
	}
}

func readLine(
	raw []byte, transcript *Transcript,
	indexOf map[string]int, toolAt map[string][2]int,
) {
	var event line
	// A run killed mid-write ends on half a line; what came before it
	// is still the run.
	if json.Unmarshal(raw, &event) != nil {
		return
	}
	switch {
	case event.Type == "system" && event.Subtype == "init":
		transcript.Init = Init{
			SessionID:   event.SessionID,
			Version:     event.Version,
			Model:       event.Model,
			OutputStyle: event.OutputStyle,
		}
		transcript.Init.Plugins, transcript.Init.BuiltinPlugins =
			pluginNames(event.Plugins)
	case event.Type == "assistant":
		readAssistant(event, transcript, indexOf, toolAt)
	case event.Type == "user":
		readResults(event, transcript, toolAt)
	case event.Type == "result":
		transcript.Result = &Result{
			NumTurns:   event.NumTurns,
			CostUSD:    event.CostUSD,
			DurationMS: event.DurationMS,
			IsError:    event.IsError,
			Subtype:    event.Subtype,
			APIError:   apiError(event.APIError),
			Text:       event.Text,
			Usage:      Usage(event.Usage),
		}
		transcript.Result.PhaseCostUSD = event.CostUSD
		if len(event.SinceLast) > 0 {
			transcript.Result.PhaseCostUSD = 0
			for _, model := range event.SinceLast {
				transcript.Result.PhaseCostUSD += model.CostUSD
			}
		}
	}
}

func readAssistant(
	event line, transcript *Transcript,
	indexOf map[string]int, toolAt map[string][2]int,
) {
	index, seen := indexOf[event.Message.ID]
	if !seen {
		usage := event.Message.Usage
		transcript.Messages = append(transcript.Messages, Message{
			ID:      event.Message.ID,
			Context: usage.Input + usage.CacheRead + usage.CacheWrite,
			Output:  usage.Output,
		})
		index = len(transcript.Messages) - 1
		indexOf[event.Message.ID] = index
	}
	message := &transcript.Messages[index]
	var blocks []block
	if json.Unmarshal(event.Message.Content, &blocks) != nil {
		return
	}
	for _, b := range blocks {
		switch b.Type {
		case "text":
			message.Text += b.Text
		case "tool_use":
			message.Tools = append(message.Tools,
				Tool{Name: b.Name, Input: b.Input})
			toolAt[b.ID] = [2]int{index, len(message.Tools) - 1}
		}
	}
}

func readResults(
	event line, transcript *Transcript, toolAt map[string][2]int,
) {
	var blocks []block
	if json.Unmarshal(event.Message.Content, &blocks) != nil {
		return
	}
	for _, b := range blocks {
		at, known := toolAt[b.ToolUseID]
		if b.Type != "tool_result" || !known {
			continue
		}
		tool := &transcript.Messages[at[0]].Tools[at[1]]
		tool.ResultLen = len([]rune(resultText(b.Content)))
	}
}

// resultText reads a tool result's content, which arrives either as a
// string or as a list of text blocks.
func resultText(content json.RawMessage) string {
	var text string
	if json.Unmarshal(content, &text) == nil {
		return text
	}
	var blocks []block
	if json.Unmarshal(content, &blocks) != nil {
		return ""
	}
	for _, b := range blocks {
		text += b.Text
	}
	return text
}

// pluginNames splits the plugins claude loaded into installed ones and
// those it ships with itself (path "builtin"), which every user has. A
// plugin is accepted as a bare name or an object, so a format change
// cannot hide an installed plugin from the clean-config check.
func pluginNames(plugins []json.RawMessage) (installed, builtin []string) {
	for _, raw := range plugins {
		var name string
		if json.Unmarshal(raw, &name) == nil {
			installed = append(installed, name)
			continue
		}
		var object struct{ Name, Path string }
		json.Unmarshal(raw, &object)
		if object.Name == "" {
			object.Name = string(raw)
		}
		if object.Path == "builtin" {
			builtin = append(builtin, object.Name)
		} else {
			installed = append(installed, object.Name)
		}
	}
	return installed, builtin
}

func apiError(status any) string {
	if status == nil {
		return ""
	}
	encoded, _ := json.Marshal(status)
	return string(encoded)
}
