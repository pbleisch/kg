package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// RunEmbedSingle invokes the embedding command for a single input.
// Writes JSON to stdin, reads a JSON float array from stdout.
func RunEmbedSingle(command string, input EmbedInput) ([]float64, error) {
	payload, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshaling input: %w", err)
	}

	parts, err := shellSplit(command)
	if err != nil {
		return nil, fmt.Errorf("parsing command: %w", err)
	}

	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Stdin = bytes.NewReader(payload)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("embed command failed: %s: %s", err, msg)
		}
		return nil, fmt.Errorf("embed command failed: %w", err)
	}

	var vec []float64
	if err := json.Unmarshal(stdout.Bytes(), &vec); err != nil {
		return nil, fmt.Errorf("parsing embed output: %w (got %q)", err, stdout.String())
	}
	return vec, nil
}

// batchInput is the JSON envelope for batch mode.
type batchInput struct {
	Items []EmbedInput `json:"items"`
}

// batchOutput is the JSON envelope returned by batch mode.
type batchOutput struct {
	Vectors [][]float64 `json:"vectors"`
}

// RunEmbedBatch invokes the embedding command once with all inputs.
func RunEmbedBatch(command string, inputs []EmbedInput) ([][]float64, error) {
	payload, err := json.Marshal(batchInput{Items: inputs})
	if err != nil {
		return nil, fmt.Errorf("marshaling batch input: %w", err)
	}

	parts, err := shellSplit(command)
	if err != nil {
		return nil, fmt.Errorf("parsing command: %w", err)
	}

	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Stdin = bytes.NewReader(payload)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("embed command failed: %s: %s", err, msg)
		}
		return nil, fmt.Errorf("embed command failed: %w", err)
	}

	var out batchOutput
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return nil, fmt.Errorf("parsing batch output: %w", err)
	}
	return out.Vectors, nil
}

// shellSplit does a simple split of a command string into parts.
// Handles basic quoting with double quotes.
func shellSplit(s string) ([]string, error) {
	var parts []string
	var current strings.Builder
	inQuote := false

	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
		case r == ' ' && !inQuote:
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}
	if inQuote {
		return nil, fmt.Errorf("unterminated quote in command: %s", s)
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	return parts, nil
}
