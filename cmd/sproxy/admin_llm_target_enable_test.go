package main

import (
	"testing"

	"github.com/l17728/pairproxy/internal/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestLLMTargetEnable(t *testing.T) {
	logger := zaptest.NewLogger(t)
	gormDB, err := db.Open(logger, ":memory:")
	require.NoError(t, err)
	defer closeGormDB(logger, gormDB)

	err = db.Migrate(logger, gormDB)
	require.NoError(t, err)

	repo := db.NewLLMTargetRepo(gormDB, logger)

	// 创建一个禁用的 target
	target := &db.LLMTarget{
		ID:         "test-enable-id",
		URL:        "http://test-enable.local:11434",
		Provider:   "ollama",
		Name:       "Test Enable",
		Weight:     1,
		Source:     "database",
		IsEditable: true,
		IsActive:   false, // 初始状态为禁用
	}

	// 使用 Upsert 创建（支持 boolean false）
	err = repo.Upsert(target)
	require.NoError(t, err)

	// 验证初始状态
	fetched, err := repo.GetByURL(target.URL)
	require.NoError(t, err)
	assert.False(t, fetched.IsActive, "target should be initially disabled")

	// 启用 target
	err = gormDB.Model(&db.LLMTarget{}).Where("id = ?", target.ID).
		Updates(map[string]interface{}{
			"is_active": true,
		}).Error
	require.NoError(t, err)

	// 验证已启用
	fetched, err = repo.GetByURL(target.URL)
	require.NoError(t, err)
	assert.True(t, fetched.IsActive, "target should be enabled")
}

func TestLLMTargetDisable(t *testing.T) {
	logger := zaptest.NewLogger(t)
	gormDB, err := db.Open(logger, ":memory:")
	require.NoError(t, err)
	defer closeGormDB(logger, gormDB)

	err = db.Migrate(logger, gormDB)
	require.NoError(t, err)

	repo := db.NewLLMTargetRepo(gormDB, logger)

	// 创建一个启用的 target
	target := &db.LLMTarget{
		ID:         "test-disable-id",
		URL:        "http://test-disable.local:11434",
		Provider:   "ollama",
		Name:       "Test Disable",
		Weight:     1,
		Source:     "database",
		IsEditable: true,
		IsActive:   true, // 初始状态为启用
	}

	err = repo.Create(target)
	require.NoError(t, err)

	// 验证初始状态
	fetched, err := repo.GetByURL(target.URL)
	require.NoError(t, err)
	assert.True(t, fetched.IsActive, "target should be initially enabled")

	// 禁用 target
	err = gormDB.Model(&db.LLMTarget{}).Where("id = ?", target.ID).
		Updates(map[string]interface{}{
			"is_active": false,
		}).Error
	require.NoError(t, err)

	// 验证已禁用
	fetched, err = repo.GetByURL(target.URL)
	require.NoError(t, err)
	assert.False(t, fetched.IsActive, "target should be disabled")
}

func TestLLMTargetEnableIdempotent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	gormDB, err := db.Open(logger, ":memory:")
	require.NoError(t, err)
	defer closeGormDB(logger, gormDB)

	err = db.Migrate(logger, gormDB)
	require.NoError(t, err)

	repo := db.NewLLMTargetRepo(gormDB, logger)

	// 创建一个已启用的 target
	target := &db.LLMTarget{
		ID:         "test-enable-idempotent-id",
		URL:        "http://test-enable-idempotent.local:11434",
		Provider:   "ollama",
		Name:       "Test Enable Idempotent",
		Weight:     1,
		Source:     "database",
		IsEditable: true,
		IsActive:   true,
	}

	err = repo.Create(target)
	require.NoError(t, err)

	// 再次启用（幂等操作）
	err = gormDB.Model(&db.LLMTarget{}).Where("id = ?", target.ID).
		Updates(map[string]interface{}{
			"is_active": true,
		}).Error
	require.NoError(t, err)

	// 验证仍然启用
	fetched, err := repo.GetByURL(target.URL)
	require.NoError(t, err)
	assert.True(t, fetched.IsActive, "target should still be enabled")
}

func TestLLMTargetDisableIdempotent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	gormDB, err := db.Open(logger, ":memory:")
	require.NoError(t, err)
	defer closeGormDB(logger, gormDB)

	err = db.Migrate(logger, gormDB)
	require.NoError(t, err)

	repo := db.NewLLMTargetRepo(gormDB, logger)

	// 创建一个禁用的 target
	target := &db.LLMTarget{
		ID:         "test-disable-idempotent-id",
		URL:        "http://test-disable-idempotent.local:11434",
		Provider:   "ollama",
		Name:       "Test Disable Idempotent",
		Weight:     1,
		Source:     "database",
		IsEditable: true,
		IsActive:   false,
	}

	// 使用 Upsert 创建（支持 boolean false）
	err = repo.Upsert(target)
	require.NoError(t, err)

	// 再次禁用（幂等操作）
	err = gormDB.Model(&db.LLMTarget{}).Where("id = ?", target.ID).
		Updates(map[string]interface{}{
			"is_active": false,
		}).Error
	require.NoError(t, err)

	// 验证仍然禁用
	fetched, err := repo.GetByURL(target.URL)
	require.NoError(t, err)
	assert.False(t, fetched.IsActive, "target should still be disabled")
}
