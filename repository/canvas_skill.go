package repository

import (
	"errors"

	"github.com/tigerowo/infinite-canvas/model"
	"gorm.io/gorm"
)

func ListCanvasSkills(q model.Query, publishedOnly bool) ([]model.CanvasSkill, int64, error) {
	db, err := DB()
	if err != nil {
		return nil, 0, err
	}
	q.Normalize()
	tx := db.Model(&model.CanvasSkill{})
	if publishedOnly {
		tx = tx.Where("status = ?", model.CanvasSkillStatusPublished)
	}
	if q.Keyword != "" {
		like := "%" + q.Keyword + "%"
		tx = tx.Where("name LIKE ? OR description LIKE ? OR slug LIKE ?", like, like, like)
	}
	if q.Category != "" && q.Category != "all" {
		tx = tx.Where("category = ?", q.Category)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []model.CanvasSkill
	err = tx.Order("sort asc, updated_at desc").Offset(q.Offset()).Limit(q.PageSize).Find(&items).Error
	return items, total, err
}

func FindCanvasSkill(id string, publishedOnly bool) (model.CanvasSkill, error) {
	db, err := DB()
	if err != nil {
		return model.CanvasSkill{}, err
	}
	tx := db.Where("id = ? OR slug = ?", id, id)
	if publishedOnly {
		tx = tx.Where("status = ?", model.CanvasSkillStatusPublished)
	}
	var item model.CanvasSkill
	return item, tx.First(&item).Error
}

func SaveCanvasSkill(item model.CanvasSkill) (model.CanvasSkill, error) {
	db, err := DB()
	if err != nil {
		return item, err
	}
	if item.ID != "" {
		var saved model.CanvasSkill
		err := db.Where("id = ?", item.ID).First(&saved).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return item, err
		}
		if err == nil && item.CreatedAt == "" {
			item.CreatedAt = saved.CreatedAt
		}
	}
	return item, db.Save(&item).Error
}

func DeleteCanvasSkill(id string) error {
	db, err := DB()
	if err != nil {
		return err
	}
	return db.Delete(&model.CanvasSkill{}, "id = ?", id).Error
}
