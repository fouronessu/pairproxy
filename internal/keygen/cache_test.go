package keygen_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l17728/pairproxy/internal/keygen"
)

func TestKeyCache_SetAndGet(t *testing.T) {
	cache, err := keygen.NewKeyCache(100, time.Minute)
	require.NoError(t, err)

	key := "sk-pp-testkey123"
	user := &keygen.CachedUser{UserID: "u1", Username: "alice"}
	cache.Set(key, user)

	got := cache.Get(key)
	require.NotNil(t, got)
	assert.Equal(t, "u1", got.UserID)
	assert.Equal(t, "alice", got.Username)
}

func TestKeyCache_Miss(t *testing.T) {
	cache, err := keygen.NewKeyCache(100, time.Minute)
	require.NoError(t, err)
	assert.Nil(t, cache.Get("nonexistent"))
}

func TestKeyCache_TTLExpiry(t *testing.T) {
	cache, err := keygen.NewKeyCache(100, 50*time.Millisecond)
	require.NoError(t, err)

	cache.Set("k", &keygen.CachedUser{UserID: "u1", Username: "alice"})
	assert.NotNil(t, cache.Get("k"), "should hit before TTL")

	time.Sleep(80 * time.Millisecond)
	assert.Nil(t, cache.Get("k"), "should miss after TTL")
}

func TestKeyCache_InvalidateUser(t *testing.T) {
	cache, err := keygen.NewKeyCache(100, time.Minute)
	require.NoError(t, err)

	cache.Set("k1", &keygen.CachedUser{UserID: "u1", Username: "alice"})
	cache.Set("k2", &keygen.CachedUser{UserID: "u1", Username: "alice"})
	cache.Set("k3", &keygen.CachedUser{UserID: "u2", Username: "bob"})

	cache.InvalidateUser("alice")

	assert.Nil(t, cache.Get("k1"), "alice key k1 should be evicted")
	assert.Nil(t, cache.Get("k2"), "alice key k2 should be evicted")
	assert.NotNil(t, cache.Get("k3"), "bob key should remain")
}

func TestKeyCache_SizeLimit(t *testing.T) {
	cache, err := keygen.NewKeyCache(2, time.Minute)
	require.NoError(t, err)

	cache.Set("k1", &keygen.CachedUser{UserID: "u1", Username: "a"})
	cache.Set("k2", &keygen.CachedUser{UserID: "u2", Username: "b"})
	cache.Set("k3", &keygen.CachedUser{UserID: "u3", Username: "c"})

	assert.Nil(t, cache.Get("k1"), "oldest entry should be evicted when size exceeded")
	assert.NotNil(t, cache.Get("k2"))
	assert.NotNil(t, cache.Get("k3"))
}
