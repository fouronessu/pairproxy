package db

import (
	"fmt"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// LLMTargetRepo LLM Target 数据库操作
type LLMTargetRepo struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewLLMTargetRepo 创建 LLMTargetRepo
func NewLLMTargetRepo(db *gorm.DB, logger *zap.Logger) *LLMTargetRepo {
	return &LLMTargetRepo{
		db:     db,
		logger: logger.Named("llmtarget_repo"),
	}
}

// Create 创建新的 LLM target
func (r *LLMTargetRepo) Create(target *LLMTarget) error {
	if err := r.db.Create(target).Error; err != nil {
		r.logger.Error("failed to create llm target",
			zap.String("url", target.URL),
			zap.Error(err))
		return fmt.Errorf("create llm target: %w", err)
	}

	r.logger.Info("llm target created",
		zap.String("id", target.ID),
		zap.String("url", target.URL),
		zap.String("source", target.Source))

	return nil
}

// GetByURL 根据 URL 查询 LLM target
func (r *LLMTargetRepo) GetByURL(url string) (*LLMTarget, error) {
	var target LLMTarget
	if err := r.db.Where("url = ?", url).First(&target).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, err
		}
		r.logger.Error("failed to get llm target by url",
			zap.String("url", url),
			zap.Error(err))
		return nil, fmt.Errorf("get llm target by url: %w", err)
	}
	return &target, nil
}

// GetByID 根据 ID 查询 LLM target
func (r *LLMTargetRepo) GetByID(id string) (*LLMTarget, error) {
	var target LLMTarget
	if err := r.db.Where("id = ?", id).First(&target).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, err
		}
		r.logger.Error("failed to get llm target by id",
			zap.String("id", id),
			zap.Error(err))
		return nil, fmt.Errorf("get llm target by id: %w", err)
	}
	return &target, nil
}

// ListAll 列出所有 LLM targets
func (r *LLMTargetRepo) ListAll() ([]*LLMTarget, error) {
	var targets []*LLMTarget
	if err := r.db.Order("created_at DESC").Find(&targets).Error; err != nil {
		r.logger.Error("failed to list llm targets", zap.Error(err))
		return nil, fmt.Errorf("list llm targets: %w", err)
	}

	r.logger.Debug("listed llm targets", zap.Int("count", len(targets)))
	return targets, nil
}

// URLExists 检查 URL 是否已存在
func (r *LLMTargetRepo) URLExists(url string) (bool, error) {
	var count int64
	if err := r.db.Model(&LLMTarget{}).Where("url = ?", url).Count(&count).Error; err != nil {
		r.logger.Error("failed to check url exists",
			zap.String("url", url),
			zap.Error(err))
		return false, fmt.Errorf("check url exists: %w", err)
	}
	return count > 0, nil
}

// Update 更新 LLM target（仅可编辑的）
func (r *LLMTargetRepo) Update(target *LLMTarget) error {
	// 检查是否可编辑
	if !target.IsEditable {
		return fmt.Errorf("target is not editable (config-sourced)")
	}

	if err := r.db.Save(target).Error; err != nil {
		r.logger.Error("failed to update llm target",
			zap.String("id", target.ID),
			zap.String("url", target.URL),
			zap.Error(err))
		return fmt.Errorf("update llm target: %w", err)
	}

	r.logger.Info("llm target updated",
		zap.String("id", target.ID),
		zap.String("url", target.URL))

	return nil
}

// Delete 删除 LLM target（仅可编辑的）
func (r *LLMTargetRepo) Delete(id string) error {
	// 先查询检查是否可编辑
	target, err := r.GetByID(id)
	if err != nil {
		return err
	}

	if !target.IsEditable {
		return fmt.Errorf("target is not editable (config-sourced)")
	}

	if err := r.db.Delete(&LLMTarget{}, "id = ?", id).Error; err != nil {
		r.logger.Error("failed to delete llm target",
			zap.String("id", id),
			zap.Error(err))
		return fmt.Errorf("delete llm target: %w", err)
	}

	r.logger.Info("llm target deleted",
		zap.String("id", id),
		zap.String("url", target.URL))

	return nil
}

// Upsert 插入或更新 LLM target（用于配置文件同步）
func (r *LLMTargetRepo) Upsert(target *LLMTarget) error {
	// 检查是否存在
	existing, err := r.GetByURL(target.URL)
	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}

	if existing != nil {
		// 更新：保留 ID，更新其他字段
		target.ID = existing.ID
		if err := r.db.Save(target).Error; err != nil {
			r.logger.Error("failed to upsert (update) llm target",
				zap.String("url", target.URL),
				zap.Error(err))
			return fmt.Errorf("upsert llm target: %w", err)
		}
		r.logger.Debug("llm target upserted (updated)",
			zap.String("url", target.URL))
	} else {
		// 插入：生成新 ID
		if target.ID == "" {
			target.ID = uuid.NewString()
		}
		if err := r.db.Create(target).Error; err != nil {
			r.logger.Error("failed to upsert (insert) llm target",
				zap.String("url", target.URL),
				zap.Error(err))
			return fmt.Errorf("upsert llm target: %w", err)
		}
		r.logger.Debug("llm target upserted (inserted)",
			zap.String("url", target.URL))
	}

	return nil
}

// DeleteConfigTargetsNotInList 删除不在列表中的配置文件来源的 targets
// 用于配置文件同步时清理已移除的 targets
func (r *LLMTargetRepo) DeleteConfigTargetsNotInList(keepURLs []string) (int, error) {
	query := r.db.Where("source = ?", "config")

	// 如果 keepURLs 不为空，添加 NOT IN 条件
	if len(keepURLs) > 0 {
		query = query.Where("url NOT IN ?", keepURLs)
	}

	result := query.Delete(&LLMTarget{})
	if result.Error != nil {
		r.logger.Error("failed to delete config targets not in list",
			zap.Int("keep_count", len(keepURLs)),
			zap.Error(result.Error))
		return 0, fmt.Errorf("delete config targets not in list: %w", result.Error)
	}

	deleted := int(result.RowsAffected)
	if deleted > 0 {
		r.logger.Info("deleted config targets not in list",
			zap.Int("deleted", deleted),
			zap.Int("kept", len(keepURLs)))
	}

	return deleted, nil
}
