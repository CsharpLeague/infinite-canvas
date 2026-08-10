package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/service"
)

func CreateCanvasAgentRun(w http.ResponseWriter, r *http.Request) {
	var input service.CreateCanvasAgentRunInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		FailError(w, err)
		return
	}
	run, err := service.CreateCanvasAgentRun(r.Context(), input)
	if err != nil {
		Fail(w, err.Error())
		return
	}
	OK(w, run)
}

func RecoverCanvasAgentRun(w http.ResponseWriter, r *http.Request) {
	run, err := service.RecoverCanvasAgentRun(r.Context(), r.URL.Query().Get("sessionId"), r.URL.Query().Get("canvasId"))
	if err != nil {
		Fail(w, err.Error())
		return
	}
	OK(w, run)
}

func SubmitCanvasAgentToolResults(w http.ResponseWriter, r *http.Request, id string) {
	var input struct {
		Results []service.CanvasAgentToolResultInput `json:"results"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		FailError(w, err)
		return
	}
	run, err := service.SubmitCanvasAgentToolResults(r.Context(), id, r.Header.Get("X-Agent-Run-Token"), input.Results)
	if err != nil {
		Fail(w, err.Error())
		return
	}
	OK(w, run)
}

func CancelCanvasAgentRun(w http.ResponseWriter, r *http.Request, id string) {
	run, err := service.CancelCanvasAgentRun(r.Context(), id, r.Header.Get("X-Agent-Run-Token"))
	if err != nil {
		Fail(w, err.Error())
		return
	}
	OK(w, run)
}

func CanvasAgentRunEvents(w http.ResponseWriter, r *http.Request, id string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		Fail(w, "当前连接不支持流式响应")
		return
	}
	after, _ := strconv.ParseUint(firstNonEmpty(r.Header.Get("Last-Event-ID"), r.URL.Query().Get("after")), 10, 64)
	token := r.Header.Get("X-Agent-Run-Token")
	if err := service.ResumeCanvasAgentRun(r.Context(), id, token); err != nil {
		FailError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	poll := time.NewTicker(500 * time.Millisecond)
	heartbeat := time.NewTicker(15 * time.Second)
	defer poll.Stop()
	defer heartbeat.Stop()
	for {
		run, events, err := service.CanvasAgentRunEvents(r.Context(), id, token, after)
		if err != nil {
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", mustJSON(map[string]string{"message": err.Error()}))
			flusher.Flush()
			return
		}
		for _, event := range events {
			fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.ID, event.Type, event.Data)
			after = event.ID
		}
		if len(events) > 0 {
			flusher.Flush()
		}
		if len(events) == 0 && (run.Status == model.CanvasAgentRunStatusCompleted || run.Status == model.CanvasAgentRunStatusFailed || run.Status == model.CanvasAgentRunStatusCancelled || run.Status == model.CanvasAgentRunStatusWaiting) {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		case <-poll.C:
		}
	}
}

func mustJSON(value any) string { data, _ := json.Marshal(value); return string(data) }
