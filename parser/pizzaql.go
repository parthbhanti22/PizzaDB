package parser

import (
	"fmt"
	"strings"
)

// Command represents a parsed PizzaQL command from the client.
type Command struct {
	Op    string // "SET", "GET", "DEL", "PING", "AUTH"
	Key   string // The key operand (empty for PING)
	Value string // The value operand (only for SET and AUTH)
}

// Parse takes a raw input line from the TCP socket and returns a structured
// Command. It handles:
//   - Arbitrary leading/trailing whitespace
//   - Multiple spaces between tokens
//   - Double-quoted values that preserve internal whitespace
//     e.g. SET user:1 "{\"name\": \"Parth Bhanti\"}"
//   - Graceful error messages on malformed input (never panics)
func Parse(input string) (*Command, error) {
	// Trim leading/trailing whitespace and carriage returns (telnet sends \r\n)
	input = strings.TrimSpace(input)

	if len(input) == 0 {
		return nil, fmt.Errorf("empty command")
	}

	// Tokenize with quote awareness
	tokens, err := tokenize(input)
	if err != nil {
		return nil, err
	}

	if len(tokens) == 0 {
		return nil, fmt.Errorf("empty command")
	}

	// Normalize the operation to uppercase
	op := strings.ToUpper(tokens[0])

	switch op {
	case "PING":
		if len(tokens) != 1 {
			return nil, fmt.Errorf("PING takes no arguments, got %d", len(tokens)-1)
		}
		return &Command{Op: "PING"}, nil

	case "GET":
		if len(tokens) != 2 {
			return nil, fmt.Errorf("usage: GET <key>")
		}
		return &Command{Op: "GET", Key: tokens[1]}, nil

	case "DEL":
		if len(tokens) != 2 {
			return nil, fmt.Errorf("usage: DEL <key>")
		}
		return &Command{Op: "DEL", Key: tokens[1]}, nil

	case "SET":
		if len(tokens) < 3 {
			return nil, fmt.Errorf("usage: SET <key> <value>")
		}
		// The value is everything after the key — already properly handled
		// by the tokenizer for quoted strings. If multiple unquoted tokens
		// remain, rejoin them with spaces (lenient mode).
		value := strings.Join(tokens[2:], " ")
		return &Command{Op: "SET", Key: tokens[1], Value: value}, nil

	case "AUTH":
		if len(tokens) != 2 {
			return nil, fmt.Errorf("usage: AUTH <token>")
		}
		return &Command{Op: "AUTH", Value: tokens[1]}, nil

	default:
		return nil, fmt.Errorf("unknown command: %s", op)
	}
}

// tokenize splits a raw input string into tokens, respecting double-quoted
// segments. A quoted segment is treated as a single token with its internal
// whitespace preserved. The surrounding quotes are stripped from the token.
//
// Examples:
//
//	`SET key value`              → ["SET", "key", "value"]
//	`SET key "hello world"`      → ["SET", "key", "hello world"]
//	`SET k "{\"a\": \"b c\"}"`   → ["SET", "k", `{"a": "b c"}`]  (escaped quotes inside)
//	`  GET   mykey  `            → ["GET", "mykey"]
func tokenize(input string) ([]string, error) {
	var tokens []string
	i := 0
	n := len(input)

	for i < n {
		// Skip whitespace between tokens
		if input[i] == ' ' || input[i] == '\t' {
			i++
			continue
		}

		// Quoted token: consume everything until the closing quote
		if input[i] == '"' {
			i++ // skip opening quote
			start := i
			for i < n && input[i] != '"' {
				// Handle escaped quotes inside the value: \"
				if input[i] == '\\' && i+1 < n && input[i+1] == '"' {
					i += 2
					continue
				}
				i++
			}
			if i >= n {
				return nil, fmt.Errorf("unterminated quoted string")
			}
			// Extract token content (between the quotes), preserving escape sequences
			tokens = append(tokens, input[start:i])
			i++ // skip closing quote
			continue
		}

		// Unquoted token: consume until whitespace
		start := i
		for i < n && input[i] != ' ' && input[i] != '\t' {
			i++
		}
		tokens = append(tokens, input[start:i])
	}

	return tokens, nil
}
