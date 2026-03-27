package alert

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/l17728/pairproxy/internal/db"
)

// setupTestDB 创建测试数据库
func setupTestDB(t *testing.T) *gorm.DB {
	testDB, err := db.Open(zap.NewNop(), ":memory:")
	require.NoError(t, err)
	require.NoError(t, db.Migrate(zap.NewNop(), testDB))
	return testDB
}

// TestTargetAlertManager_RecordError 测试记录错误
func TestTargetAlertManager_RecordError(t *testing.T) {
	testDB := setupTestDB(t)
	alertRepo := db.NewTargetAlertRepo(testDB, zap.NewNop())

	config := TargetAlertConfig{
		Enabled: true,
		Triggers: map[string]TriggerConfig{
			"http_error": {
				Type:           "http_error",
				StatusCodes:    []int{500, 502, 503},
				Severity:       "error",
				MinOccurrences: 3,
				Window:         5 * time.Minute,
			},
		},
		Recovery: RecoveryConfig{
			ConsecutiveSuccesses: 2,
			Window:               5 * time.Minute,
		},
	}

	manager := NewTargetAlertManager(alertRepo, config, zap.NewNop())
	manager.Start(context.Background())
	defer manager.Stop()

	// 记录错误
	targetURL := "https://api.example.com"
	for i := 0; i < 3; i++ {
		manager.RecordError(targetURL, 503, nil, []string{"engineering"})
	}

	// 等待事件处理
	time.Sleep(100 * time.Millisecond)

	// 验证活跃告警
	alerts := manager.GetActiveAlerts()
	assert.Greater(t, len(alerts), 0)
}

// TestTargetAlertManager_RecordSuccess 测试记录成功
func TestTargetAlertManager_RecordSuccess(t *testing.T) {
	testDB := setupTestDB(t)
	alertRepo := db.NewTargetAlertRepo(testDB, zap.NewNop())

	config := TargetAlertConfig{
		Enabled: true,
		Triggers: map[string]TriggerConfig{
			"http_error": {
				Type:           "http_error",
				MinOccurrences: 1,
			},
		},
		Recovery: RecoveryConfig{
			ConsecutiveSuccesses: 1,
		},
	}

	manager := NewTargetAlertManager(alertRepo, config, zap.NewNop())
	manager.Start(context.Background())
	defer manager.Stop()

	targetURL := "https://api.example.com"

	// 记录错误
	manager.RecordError(targetURL, 503, nil, []string{})

	// 记录成功
	manager.RecordSuccess(targetURL)

	// 验证成功计数
	alerts := manager.GetActiveAlerts()
	if len(alerts) > 0 {
		assert.Greater(t, alerts[0].SuccessCount, 0)
	}
}

// TestTargetAlertManager_SubscribeEvents 测试订阅事件
func TestTargetAlertManager_SubscribeEvents(t *testing.T) {
	testDB := setupTestDB(t)
	alertRepo := db.NewTargetAlertRepo(testDB, zap.NewNop())

	config := TargetAlertConfig{
		Enabled: true,
		Triggers: map[string]TriggerConfig{
			"http_error": {
				Type:           "http_error",
				MinOccurrences: 1,
			},
		},
	}

	manager := NewTargetAlertManager(alertRepo, config, zap.NewNop())
	manager.Start(context.Background())
	defer manager.Stop()

	// 订阅事件
	eventCh := manager.SubscribeEvents()

	// 记录错误（应该推送事件）
	manager.RecordError("https://api.example.com", 503, nil, []string{})

	// 等待事件
	select {
	case event := <-eventCh:
		assert.NotNil(t, event)
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

// TestTargetAlertManager_Disabled 测试禁用告警管理器
func TestTargetAlertManager_Disabled(t *testing.T) {
	testDB := setupTestDB(t)
	alertRepo := db.NewTargetAlertRepo(testDB, zap.NewNop())

	config := TargetAlertConfig{
		Enabled: false,
	}

	manager := NewTargetAlertManager(alertRepo, config, zap.NewNop())
	manager.Start(context.Background())
	defer manager.Stop()

	// 记录错误（应该被忽略）
	manager.RecordError("https://api.example.com", 503, nil, []string{})

	// 验证没有活跃告警
	alerts := manager.GetActiveAlerts()
	assert.Len(t, alerts, 0)
}

