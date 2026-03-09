package proxy

import (
	"testing"

	"github.com/l17728/pairproxy/internal/config"
	"github.com/l17728/pairproxy/internal/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestSyncConfigTargetsToDatabase(t *testing.T) {
	logger := zap.NewNop()
	gormDB, err := db.Open(logger, ":memory:")
	require.NoError(t, err)
	require.NoError(t, db.Migrate(logger, gormDB))

	// Create SProxy with config
	cfg := &config.SProxyFullConfig{
		LLM: config.LLMConfig{
			Targets: []config.LLMTarget{
				{
					URL:      "http://test1.local",
					APIKey:   "key1",
					Provider: "anthropic",
					Name:     "Test 1",
					Weight:   1,
				},
				{
					URL:      "http://test2.local",
					APIKey:   "key2",
					Provider: "openai",
					Name:     "Test 2",
					Weight:   2,
				},
			},
		},
	}

	sp := &SProxy{
		cfg:    cfg,
		db:     gormDB,
		logger: logger,
	}

	repo := db.NewLLMTargetRepo(gormDB, logger)

	// Sync
	err = sp.syncConfigTargetsToDatabase(repo)
	require.NoError(t, err)

	// Verify targets were synced
	targets, err := repo.ListAll()
	require.NoError(t, err)
	assert.Len(t, targets, 2)

	// Verify properties
	for _, target := range targets {
		assert.Equal(t, "config", target.Source)
		assert.False(t, target.IsEditable)
		assert.True(t, target.IsActive)
	}
}

func TestSyncConfigTargets_Cleanup(t *testing.T) {
	logger := zap.NewNop()
	gormDB, err := db.Open(logger, ":memory:")
	require.NoError(t, err)
	require.NoError(t, db.Migrate(logger, gormDB))

	repo := db.NewLLMTargetRepo(gormDB, logger)

	// Create existing config target
	oldTarget := &db.LLMTarget{
		ID:         "old-id",
		URL:        "http://old.local",
		Source:     "config",
		IsEditable: false,
	}
	err = repo.Create(oldTarget)
	require.NoError(t, err)

	// Sync with new config (old target removed)
	cfg := &config.SProxyFullConfig{
		LLM: config.LLMConfig{
			Targets: []config.LLMTarget{
				{
					URL:      "http://new.local",
					APIKey:   "key",
					Provider: "anthropic",
					Name:     "New",
					Weight:   1,
				},
			},
		},
	}

	sp := &SProxy{
		cfg:    cfg,
		db:     gormDB,
		logger: logger,
	}

	err = sp.syncConfigTargetsToDatabase(repo)
	require.NoError(t, err)

	// Verify old target was deleted
	_, err = repo.GetByURL("http://old.local")
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)

	// Verify new target exists
	_, err = repo.GetByURL("http://new.local")
	assert.NoError(t, err)
}
