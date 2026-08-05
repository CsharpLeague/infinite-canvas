package service

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
)

const videoTaskPollInterval = 5 * time.Second
const videoTaskFinishedRetention = 24 * time.Hour
const videoTaskCleanupInterval = 10 * time.Minute

var (
	videoTaskPollerOnce  sync.Once
	videoTaskPollWake    = make(chan struct{}, 1)
	videoTaskPollerMu    sync.RWMutex
	videoTaskPoller      VideoTaskPollFunc
	videoTaskRunningMu   sync.Mutex
	videoTaskRunning     bool
	videoTaskWakePending bool
)

type VideoTaskCreateInput struct {
	UserID          string
	UserDisplayName string
	Model           string
	ChannelID       string
	UserChannelID   string
	ChannelName     string
	Source          string
	SourceID        string
	ClientTaskID    string
	UpstreamTaskID  string
	UpstreamVideoID string
	Status          string
	Progress        int
	Seconds         string
	Size            string
	VideoURL        string
	StorageKey      string
	FirstFrameURL   string
	FirstFrameKey   string
	LastFrameURL    string
	LastFrameKey    string
	MimeType        string
	Bytes           int64
	Error           string
	ErrorDetail     string
	RequestBody     string
	ResponseBody    string
	Credits         int
}

type VideoTaskPollUpdate struct {
	Status        string
	Progress      int
	Seconds       string
	Size          string
	VideoURL      string
	FirstFrameURL string
	LastFrameURL  string
	Error         string
	ErrorDetail   string
	ResponseBody  string
}

type VideoTaskPollFunc func(model.VideoTask) (VideoTaskPollUpdate, error)

func CreateVideoTask(input VideoTaskCreateInput) (model.VideoTask, error) {
	current := now()
	status := NormalizeVideoTaskStatus(input.Status)
	if status == "" {
		status = "queued"
	}
	task := model.VideoTask{
		ID:              firstVideoTaskValue(input.ClientTaskID, input.UpstreamTaskID, input.UpstreamVideoID, "video-task-"+uuid.NewString()),
		UserID:          strings.TrimSpace(input.UserID),
		UserDisplayName: strings.TrimSpace(input.UserDisplayName),
		Model:           strings.TrimSpace(input.Model),
		ChannelID:       strings.TrimSpace(input.ChannelID),
		UserChannelID:   strings.TrimSpace(input.UserChannelID),
		ChannelName:     strings.TrimSpace(input.ChannelName),
		Source:          normalizeVideoTaskSource(input.Source),
		SourceID:        strings.TrimSpace(input.SourceID),
		UpstreamTaskID:  strings.TrimSpace(input.UpstreamTaskID),
		UpstreamVideoID: strings.TrimSpace(input.UpstreamVideoID),
		Status:          status,
		Progress:        clampProgress(input.Progress),
		Seconds:         strings.TrimSpace(input.Seconds),
		Size:            strings.TrimSpace(input.Size),
		VideoURL:        strings.TrimSpace(input.VideoURL),
		StorageKey:      strings.TrimSpace(input.StorageKey),
		FirstFrameURL:   strings.TrimSpace(input.FirstFrameURL),
		FirstFrameKey:   strings.TrimSpace(input.FirstFrameKey),
		LastFrameURL:    strings.TrimSpace(input.LastFrameURL),
		LastFrameKey:    strings.TrimSpace(input.LastFrameKey),
		MimeType:        strings.TrimSpace(input.MimeType),
		Bytes:           input.Bytes,
		Error:           strings.TrimSpace(input.Error),
		ErrorDetail:     strings.TrimSpace(input.ErrorDetail),
		RequestBody:     input.RequestBody,
		ResponseBody:    input.ResponseBody,
		LastResponse:    input.ResponseBody,
		Credits:         input.Credits,
		CreatedAt:       current,
		UpdatedAt:       current,
	}
	if IsCompletedVideoTaskStatus(task.Status) || task.VideoURL != "" {
		if task.VideoURL != "" && task.StorageKey == "" {
			object, uploadErr := uploadGeneratedVideo(task.UserID, task.VideoURL)
			if uploadErr != nil {
				task.ErrorDetail = "视频自动上传云存储失败: " + uploadErr.Error()
			} else {
				task.VideoURL = object.URL
				task.StorageKey = object.StorageKey
				task.MimeType = object.MimeType
				task.Bytes = object.Bytes
			}
		}
		if task.FirstFrameURL != "" && task.FirstFrameKey == "" {
			task.FirstFrameURL, task.FirstFrameKey = persistGeneratedVideoFrame(task.UserID, task.FirstFrameURL, "canvas-video-first-frame.png")
		}
		if task.LastFrameURL != "" && task.LastFrameKey == "" {
			task.LastFrameURL, task.LastFrameKey = persistGeneratedVideoFrame(task.UserID, task.LastFrameURL, "canvas-video-last-frame.png")
		}
		task.Status = "completed"
		task.Progress = 100
		task.CompletedAt = current
	} else if IsFailedVideoTaskStatus(task.Status) || task.Error != "" {
		task.Status = "failed"
		task.CompletedAt = current
	}
	saved, err := repository.SaveVideoTask(task)
	if err == nil && (saved.UpstreamTaskID != "" || saved.UpstreamVideoID != "") && !IsCompletedVideoTaskStatus(saved.Status) && !IsFailedVideoTaskStatus(saved.Status) {
		WakeVideoTaskPoller()
	}
	return saved, err
}

func GetUserVideoTask(userID string, id string) (model.VideoTask, bool, error) {
	return repository.GetUserVideoTask(strings.TrimSpace(userID), strings.TrimSpace(id))
}

func ListUserVideoTasks(userID string, source string, limit int) ([]map[string]any, error) {
	tasks, err := repository.ListUserVideoTasks(strings.TrimSpace(userID), normalizeVideoTaskSource(source), limit)
	if err != nil {
		return nil, err
	}
	result := make([]map[string]any, 0, len(tasks))
	for _, task := range tasks {
		result = append(result, VideoTaskResponse(task))
	}
	return result, nil
}

func DeleteUserVideoTask(userID string, id string) error {
	return repository.DeleteUserVideoTask(strings.TrimSpace(userID), strings.TrimSpace(id))
}

func VideoTaskResponse(task model.VideoTask) map[string]any {
	result := map[string]any{
		"id":            task.ID,
		"object":        "video",
		"model":         task.Model,
		"channelId":     task.ChannelID,
		"userChannelId": task.UserChannelID,
		"channelName":   task.ChannelName,
		"source":        task.Source,
		"source_id":     task.SourceID,
		"status":        task.Status,
		"progress":      task.Progress,
		"task_id":       firstVideoTaskValue(task.UpstreamTaskID, task.ID),
		"video_id":      task.UpstreamVideoID,
		"seconds":       task.Seconds,
		"size":          task.Size,
		"created_at":    task.CreatedAt,
		"updated_at":    task.UpdatedAt,
		"started_at":    task.StartedAt,
		"completed_at":  task.CompletedAt,
		"createdAt":     task.CreatedAt,
		"updatedAt":     task.UpdatedAt,
		"request_body":  task.RequestBody,
	}
	if task.VideoURL != "" {
		result["url"] = task.VideoURL
		result["video_url"] = task.VideoURL
		result["data"] = []map[string]any{{"url": task.VideoURL}}
		result["storageKey"] = task.StorageKey
		result["mimeType"] = task.MimeType
		result["bytes"] = task.Bytes
	}
	if task.FirstFrameURL != "" {
		result["first_frame_url"] = task.FirstFrameURL
		result["firstFrameUrl"] = task.FirstFrameURL
		result["firstFrameStorageKey"] = task.FirstFrameKey
	}
	if task.LastFrameURL != "" {
		result["last_frame_url"] = task.LastFrameURL
		result["lastFrameUrl"] = task.LastFrameURL
		result["lastFrameStorageKey"] = task.LastFrameKey
	}
	if IsFailedVideoTaskStatus(task.Status) && (task.Error != "" || task.ErrorDetail != "") {
		result["error"] = map[string]any{"message": firstVideoTaskValue(task.Error, task.ErrorDetail)}
		result["error_detail"] = task.ErrorDetail
	}
	return result
}

func StartVideoTaskPoller(poll VideoTaskPollFunc) {
	if poll == nil {
		return
	}
	videoTaskPollerMu.Lock()
	videoTaskPoller = poll
	videoTaskPollerMu.Unlock()
	videoTaskPollerOnce.Do(func() {
		go runVideoTaskPoller()
	})
	WakeVideoTaskPoller()
}

func WakeVideoTaskPoller() {
	videoTaskRunningMu.Lock()
	if videoTaskRunning {
		videoTaskWakePending = true
		videoTaskRunningMu.Unlock()
		return
	}
	videoTaskRunning = true
	videoTaskWakePending = false
	videoTaskRunningMu.Unlock()
	select {
	case videoTaskPollWake <- struct{}{}:
	default:
		videoTaskRunningMu.Lock()
		videoTaskRunning = false
		videoTaskRunningMu.Unlock()
	}
}

func runVideoTaskPoller() {
	inFlight := sync.Map{}
	lastCleanupAt := time.Time{}
	for range videoTaskPollWake {
		for {
			current := time.Now()
			tasks, err := repository.ListDueVideoTasks(200)
			if err != nil {
				log.Printf("list due video tasks failed err=%v", err)
				waitForNextVideoTaskPoll()
				continue
			}
			if len(tasks) == 0 {
				videoTaskRunningMu.Lock()
				if videoTaskWakePending {
					videoTaskWakePending = false
					videoTaskRunningMu.Unlock()
					continue
				}
				videoTaskRunning = false
				videoTaskRunningMu.Unlock()
				break
			}
			if lastCleanupAt.IsZero() || current.Sub(lastCleanupAt) >= videoTaskCleanupInterval {
				if err := repository.DeleteFinishedVideoTasksBefore(videoTaskTime(current.Add(-videoTaskFinishedRetention))); err != nil {
					log.Printf("cleanup finished video tasks failed err=%v", err)
				}
				lastCleanupAt = current
			}
			for _, task := range tasks {
				if _, loaded := inFlight.LoadOrStore(task.ID, true); loaded {
					continue
				}
				go func(task model.VideoTask) {
					defer inFlight.Delete(task.ID)
					poll := currentVideoTaskPoller()
					if poll == nil {
						return
					}
					update, err := poll(task)
					if err != nil {
						update = VideoTaskPollUpdate{Status: task.Status, ErrorDetail: err.Error()}
					}
					if err := UpdateVideoTaskFromPoll(task, update); err != nil {
						log.Printf("update video task failed id=%s err=%v", task.ID, err)
					}
				}(task)
			}
			waitForNextVideoTaskPoll()
		}
	}
}

func currentVideoTaskPoller() VideoTaskPollFunc {
	videoTaskPollerMu.RLock()
	defer videoTaskPollerMu.RUnlock()
	return videoTaskPoller
}

func waitForNextVideoTaskPoll() {
	time.Sleep(videoTaskPollInterval)
}

func UpdateVideoTaskFromPoll(task model.VideoTask, update VideoTaskPollUpdate) error {
	current := now()
	task.Status = NormalizeVideoTaskStatus(firstVideoTaskValue(update.Status, task.Status))
	if task.Status == "" {
		task.Status = "processing"
	}
	if update.Progress > 0 || task.Progress == 0 {
		task.Progress = clampProgress(update.Progress)
	}
	if strings.TrimSpace(update.Seconds) != "" {
		task.Seconds = strings.TrimSpace(update.Seconds)
	}
	if strings.TrimSpace(update.Size) != "" {
		task.Size = strings.TrimSpace(update.Size)
	}
	if videoURL := strings.TrimSpace(update.VideoURL); videoURL != "" && task.StorageKey == "" {
		task.VideoURL = videoURL
	}
	if frameURL := strings.TrimSpace(update.FirstFrameURL); frameURL != "" && task.FirstFrameKey == "" {
		task.FirstFrameURL = frameURL
	}
	if frameURL := strings.TrimSpace(update.LastFrameURL); frameURL != "" && task.LastFrameKey == "" {
		task.LastFrameURL = frameURL
	}
	if strings.TrimSpace(update.Error) != "" {
		task.Error = strings.TrimSpace(update.Error)
	}
	if strings.TrimSpace(update.ErrorDetail) != "" {
		task.ErrorDetail = strings.TrimSpace(update.ErrorDetail)
	}
	if update.ResponseBody != "" {
		task.LastResponse = update.ResponseBody
	}
	task.UpdatedAt = current
	task.LastPolledAt = videoTaskTime(time.Now())
	if task.VideoURL != "" || IsCompletedVideoTaskStatus(task.Status) {
		task.Status = "completed"
		task.Progress = 100
		task.CompletedAt = current
		task.Error = ""
		task.ErrorDetail = ""
	} else if task.Error != "" || IsFailedVideoTaskStatus(task.Status) {
		task.Status = "failed"
		task.CompletedAt = current
	}
	if _, err := repository.SaveVideoTask(task); err != nil || task.Status != "completed" {
		return err
	}

	if task.VideoURL != "" && task.StorageKey == "" {
		object, uploadErr := uploadGeneratedVideo(task.UserID, task.VideoURL)
		if uploadErr != nil {
			task.ErrorDetail = "视频自动上传云存储失败: " + uploadErr.Error()
		} else {
			task.VideoURL = object.URL
			task.StorageKey = object.StorageKey
			task.MimeType = object.MimeType
			task.Bytes = object.Bytes
		}
	}
	if task.FirstFrameURL != "" && task.FirstFrameKey == "" {
		task.FirstFrameURL, task.FirstFrameKey = persistGeneratedVideoFrame(task.UserID, task.FirstFrameURL, "canvas-video-first-frame.png")
	}
	if task.LastFrameURL != "" && task.LastFrameKey == "" {
		task.LastFrameURL, task.LastFrameKey = persistGeneratedVideoFrame(task.UserID, task.LastFrameURL, "canvas-video-last-frame.png")
	}
	task.UpdatedAt = now()
	_, err := repository.SaveVideoTask(task)
	return err
}

func uploadGeneratedVideo(userID string, videoURL string) (UploadedStorageObject, error) {
	return uploadGeneratedMedia(userID, videoURL, "canvas-video.mp4")
}

func persistGeneratedVideoFrame(userID string, frameURL string, filename string) (string, string) {
	object, err := uploadGeneratedMedia(userID, frameURL, filename)
	if err != nil {
		return frameURL, ""
	}
	return object.URL, object.StorageKey
}

func uploadGeneratedMedia(userID string, sourceURL string, filename string) (UploadedStorageObject, error) {
	user, found, err := repository.GetUserByID(strings.TrimSpace(userID))
	if err != nil {
		return UploadedStorageObject{}, err
	}
	if !found {
		return UploadedStorageObject{}, fmt.Errorf("用户不存在")
	}
	ctx := WithUser(context.Background(), model.PublicUser(user))
	return UploadRemoteStorageObject(ctx, sourceURL, filename)
}

func NormalizeVideoTaskStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "complete", "done", "succeeded", "success":
		return "completed"
	case "failed", "fail", "error", "cancelled", "canceled":
		return "failed"
	case "running", "processing", "in_progress", "in-progress":
		return "processing"
	case "queued", "queue", "pending", "":
		return "queued"
	default:
		return strings.ToLower(strings.TrimSpace(status))
	}
}

func IsCompletedVideoTaskStatus(status string) bool {
	return NormalizeVideoTaskStatus(status) == "completed"
}

func IsFailedVideoTaskStatus(status string) bool {
	return NormalizeVideoTaskStatus(status) == "failed"
}

func videoTaskTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func firstVideoTaskValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizeVideoTaskSource(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "canvas":
		return "canvas"
	case "video-workbench", "":
		return "video-workbench"
	default:
		return "video-workbench"
	}
}

func clampProgress(value int) int {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}
