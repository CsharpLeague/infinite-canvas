package repository

import "github.com/tigerowo/infinite-canvas/model"

func SaveVirtualPortraitTask(task model.VirtualPortraitTask) error {
	db, err := DB()
	if err != nil {
		return err
	}
	return db.Save(&task).Error
}

func GetVirtualPortraitTask(userID, id string) (model.VirtualPortraitTask, bool, error) {
	db, err := DB()
	if err != nil {
		return model.VirtualPortraitTask{}, false, err
	}
	var task model.VirtualPortraitTask
	err = db.First(&task, "user_id = ? AND (id = ? OR asset_id = ?)", userID, id, id).Error
	if err != nil {
		return model.VirtualPortraitTask{}, false, nil
	}
	return task, true, nil
}

func FindVirtualPortraitTask(userID, channelID, fingerprint string) (model.VirtualPortraitTask, bool, error) {
	db, err := DB()
	if err != nil {
		return model.VirtualPortraitTask{}, false, err
	}
	var task model.VirtualPortraitTask
	err = db.Where("user_id = ? AND channel_id = ? AND source_fingerprint = ?", userID, channelID, fingerprint).First(&task).Error
	if err != nil {
		return model.VirtualPortraitTask{}, false, nil
	}
	return task, true, nil
}

func DeleteVirtualPortraitTask(userID, id string) error {
	db, err := DB()
	if err != nil {
		return err
	}
	return db.Where("user_id = ? AND id = ?", userID, id).Delete(&model.VirtualPortraitTask{}).Error
}
