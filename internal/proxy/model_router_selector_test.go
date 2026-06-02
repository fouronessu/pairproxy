package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/l17728/pairproxy/internal/config"
)

type fakeModelRouterRedis struct {
	mu          sync.Mutex
	values      map[string]string
	expireCount map[string]int
}

func newFakeModelRouterRedis() *fakeModelRouterRedis {
	return &fakeModelRouterRedis{
		values:      make(map[string]string),
		expireCount: make(map[string]int),
	}
}

func (f *fakeModelRouterRedis) Get(_ context.Context, key string) *redis.StringCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	val, ok := f.values[key]
	if !ok {
		return redis.NewStringResult("", redis.Nil)
	}
	return redis.NewStringResult(val, nil)
}

func (f *fakeModelRouterRedis) Set(_ context.Context, key string, value interface{}, _ time.Duration) *redis.StatusCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch v := value.(type) {
	case []byte:
		f.values[key] = string(v)
	case string:
		f.values[key] = v
	default:
		data, _ := json.Marshal(v)
		f.values[key] = string(data)
	}
	return redis.NewStatusResult("OK", nil)
}

func (f *fakeModelRouterRedis) Expire(_ context.Context, key string, _ time.Duration) *redis.BoolCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.expireCount[key]++
	return redis.NewBoolResult(true, nil)
}

func (f *fakeModelRouterRedis) history(t *testing.T, sessionID string) []string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	var history []string
	require.NoError(t, json.Unmarshal([]byte(f.values[redisKey(sessionID)]), &history))
	return history
}

func (f *fakeModelRouterRedis) expires(sessionID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.expireCount[redisKey(sessionID)]
}

type fakeModelSelectorQuerier struct {
	scaleByName map[string]string
}

func (q fakeModelSelectorQuerier) ListMultimodalNames() ([]string, error) {
	return nil, nil
}

func (q fakeModelSelectorQuerier) ListNonMultimodalNames() ([]string, error) {
	return []string{"model-1", "model-2", "model-3", "model-4", "model-5", "model-big"}, nil
}

func (q fakeModelSelectorQuerier) GetScaleByName(modelName string) (string, error) {
	return q.scaleByName[modelName], nil
}

func newSequencedRouterSelector(t *testing.T, models []string, rdb modelRouterRedisClient, historyN int) (*ModelRouterSelector, func() int) {
	t.Helper()

	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		idx := calls - 1
		if idx >= len(models) {
			idx = len(models) - 1
		}
		model := models[idx]
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"model_rankings": []map[string]interface{}{
				{"rank": 1, "model": model, "score": 1.0},
			},
		})
	}))
	t.Cleanup(srv.Close)

	client := NewModelRouterClient(config.ModelRouterConfig{URL: srv.URL, Timeout: time.Second}, zaptest.NewLogger(t))
	selector := NewModelRouterSelector(client, rdb, fakeModelSelectorQuerier{
		scaleByName: map[string]string{"model-big": "big"},
	}, config.ModelRouterConfig{
		SessionHistoryN: historyN,
		Redis:           config.RedisConfig{TTL: time.Hour},
	}, zaptest.NewLogger(t))

	return selector, func() int {
		mu.Lock()
		defer mu.Unlock()
		return calls
	}
}

func TestModelRouterSelector_HistoryFullAfterNEntries(t *testing.T) {
	rdb := newFakeModelRouterRedis()
	selector, routeCalls := newSequencedRouterSelector(t,
		[]string{"model-1", "model-2", "model-3", "model-4", "model-5"},
		rdb,
		5,
	)

	body := []byte(`{"messages":[{"role":"user","content":"hello"}]}`)
	var result SelectModelResult
	for i := 0; i < 5; i++ {
		var err error
		result, err = selector.SelectModel(context.Background(), "req", "alice", "sess-n", body, "auto", []string{"model-1", "model-2", "model-3", "model-4", "model-5"}, nil)
		require.NoError(t, err)
		assert.Equal(t, 1, result.RouterResultStatus)
	}

	assert.Equal(t, 5, routeCalls())
	assert.Equal(t, []string{"model-1", "model-2", "model-3", "model-4", "model-5"}, rdb.history(t, "sess-n"))

	result, err := selector.SelectModel(context.Background(), "req", "alice", "sess-n", body, "auto", []string{"model-1", "model-2", "model-3", "model-4", "model-5"}, nil)
	require.NoError(t, err)
	assert.Equal(t, "model-5", result.Model)
	assert.Equal(t, 0, result.RouterResultStatus)
	assert.Equal(t, 1, result.CacheHitScene)
	assert.Equal(t, 5, routeCalls(), "history should be considered full only after N writes")
	assert.Equal(t, 1, rdb.expires("sess-n"), "full-history reuse should refresh TTL")
}

func TestModelRouterSelector_BigReuseRefreshesTTLWithoutAppending(t *testing.T) {
	rdb := newFakeModelRouterRedis()
	selector, routeCalls := newSequencedRouterSelector(t, []string{"model-big"}, rdb, 5)
	body := []byte(`{"messages":[{"role":"user","content":"hello"}]}`)

	result, err := selector.SelectModel(context.Background(), "req", "alice", "sess-big", body, "auto", []string{"model-big"}, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, result.RouterResultStatus)

	result, err = selector.SelectModel(context.Background(), "req", "alice", "sess-big", body, "auto", []string{"model-big"}, nil)
	require.NoError(t, err)
	assert.Equal(t, "model-big", result.Model)
	assert.Equal(t, 0, result.RouterResultStatus)
	assert.Equal(t, 2, result.CacheHitScene)
	assert.Equal(t, 1, routeCalls())
	assert.Equal(t, []string{"model-big"}, rdb.history(t, "sess-big"))
	assert.Equal(t, 1, rdb.expires("sess-big"), "big-model reuse should refresh TTL")
}
