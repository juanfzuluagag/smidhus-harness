package setup

import (
	"strings"
	"testing"
)

func TestFormatErrorLine(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Non-error short line",
			input:    "INFO: System is starting up",
			expected: "INFO: System is starting up",
		},
		{
			name:     "Non-error long line truncation",
			input:    strings.Repeat("A", 600),
			expected: strings.Repeat("A", 497) + "...",
		},
		{
			name:     "Error line flat json parsing success",
			input:    `[LOG] error={"name":"UnknownError","statusCode":500,"type":"UnexpectedError","message":"Server is overloaded"}`,
			expected: `[LOG] error={Name: UnknownError, StatusCode: 500, Type: UnexpectedError, Message: Server is overloaded}`,
		},
		{
			name:     "Error line nested json parsing success",
			input:    `[LOG] error={"error":{"name":"APIError","statusCode":429,"type":"RateLimit","message":"Too many requests"}}`,
			expected: `[LOG] error={Name: APIError, StatusCode: 429, Type: RateLimit, Message: Too many requests}`,
		},
		{
			name:     "Error line inner data nesting success",
			input:    `[LOG] error={"name":"QuotaExceeded","statusCode":403,"type":"","data":{"error":{"message":"Monthly budget exceeded","type":"QuotaError"}}}`,
			expected: `[LOG] error={Name: QuotaExceeded, StatusCode: 403, Type: QuotaError, Message: Monthly budget exceeded}`,
		},
		{
			name:     "Error line invalid json remains unchanged",
			input:    `[LOG] error={invalid-json-here}`,
			expected: `[LOG] error={invalid-json-here}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatErrorLine(tt.input)
			if got != tt.expected {
				t.Errorf("formatErrorLine() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedMsg   string
		expectedFound bool
	}{
		{
			name:          "Normal log line",
			input:         "INFO: Running setup scripts",
			expectedMsg:   "",
			expectedFound: false,
		},
		{
			name:          "Rate limit error code usage_limit_reached",
			input:         "ERROR: usage_limit_reached occurred",
			expectedMsg:   "API Quota/Rate Limit Exceeded. Process aborted to prevent hang.",
			expectedFound: true,
		},
		{
			name:          "Rate limit error code rate_limit_exceeded",
			input:         `{"level":"error", "message":"rate_limit_exceeded"}`,
			expectedMsg:   "API Quota/Rate Limit Exceeded. Process aborted to prevent hang.",
			expectedFound: true,
		},
		{
			name:          "Rate limit general text",
			input:         "Some error: quota_exceeded on this model",
			expectedMsg:   "API Quota/Rate Limit Exceeded. Process aborted to prevent hang.",
			expectedFound: true,
		},
		{
			name:          "Unexpected server error",
			input:         "ERROR: Unexpected server error. Check logs.",
			expectedMsg:   "Unexpected server error. Process aborted.",
			expectedFound: true,
		},
		{
			name:          "UnknownError log",
			input:         "Received UnknownError in api call",
			expectedMsg:   "Unexpected server error. Process aborted.",
			expectedFound: true,
		},
		{
			name:          "Context window limit",
			input:         "ERROR: context_length_exceeded, tokens too long",
			expectedMsg:   "Context window limit exceeded. Process aborted.",
			expectedFound: true,
		},
		{
			name:          "API Call error with message extraction",
			input:         `{"level":"error", "error": "api_apicallerror", "message":"model_not_found: The model does not exist"}`,
			expectedMsg:   "API Call Error: model_not_found: The model does not exist",
			expectedFound: true,
		},
		{
			name:          "General API error fallback",
			input:         "ERROR: API error occurred in runtime",
			expectedMsg:   "API Call Error. Process aborted to prevent hang.",
			expectedFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMsg, gotFound := classifyError(tt.input)
			if gotFound != tt.expectedFound {
				t.Fatalf("classifyError() found = %v, want %v", gotFound, tt.expectedFound)
			}
			if gotMsg != tt.expectedMsg {
				t.Errorf("classifyError() msg = %q, want %q", gotMsg, tt.expectedMsg)
			}
		})
	}
}
