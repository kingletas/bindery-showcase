package guard

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// MaxCallBytes bounds how much of standard input is read. A larger call is
// refused rather than truncated.
const MaxCallBytes = 1 << 20

// Call is one tool call as a coding agent's pre-tool-use hook sends it. Fields
// the guards do not use, such as a session identifier, are ignored.
type Call struct {
	ToolName  string    `json:"tool_name"`
	ToolInput ToolInput `json:"tool_input"`
	// Cwd is the directory the tool runs in. When absent the runner's own
	// working directory is used.
	Cwd string `json:"cwd"`
}

// ToolInput holds the parts of a tool's input that the guards read.
type ToolInput struct {
	// Command is the shell command line of a Bash call.
	Command string `json:"command"`
	// FilePath is the file a Read call opens.
	FilePath string `json:"file_path"`
}

// ShellTool is the tool name whose input carries a shell command line.
const ShellTool = "Bash"

// ReadTool is the tool name whose input names a file to read.
const ReadTool = "Read"

// DecodeCall reads exactly one tool call from r. Anything that is not a single
// JSON object of the expected shape is an error.
func DecodeCall(r io.Reader) (Call, error) {
	raw, err := io.ReadAll(io.LimitReader(r, MaxCallBytes+1))
	if err != nil {
		return Call{}, fmt.Errorf("standard input could not be read: %w", err)
	}
	if len(raw) > MaxCallBytes {
		return Call{}, errors.New("the tool call is larger than the limit")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	var fields struct {
		ToolName  *string          `json:"tool_name"`
		ToolInput *json.RawMessage `json:"tool_input"`
		Cwd       string           `json:"cwd"`
	}
	if err := dec.Decode(&fields); err != nil {
		return Call{}, fmt.Errorf("the tool call is not valid JSON of the expected shape: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return Call{}, errors.New("the tool call has data after its JSON object")
	}
	if fields.ToolName == nil || *fields.ToolName == "" {
		return Call{}, errors.New("the tool call has no tool_name")
	}
	call := Call{ToolName: *fields.ToolName, Cwd: fields.Cwd}
	if fields.ToolInput == nil {
		return Call{}, errors.New("the tool call has no tool_input")
	}
	if err := json.Unmarshal(*fields.ToolInput, &call.ToolInput); err != nil {
		return Call{}, fmt.Errorf("tool_input is not an object of the expected shape: %w", err)
	}
	if call.ToolName == ShellTool && call.ToolInput.Command == "" {
		return Call{}, errors.New("a Bash call has no command")
	}
	return call, nil
}
