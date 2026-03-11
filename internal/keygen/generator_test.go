package keygen_test

import (
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l17728/pairproxy/internal/keygen"
)

func TestGenerateKey_Format(t *testing.T) {
	key, err := keygen.GenerateKey("alice")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(key, keygen.KeyPrefix), "must start with sk-pp-")
	assert.Equal(t, keygen.KeyTotalLen, len(key), "total length must be 54")
	body := key[len(keygen.KeyPrefix):]
	for _, c := range body {
		assert.True(t, unicode.IsLetter(c) || unicode.IsDigit(c),
			"body must be alphanumeric, got %q", c)
	}
}

func TestGenerateKey_ContainsUsernameChars(t *testing.T) {
	username := "alice"
	for i := 0; i < 20; i++ {
		key, err := keygen.GenerateKey(username)
		require.NoError(t, err)
		body := strings.ToLower(key[len(keygen.KeyPrefix):])
		need := map[rune]int{'a': 1, 'l': 1, 'i': 1, 'c': 1, 'e': 1}
		have := map[rune]int{}
		for _, c := range body {
			have[c]++
		}
		for ch, count := range need {
			assert.GreaterOrEqual(t, have[ch], count,
				"key body must contain username char %q at least %d times", ch, count)
		}
	}
}

func TestGenerateKey_DifferentEachTime(t *testing.T) {
	keys := make(map[string]bool)
	for i := 0; i < 100; i++ {
		k, err := keygen.GenerateKey("alice")
		require.NoError(t, err)
		keys[k] = true
	}
	assert.Greater(t, len(keys), 90, "keys should be unique across 100 generations")
}

func TestGenerateKey_EmptyUsername(t *testing.T) {
	_, err := keygen.GenerateKey("")
	assert.Error(t, err, "empty username must return error")
}

func TestGenerateKey_UsernameWithRepeatedChars(t *testing.T) {
	key, err := keygen.GenerateKey("aaab")
	require.NoError(t, err)
	body := strings.ToLower(key[len(keygen.KeyPrefix):])
	count := 0
	for _, c := range body {
		if c == 'a' {
			count++
		}
	}
	assert.GreaterOrEqual(t, count, 3, "body must contain at least 3 'a' chars for username 'aaab'")
}

func TestExtractAlphanumeric(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"alice", "alice"},
		{"Alice123", "alice123"},
		{"user-name_dot", "usernamedot"},
		{"", ""},
		{"---", ""},
	}
	for _, tc := range cases {
		result := keygen.ExtractAlphanumeric(tc.input)
		assert.Equal(t, tc.expected, string(result), "input=%q", tc.input)
	}
}
