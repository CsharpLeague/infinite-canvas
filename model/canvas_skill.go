package model

const (
	CanvasSkillStatusDraft     = "draft"
	CanvasSkillStatusPublished = "published"
)

type CanvasSkill struct {
	ID           string            `json:"id" gorm:"primaryKey"`
	Slug         string            `json:"slug" gorm:"size:191;uniqueIndex"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Category     string            `json:"category" gorm:"index"`
	Icon         string            `json:"icon"`
	Placeholder  string            `json:"placeholder"`
	Instructions string            `json:"instructions" gorm:"type:text"`
	Files        map[string]string `json:"files" gorm:"serializer:json;type:text"`
	AllowedTools []string          `json:"allowedTools" gorm:"serializer:json"`
	Status       string            `json:"status" gorm:"index"`
	Sort         int               `json:"sort"`
	CreatedAt    string            `json:"createdAt"`
	UpdatedAt    string            `json:"updatedAt"`
}

type CanvasSkillList struct {
	Items []CanvasSkill `json:"items"`
	Total int           `json:"total"`
}

type CanvasSkillSummary struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Icon        string `json:"icon"`
	Placeholder string `json:"placeholder"`
}
