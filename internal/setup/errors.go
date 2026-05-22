package setup

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// opencodeEvent is a minimal JSON event emitted by `opencode run --format json`.
// We decode only the fields we act on to stay resilient against schema changes.
type opencodeEvent struct {
	Type string `json:"type"`
	Part struct {
		Type  string `json:"type"`
		Text  string `json:"text"`
		Title string `json:"title"`
		// tool_call fields
		Tool  string `json:"tool"`
		State string `json:"state"`
		Input any    `json:"input"`
	} `json:"part"`
	Error struct {
		Data struct {
			Message string `json:"message"`
		} `json:"data"`
	} `json:"error"`
}

// ---------------------------------------------------------------------------
// parsedErrorInner extracts the structured fields from the deeply nested
// error objects opencode embeds in its log lines.
type parsedErrorInner struct {
	Name       string `json:"name"`
	StatusCode int    `json:"statusCode"`
	Message    string `json:"message"`
	Type       string `json:"type"`
	Data       struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	} `json:"data"`
}

// parsedErrorLog handles two layouts: a top-level error field and a flat
// structure where the error fields are at the root of the JSON object.
type parsedErrorLog struct {
	Error            *parsedErrorInner `json:"error"`
	parsedErrorInner                   // embedded
}

// formatErrorLine trims massive log lines (which contain full prompts) down
// to just the error metadata. Long lines without an error object are truncated
// at 500 chars to keep the terminal output readable.
func formatErrorLine(line string) string {
	idx := strings.Index(line, "error={")
	if idx == -1 {
		if len(line) > 500 {
			return line[:497] + "..."
		}
		return line
	}

	metadata := line[:idx]
	errPart := line[idx+6:]

	var parsed parsedErrorLog
	if json.Unmarshal([]byte(errPart), &parsed) == nil {
		inner := &parsed.parsedErrorInner
		if parsed.Error != nil {
			inner = parsed.Error
		}

		errMsg := inner.Message
		if inner.Data.Error.Message != "" {
			errMsg = inner.Data.Error.Message
		}
		errType := inner.Type
		if inner.Data.Error.Type != "" {
			errType = inner.Data.Error.Type
		}

		return fmt.Sprintf("%serror={Name: %s, StatusCode: %d, Type: %s, Message: %s}",
			metadata, inner.Name, inner.StatusCode, errType, errMsg)
	}

	if len(line) > 500 {
		return line[:497] + "..."
	}
	return line
}

// classifyError detects fatal API errors from a single stderr log line and
// returns a human-readable message. Using ToLower once avoids repeated
// case-insensitive comparisons on potentially large strings.
func classifyError(line string) (string, bool) {
	lower := strings.ToLower(line)

	isErrLog := strings.Contains(line, "ERROR") ||
		strings.Contains(line, "error=") ||
		strings.Contains(lower, "error:") ||
		strings.Contains(lower, "[error]") ||
		strings.Contains(lower, `"level":50`) ||
		strings.Contains(lower, `level:50`) ||
		strings.Contains(lower, `"level":"error"`) ||
		strings.Contains(lower, `"level":"fatal"`) ||
		strings.Contains(lower, "unknownerror") ||
		strings.Contains(lower, "apierror")

	if !isErrLog {
		return "", false
	}

	// Rate limit / Quota errors
	if strings.Contains(lower, "usage_limit_reached") ||
		strings.Contains(lower, "rate_limit_exceeded") ||
		strings.Contains(lower, "rate_limit") ||
		strings.Contains(lower, "quota_exceeded") ||
		strings.Contains(lower, "usage limit reached") ||
		strings.Contains(lower, "rate limit exceeded") {
		return "API Quota/Rate Limit Exceeded. Process aborted to prevent hang.", true
	}

	// Unexpected server error / UnknownError
	if strings.Contains(lower, "unexpected server error") ||
		strings.Contains(lower, "unknownerror") ||
		strings.Contains(lower, "unknown error") {
		return "Unexpected server error. Process aborted.", true
	}

	// Context window limit
	if strings.Contains(lower, "context_length_exceeded") ||
		strings.Contains(lower, "context length") ||
		strings.Contains(lower, "max_tokens") {
		return "Context window limit exceeded. Process aborted.", true
	}

	// AI Call errors general fallback
	if strings.Contains(lower, "ai_apicallerror") ||
		strings.Contains(lower, "api_apicallerror") ||
		strings.Contains(lower, "apierror") ||
		strings.Contains(lower, "api_error") {

		// Try to parse the inner message from the line.
		if idx := strings.Index(lower, "message\":\""); idx != -1 {
			msgStart := idx + 10
			if endIdx := strings.Index(lower[msgStart:], "\""); endIdx != -1 {
				msg := line[msgStart : msgStart+endIdx]
				if msg != "" {
					return fmt.Sprintf("API Call Error: %s", msg), true
				}
			}
		}
		return "API Call Error. Process aborted to prevent hang.", true
	}

	// Fallback error classification
	return "API Call Error. Process aborted to prevent hang.", true
}
