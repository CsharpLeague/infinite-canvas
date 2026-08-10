package model

import "encoding/json"

const (
	CanvasAgentRunStatusRunning   = "running"
	CanvasAgentRunStatusWaiting   = "waiting_tool"
	CanvasAgentRunStatusCompleted = "completed"
	CanvasAgentRunStatusFailed    = "failed"
	CanvasAgentRunStatusCancelled = "cancelled"
)

const (
	CanvasAgentEventRunCreated    = "run.created"
	CanvasAgentEventStatus        = "run.status"
	CanvasAgentEventTextDelta     = "text.delta"
	CanvasAgentEventTextCompleted = "text.completed"
	CanvasAgentEventToolRequested = "tool.requested"
	CanvasAgentEventToolAccepted  = "tool.result.accepted"
	CanvasAgentEventCompleted     = "run.completed"
	CanvasAgentEventFailed        = "run.failed"
	CanvasAgentEventCancelled     = "run.cancelled"
)

type CanvasAgentRun struct {
	ID               string          `json:"id" gorm:"primaryKey"`
	OwnerID          string          `json:"-" gorm:"index"`
	Token            string          `json:"token" gorm:"-"`
	TokenHash        string          `json:"-" gorm:"size:64;index"`
	SessionID        string          `json:"sessionId" gorm:"index"`
	CanvasID         string          `json:"canvasId" gorm:"index"`
	SkillID          string          `json:"skillId" gorm:"index"`
	Model            string          `json:"model"`
	ChannelID        string          `json:"-" gorm:"index"`
	UserChannelID    string          `json:"-" gorm:"index"`
	Status           string          `json:"status" gorm:"index"`
	Phase            string          `json:"phase"`
	Step             int             `json:"step"`
	MaxSteps         int             `json:"maxSteps"`
	Input            json.RawMessage `json:"input" gorm:"type:text"`
	Protocol         json.RawMessage `json:"-" gorm:"type:text"`
	Tools            json.RawMessage `json:"-" gorm:"type:text"`
	PendingToolCalls json.RawMessage `json:"pendingToolCalls,omitempty" gorm:"type:text"`
	Output           json.RawMessage `json:"output" gorm:"type:text"`
	Error            string          `json:"error" gorm:"type:text"`
	CreatedAt        string          `json:"createdAt"`
	UpdatedAt        string          `json:"updatedAt"`
}

type CanvasAgentEvent struct {
	ID        uint64          `json:"id" gorm:"primaryKey;autoIncrement"`
	RunID     string          `json:"runId" gorm:"index"`
	Type      string          `json:"type" gorm:"index"`
	Data      json.RawMessage `json:"data" gorm:"type:text"`
	CreatedAt string          `json:"createdAt"`
}

type CanvasAgentToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}
