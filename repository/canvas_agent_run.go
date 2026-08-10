package repository

import (
	"errors"

	"github.com/tigerowo/infinite-canvas/model"
	"gorm.io/gorm"
)

func CreateCanvasAgentRun(run model.CanvasAgentRun, event model.CanvasAgentEvent) error {
	db, err := DB()
	if err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&run).Error; err != nil {
			return err
		}
		return tx.Create(&event).Error
	})
}

func SaveCanvasAgentRun(run model.CanvasAgentRun) error {
	db, err := DB()
	if err != nil {
		return err
	}
	return db.Save(&run).Error
}

func AcceptCanvasAgentToolResults(id string, calls []model.CanvasAgentToolCall, protocol []byte, phase string, event model.CanvasAgentEvent, now string) error {
	db, err := DB()
	if err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{
			"status": model.CanvasAgentRunStatusRunning, "pending_tool_calls": nil, "protocol": protocol, "updated_at": now,
		}
		if phase != "" {
			updates["phase"] = phase
		}
		result := tx.Model(&model.CanvasAgentRun{}).Where("id = ? AND status = ?", id, model.CanvasAgentRunStatusWaiting).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("工具结果已接收或 Run 不在等待状态")
		}
		return tx.Create(&event).Error
	})
}

func FindCanvasAgentRun(id string) (model.CanvasAgentRun, error) {
	db, err := DB()
	if err != nil {
		return model.CanvasAgentRun{}, err
	}
	var run model.CanvasAgentRun
	err = db.First(&run, "id = ?", id).Error
	return run, err
}

func FindLatestCanvasAgentRun(sessionID, canvasID, ownerID string) (model.CanvasAgentRun, bool, error) {
	db, err := DB()
	if err != nil {
		return model.CanvasAgentRun{}, false, err
	}
	var run model.CanvasAgentRun
	query := db.Where("session_id = ? AND canvas_id = ? AND owner_id = ?", sessionID, canvasID, ownerID).Order("created_at DESC")
	err = query.First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.CanvasAgentRun{}, false, nil
	}
	return run, err == nil, err
}

func SaveCanvasAgentEvent(event model.CanvasAgentEvent) error {
	db, err := DB()
	if err != nil {
		return err
	}
	return db.Create(&event).Error
}

func UpdateCanvasAgentRun(id string, values map[string]any) error {
	db, err := DB()
	if err != nil {
		return err
	}
	return db.Model(&model.CanvasAgentRun{}).Where("id = ?", id).Updates(values).Error
}

func UpdateCanvasAgentRunWithEvent(id, expectedStatus string, values map[string]any, event model.CanvasAgentEvent) error {
	db, err := DB()
	if err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.CanvasAgentRun{}).Where("id = ? AND status = ?", id, expectedStatus).Updates(values)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("Agent Run 状态已变化")
		}
		return tx.Create(&event).Error
	})
}

func ListCanvasAgentEvents(runID string, after uint64) ([]model.CanvasAgentEvent, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	var events []model.CanvasAgentEvent
	err = db.Where("run_id = ? AND id > ?", runID, after).Order("id ASC").Limit(200).Find(&events).Error
	return events, err
}
