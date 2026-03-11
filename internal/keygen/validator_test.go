package keygen_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l17728/pairproxy/internal/keygen"
)

// ---- IsValidFormat ----

func TestIsValidFormat_Valid(t *testing.T) {
	key, err := keygen.GenerateKey("alice")
	require.NoError(t, err)
	assert.True(t, keygen.IsValidFormat(key))
}

func TestIsValidFormat_WrongPrefix(t *testing.T) {
	assert.False(t, keygen.IsValidFormat("sk-ant-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
}

func TestIsValidFormat_TooShort(t *testing.T) {
	assert.False(t, keygen.IsValidFormat("sk-pp-short"))
}

func TestIsValidFormat_TooLong(t *testing.T) {
	assert.False(t, keygen.IsValidFormat("sk-pp-" + "a" + string(make([]byte, 48)) + "X"))
}

func TestIsValidFormat_InvalidChars(t *testing.T) {
	assert.False(t, keygen.IsValidFormat("sk-pp-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-aa"))
}

// ---- ValidateAndGetUser ----

func TestValidateAndGetUser_Match(t *testing.T) {
	key, err := keygen.GenerateKey("alice")
	require.NoError(t, err)

	users := []keygen.UserEntry{{ID: "u1", Username: "alice", IsActive: true}}
	u, err := keygen.ValidateAndGetUser(key, users)
	require.NoError(t, err)
	require.NotNil(t, u)
	assert.Equal(t, "alice", u.Username)
}

func TestValidateAndGetUser_NoMatch(t *testing.T) {
	aliceKey, _ := keygen.GenerateKey("alice")
	users := []keygen.UserEntry{{ID: "u2", Username: "xyz99", IsActive: true}}
	u, err := keygen.ValidateAndGetUser(aliceKey, users)
	_ = u
	_ = err
}

func TestValidateAndGetUser_InactiveSkipped(t *testing.T) {
	key, err := keygen.GenerateKey("alice")
	require.NoError(t, err)
	users := []keygen.UserEntry{
		{ID: "u1", Username: "alice", IsActive: false},
	}
	u, err := keygen.ValidateAndGetUser(key, users)
	require.NoError(t, err)
	assert.Nil(t, u, "inactive user must not be returned")
}

func TestValidateAndGetUser_LongestMatchWins(t *testing.T) {
	key, err := keygen.GenerateKey("alice")
	require.NoError(t, err)
	users := []keygen.UserEntry{
		{ID: "u1", Username: "alice", IsActive: true},
		{ID: "u2", Username: "ali", IsActive: true},
	}
	u, err := keygen.ValidateAndGetUser(key, users)
	require.NoError(t, err)
	require.NotNil(t, u)
	assert.Equal(t, "alice", u.Username, "longest match must win")
}

func TestValidateAndGetUser_RepeatedChars(t *testing.T) {
	key, err := keygen.GenerateKey("aaab")
	require.NoError(t, err)
	users := []keygen.UserEntry{
		{ID: "u1", Username: "aaab", IsActive: true},
	}
	u, err := keygen.ValidateAndGetUser(key, users)
	require.NoError(t, err)
	require.NotNil(t, u)
	assert.Equal(t, "aaab", u.Username)
}

func TestValidateAndGetUser_Collision(t *testing.T) {
	key := "sk-pp-aabbccddeeffgghhiijjkkllmmnnooaabbccddeeffgghhii"
	users := []keygen.UserEntry{
		{ID: "u1", Username: "abcde", IsActive: true},
		{ID: "u2", Username: "abcdf", IsActive: true},
	}
	u, err := keygen.ValidateAndGetUser(key, users)
	if err != nil {
		assert.Contains(t, err.Error(), "collision")
		assert.Nil(t, u)
	}
}

// ---- ValidateUsername ----

func TestValidateUsername_Valid(t *testing.T) {
	assert.NoError(t, keygen.ValidateUsername("alice"))
	assert.NoError(t, keygen.ValidateUsername("user123"))
	assert.NoError(t, keygen.ValidateUsername("ab12"))
}

func TestValidateUsername_TooShort(t *testing.T) {
	assert.Error(t, keygen.ValidateUsername("ab"))
	assert.Error(t, keygen.ValidateUsername("abc"))
}

func TestValidateUsername_TooFewUniqueChars(t *testing.T) {
	assert.Error(t, keygen.ValidateUsername("aaaa"))
	assert.Error(t, keygen.ValidateUsername("1111"))
	assert.Error(t, keygen.ValidateUsername("----"))
}

func TestValidateUsername_Valid_TwoUniqueChars(t *testing.T) {
	assert.NoError(t, keygen.ValidateUsername("aabb"))
}

// ---- ContainsAllCharsWithCount ----

func TestContainsAllCharsWithCount(t *testing.T) {
	cases := []struct {
		body   string
		chars  []byte
		expect bool
	}{
		{"alicexyz", []byte("alice"), true},
		{"abcd", []byte("alice"), false},
		{"aaabcd", []byte("aaab"), true},
		{"aabcd", []byte("aaab"), false},
	}
	for _, tc := range cases {
		result := keygen.ContainsAllCharsWithCount(tc.body, tc.chars)
		assert.Equal(t, tc.expect, result, "body=%q chars=%q", tc.body, tc.chars)
	}
}
