package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/l17728/pairproxy/internal/db"
)

// TestResolveUser_SingleUser_Returns verifies that resolveUser returns the user
// when exactly one user with the given username exists.
func TestResolveUser_SingleUser_Returns(t *testing.T) {
	logger := zap.NewNop()
	gormDB, err := db.Open(logger, ":memory:")
	require.NoError(t, err)
	require.NoError(t, db.Migrate(logger, gormDB))
	repo := db.NewUserRepo(gormDB, logger)

	require.NoError(t, repo.Create(&db.User{Username: "alice", PasswordHash: "h1", AuthProvider: "local"}))

	u, err := resolveUser(repo, "alice")
	require.NoError(t, err)
	assert.NotNil(t, u)
	assert.Equal(t, "alice", u.Username)
}

// TestResolveUser_NoUser_ReturnsError verifies that resolveUser returns an error
// when no user with the given username is found.
func TestResolveUser_NoUser_ReturnsError(t *testing.T) {
	logger := zap.NewNop()
	gormDB, err := db.Open(logger, ":memory:")
	require.NoError(t, err)
	require.NoError(t, db.Migrate(logger, gormDB))
	repo := db.NewUserRepo(gormDB, logger)

	u, err := resolveUser(repo, "nobody")
	require.Error(t, err)
	assert.Nil(t, u)
	assert.Contains(t, err.Error(), "not found")
}

// TestResolveUser_Ambiguous_ReturnsError verifies that when two users share the
// same username (different auth providers), resolveUser returns an error advising
// the user to use provider disambiguation or user ID.
func TestResolveUser_Ambiguous_ReturnsError(t *testing.T) {
	logger := zap.NewNop()
	gormDB, err := db.Open(logger, ":memory:")
	require.NoError(t, err)
	require.NoError(t, db.Migrate(logger, gormDB))
	repo := db.NewUserRepo(gormDB, logger)

	extID := "ldap-alice"
	require.NoError(t, repo.Create(&db.User{Username: "alice", PasswordHash: "h1", AuthProvider: "local"}))
	require.NoError(t, repo.Create(&db.User{Username: "alice", PasswordHash: "", AuthProvider: "ldap", ExternalID: &extID}))

	u, err := resolveUser(repo, "alice")
	require.Error(t, err)
	assert.Nil(t, u)
	assert.Contains(t, err.Error(), "matches 2 users")
	assert.Contains(t, err.Error(), "different auth providers")
}
