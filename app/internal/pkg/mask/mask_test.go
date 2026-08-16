package mask

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMaskJSONBody(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected map[string]any
	}{
		{
			name:  "plain json with password and token",
			input: `{"username":"admin","password":"secretpassword","token":"jwt-token-123","email":"admin@example.com"}`,
			expected: map[string]any{
				"username": "admin",
				"password": "***",
				"token":    "***",
				"email":    "admin@example.com",
			},
		},
		{
			name:  "nested json object and array",
			input: `{"user":{"oldPassword":"123","new_password":"456","subTokens":["a","b"]},"secret_key":"xyz"}`,
			expected: map[string]any{
				"user": map[string]any{
					"oldPassword":  "***",
					"new_password": "***",
					"subTokens":    []any{"a", "b"},
				},
				"secret_key": "***",
			},
		},
		{
			name:  "case insensitivity and variations",
			input: `{"PASSWORD":"123","accessToken":"token1","RefreshToken":"token2","ClientSecret":"s"}`,
			expected: map[string]any{
				"PASSWORD":     "***",
				"accessToken":  "***",
				"RefreshToken": "***",
				"ClientSecret": "***",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := MaskJSONBody([]byte(tt.input), tt.maxLen)
			var parsed map[string]any
			if err := json.Unmarshal([]byte(res), &parsed); err != nil {
				t.Fatalf("failed to parse result json: %v, raw: %s", err, res)
			}

			// check password masked
			if tt.expected["password"] != nil && parsed["password"] != "***" {
				t.Errorf("password not masked: %v", parsed["password"])
			}
			if tt.expected["PASSWORD"] != nil && parsed["PASSWORD"] != "***" {
				t.Errorf("PASSWORD not masked: %v", parsed["PASSWORD"])
			}
			if tt.expected["token"] != nil && parsed["token"] != "***" {
				t.Errorf("token not masked: %v", parsed["token"])
			}
			if tt.expected["secret_key"] != nil && parsed["secret_key"] != "***" {
				t.Errorf("secret_key not masked: %v", parsed["secret_key"])
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	s := "hello world from golang"
	if got := Truncate(s, 10); got != "hello w..." {
		t.Errorf("Truncate(10) = %q, want %q", got, "hello w...")
	}
	if got := Truncate(s, 50); got != s {
		t.Errorf("Truncate(50) = %q, want %q", got, s)
	}
	if got := Truncate(s, 2); got != "he" {
		t.Errorf("Truncate(2) = %q, want %q", got, "he")
	}
}

func TestNonJSON(t *testing.T) {
	raw := "plain text body not json"
	got := MaskJSONBody([]byte(raw), 10)
	if got != OmittedBody {
		t.Errorf("MaskJSONBody non-json = %q, want %q", got, OmittedBody)
	}
}

func TestSensitiveFallbacksDoNotLeakSecrets(t *testing.T) {
	for _, input := range []string{
		`{"username":"alice","password":"secret"`,
		"username=alice&password=secret",
	} {
		t.Run(input, func(t *testing.T) {
			got := MaskJSONBody([]byte(input), DefaultMaxBodyLen)
			if got != OmittedBody {
				t.Fatalf("MaskJSONBody fallback = %q, want %q", got, OmittedBody)
			}
			if strings.Contains(got, "secret") {
				t.Fatalf("fallback leaked secret: %q", got)
			}
		})
	}
}

func TestMaskQuery(t *testing.T) {
	got := MaskQuery("username=alice&token=secret&scope=read", DefaultMaxBodyLen)
	if strings.Contains(got, "secret") {
		t.Fatalf("MaskQuery leaked token: %q", got)
	}
	if !strings.Contains(got, "token=%2A%2A%2A") {
		t.Fatalf("MaskQuery did not mask token: %q", got)
	}
	if !strings.Contains(got, "username=alice") || !strings.Contains(got, "scope=read") {
		t.Fatalf("MaskQuery changed non-sensitive values: %q", got)
	}

	if got := MaskQuery("password=secret%ZZ", DefaultMaxBodyLen); got != OmittedQuery {
		t.Fatalf("invalid query = %q, want %q", got, OmittedQuery)
	}
}
