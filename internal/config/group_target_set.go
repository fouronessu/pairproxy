package config

import (
	"fmt"
	"time"
)

// GroupTargetSetConfig Group Target Set 配置
type GroupTargetSetConfig struct {
	Name        string                    `yaml:"name"`
	GroupName   string                    `yaml:"group_name"`
	GroupID     string                    `yaml:"group_id"`
	Strategy    string                    `yaml:"strategy"`
	RetryPolicy string                    `yaml:"retry_policy"`
	IsDefault   bool                      `yaml:"is_default"`
	Targets     []GroupTargetSetMemberConfig `yaml:"targets"`
}

// GroupTargetSetMemberConfig Target Set 成员配置
type GroupTargetSetMemberConfig struct {
	URL      string `yaml:"url"`
	Weight   int    `yaml:"weight"`
	Priority int    `yaml:"priority"`
}

// TargetAlertTriggerConfig 告警触发条件配置
type TargetAlertTriggerConfig struct {
	Type           string        `yaml:"type"`
	StatusCodes    []int         `yaml:"status_codes"`
	Severity       string        `yaml:"severity"`
	MinOccurrences int           `yaml:"min_occurrences"`
	Window         time.Duration `yaml:"window"`
}

// TargetAlertRecoveryConfig 告警恢复条件配置
type TargetAlertRecoveryConfig struct {
	ConsecutiveSuccesses int           `yaml:"consecutive_successes"`
	Window               time.Duration `yaml:"window"`
}

// TargetAlertDashboardConfig Dashboard 配置
type TargetAlertDashboardConfig struct {
	MaxActiveAlerts int           `yaml:"max_active_alerts"`
	Retention       time.Duration `yaml:"retention"`
	AutoRefresh     bool          `yaml:"auto_refresh"`
}

// TargetAlertConfig 告警配置
type TargetAlertConfig struct {
	Enabled   bool                              `yaml:"enabled"`
	Triggers  map[string]TargetAlertTriggerConfig `yaml:"triggers"`
	Recovery  TargetAlertRecoveryConfig         `yaml:"recovery"`
	Dashboard TargetAlertDashboardConfig        `yaml:"dashboard"`
}

// HealthCheckConfig 健康检查配置
type HealthCheckConfig struct {
	Interval         time.Duration `yaml:"interval"`
	Timeout          time.Duration `yaml:"timeout"`
	FailureThreshold int           `yaml:"failure_threshold"`
	SuccessThreshold int           `yaml:"success_threshold"`
	Path             string        `yaml:"path"`
}

// GroupTargetSetFullConfig 完整的 Group Target Set 配置
type GroupTargetSetFullConfig struct {
	GroupTargetSets []GroupTargetSetConfig `yaml:"group_target_sets"`
	TargetAlerts    TargetAlertConfig      `yaml:"target_alerts"`
	HealthCheck     HealthCheckConfig      `yaml:"health_check"`
}

// ValidateGroupTargetSetConfig 验证 Group Target Set 配置
func ValidateGroupTargetSetConfig(cfg *GroupTargetSetConfig) error {
	if cfg.Name == "" {
		return fmt.Errorf("group target set name cannot be empty")
	}

	if cfg.Strategy == "" {
		cfg.Strategy = "weighted_random"
	}

	if cfg.Strategy != "weighted_random" && cfg.Strategy != "round_robin" && cfg.Strategy != "priority" {
		return fmt.Errorf("invalid strategy: %s", cfg.Strategy)
	}

	if cfg.RetryPolicy == "" {
		cfg.RetryPolicy = "try_next"
	}

	if cfg.RetryPolicy != "try_next" && cfg.RetryPolicy != "fail_fast" {
		return fmt.Errorf("invalid retry policy: %s", cfg.RetryPolicy)
	}

	if len(cfg.Targets) == 0 {
		return fmt.Errorf("group target set must have at least one target")
	}

	for i, t := range cfg.Targets {
		if t.URL == "" {
			return fmt.Errorf("target %d URL cannot be empty", i)
		}
		if t.Weight <= 0 {
			t.Weight = 1
		}
	}

	return nil
}

// ValidateTargetAlertConfig 验证告警配置
func ValidateTargetAlertConfig(cfg *TargetAlertConfig) error {
	if !cfg.Enabled {
		return nil
	}

	if cfg.Recovery.ConsecutiveSuccesses <= 0 {
		cfg.Recovery.ConsecutiveSuccesses = 2
	}

	if cfg.Recovery.Window <= 0 {
		cfg.Recovery.Window = 5 * time.Minute
	}

	if cfg.Dashboard.MaxActiveAlerts <= 0 {
		cfg.Dashboard.MaxActiveAlerts = 50
	}

	if cfg.Dashboard.Retention <= 0 {
		cfg.Dashboard.Retention = 7 * 24 * time.Hour
	}

	return nil
}

// ValidateHealthCheckConfig 验证健康检查配置
func ValidateHealthCheckConfig(cfg *HealthCheckConfig) error {
	if cfg.Interval <= 0 {
		cfg.Interval = 30 * time.Second
	}

	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}

	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 3
	}

	if cfg.SuccessThreshold <= 0 {
		cfg.SuccessThreshold = 2
	}

	if cfg.Path == "" {
		cfg.Path = "/health"
	}

	return nil
}
