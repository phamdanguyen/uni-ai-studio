// Package poller implements async task polling for external AI generation services.
// It polls FAL, Ark, MiniMax, and Vidu APIs for task completion
// and updates the world state with results.
package poller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// TaskStatus represents the lifecycle of an async generation task.
type TaskStatus string

const (
	TaskPending    TaskStatus = "pending"
	TaskProcessing TaskStatus = "processing"
	TaskCompleted  TaskStatus = "completed"
	TaskFailed     TaskStatus = "failed"
)

// TrackedTask represents a task being polled.
type TrackedTask struct {
	ExternalID  string            `json:"externalId"`
	Provider    string            `json:"provider"`
	MediaType   string            `json:"mediaType"` // "IMAGE", "VIDEO"
	ProjectID   string            `json:"projectId"`
	PanelIndex  int               `json:"panelIndex,omitempty"`
	Status      TaskStatus        `json:"status"`
	ResultURL   string            `json:"resultUrl,omitempty"`
	Error       string            `json:"error,omitempty"`
	APIKeys     map[string]string `json:"-"` // Provider API keys (not serialized)
	CreatedAt   time.Time         `json:"createdAt"`
	CompletedAt *time.Time        `json:"completedAt,omitempty"`
	Attempts    int               `json:"attempts"`
}

// ResultCallback is called when a task completes or fails.
type ResultCallback func(task TrackedTask)

// Poller continuously checks external APIs for task completion.
type Poller struct {
	mu       sync.RWMutex
	tasks    map[string]*TrackedTask
	client   *http.Client
	logger   *slog.Logger
	callback ResultCallback
	interval time.Duration
	stop     chan struct{}
}

// NewPoller creates a new task poller.
func NewPoller(callback ResultCallback, interval time.Duration, logger *slog.Logger) *Poller {
	if interval == 0 {
		interval = 5 * time.Second
	}
	return &Poller{
		tasks:    make(map[string]*TrackedTask),
		client:   &http.Client{Timeout: 30 * time.Second},
		logger:   logger.With("component", "poller"),
		callback: callback,
		interval: interval,
		stop:     make(chan struct{}),
	}
}

// Track adds a new task to be polled.
func (p *Poller) Track(task TrackedTask) {
	p.mu.Lock()
	defer p.mu.Unlock()

	task.Status = TaskPending
	task.CreatedAt = time.Now()
	p.tasks[task.ExternalID] = &task

	p.logger.Info("tracking task",
		"externalId", task.ExternalID,
		"provider", task.Provider,
		"mediaType", task.MediaType,
	)
}

// Start begins the polling loop in a goroutine.
func (p *Poller) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-p.stop:
				return
			case <-ticker.C:
				p.pollAll(ctx)
			}
		}
	}()
	p.logger.Info("poller started", "interval", p.interval)
}

// Stop halts the polling loop.
func (p *Poller) Stop() {
	close(p.stop)
}

// PendingCount returns the number of tasks still being polled.
func (p *Poller) PendingCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	count := 0
	for _, t := range p.tasks {
		if t.Status == TaskPending || t.Status == TaskProcessing {
			count++
		}
	}
	return count
}

// pollAll checks all pending tasks.
func (p *Poller) pollAll(ctx context.Context) {
	p.mu.RLock()
	pending := make([]*TrackedTask, 0)
	for _, t := range p.tasks {
		if t.Status == TaskPending || t.Status == TaskProcessing {
			pending = append(pending, t)
		}
	}
	p.mu.RUnlock()

	if len(pending) == 0 {
		return
	}

	for _, task := range pending {
		select {
		case <-ctx.Done():
			return
		default:
		}

		p.pollTask(ctx, task)
	}
}

// pollTask checks a single task's status via the provider API.
func (p *Poller) pollTask(ctx context.Context, task *TrackedTask) {
	task.Attempts++

	var resultURL string
	var err error

	switch task.Provider {
	case "FAL":
		resultURL, err = p.pollFAL(ctx, task)
	case "ARK":
		resultURL, err = p.pollARK(ctx, task)
	case "MINIMAX":
		resultURL, err = p.pollMiniMax(ctx, task)
	case "VIDU":
		resultURL, err = p.pollVidu(ctx, task)
	default:
		err = fmt.Errorf("unknown provider: %s", task.Provider)
	}

	if err != nil {
		if task.Attempts > 60 { // ~5 minutes at 5s intervals
			p.mu.Lock()
			task.Status = TaskFailed
			task.Error = err.Error()
			p.mu.Unlock()
			p.logger.Error("task failed after max attempts",
				"externalId", task.ExternalID, "error", err,
			)
			if p.callback != nil {
				p.callback(*task)
			}
		}
		return
	}

	if resultURL != "" {
		now := time.Now()
		p.mu.Lock()
		task.Status = TaskCompleted
		task.ResultURL = resultURL
		task.CompletedAt = &now
		p.mu.Unlock()

		p.logger.Info("task completed",
			"externalId", task.ExternalID,
			"resultUrl", resultURL,
			"duration", now.Sub(task.CreatedAt),
		)
		if p.callback != nil {
			p.callback(*task)
		}
	} else {
		p.mu.Lock()
		task.Status = TaskProcessing
		p.mu.Unlock()
	}
}

// --- Provider-specific polling ---

func (p *Poller) pollFAL(ctx context.Context, task *TrackedTask) (string, error) {
	// ExternalID format: FAL:IMAGE:endpoint:requestId
	parts := strings.SplitN(task.ExternalID, ":", 4)
	if len(parts) < 4 {
		return "", fmt.Errorf("invalid FAL external ID: %s", task.ExternalID)
	}
	endpoint, requestID := parts[2], parts[3]
	apiKey := task.APIKeys["fal"]

	url := fmt.Sprintf("https://queue.fal.run/%s/requests/%s/status", endpoint, requestID)
	data, err := p.httpGet(ctx, url, map[string]string{
		"Authorization": "Key " + apiKey,
	})
	if err != nil {
		return "", err
	}

	status, _ := data["status"].(string)
	switch status {
	case "COMPLETED":
		// Get the actual result
		resultURL := fmt.Sprintf("https://queue.fal.run/%s/requests/%s", endpoint, requestID)
		resultData, err := p.httpGet(ctx, resultURL, map[string]string{
			"Authorization": "Key " + apiKey,
		})
		if err != nil {
			return "", err
		}
		return extractFALResultURL(resultData, task.MediaType), nil
	case "FAILED":
		errMsg, _ := data["error"].(string)
		return "", fmt.Errorf("FAL task failed: %s", errMsg)
	default:
		return "", nil // Still processing
	}
}

func (p *Poller) pollARK(ctx context.Context, task *TrackedTask) (string, error) {
	// ExternalID format: ARK:VIDEO:taskId
	parts := strings.SplitN(task.ExternalID, ":", 3)
	if len(parts) < 3 {
		return "", fmt.Errorf("invalid ARK external ID: %s", task.ExternalID)
	}
	taskID := parts[2]
	apiKey := task.APIKeys["ark"]

	url := fmt.Sprintf("https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks/%s", taskID)
	data, err := p.httpGet(ctx, url, map[string]string{
		"Authorization": "Bearer " + apiKey,
	})
	if err != nil {
		return "", err
	}

	status, _ := data["status"].(string)
	switch status {
	case "succeeded":
		content, _ := data["content"].(map[string]any)
		videoURL, _ := content["video_url"].(string)
		return videoURL, nil
	case "failed":
		errInfo, _ := data["error"].(map[string]any)
		errMsg, _ := errInfo["message"].(string)
		return "", fmt.Errorf("ARK task failed: %s", errMsg)
	default:
		return "", nil
	}
}

func (p *Poller) pollMiniMax(ctx context.Context, task *TrackedTask) (string, error) {
	parts := strings.SplitN(task.ExternalID, ":", 3)
	if len(parts) < 3 {
		return "", fmt.Errorf("invalid MiniMax external ID: %s", task.ExternalID)
	}
	taskID := parts[2]
	apiKey := task.APIKeys["minimax"]

	url := fmt.Sprintf("https://api.minimaxi.com/v1/query/video_generation?task_id=%s", taskID)
	data, err := p.httpGet(ctx, url, map[string]string{
		"Authorization": "Bearer " + apiKey,
	})
	if err != nil {
		return "", err
	}

	status, _ := data["status"].(string)
	switch status {
	case "Success":
		fileID, _ := data["file_id"].(string)
		if fileID != "" {
			return fmt.Sprintf("https://api.minimaxi.com/v1/files/retrieve?file_id=%s", fileID), nil
		}
		return "", nil
	case "Fail":
		baseResp, _ := data["base_resp"].(map[string]any)
		errMsg, _ := baseResp["status_msg"].(string)
		return "", fmt.Errorf("MiniMax task failed: %s", errMsg)
	default:
		return "", nil
	}
}

func (p *Poller) pollVidu(ctx context.Context, task *TrackedTask) (string, error) {
	parts := strings.SplitN(task.ExternalID, ":", 3)
	if len(parts) < 3 {
		return "", fmt.Errorf("invalid Vidu external ID: %s", task.ExternalID)
	}
	taskID := parts[2]
	apiKey := task.APIKeys["vidu"]

	url := fmt.Sprintf("https://api.vidu.cn/ent/v2/tasks/%s", taskID)
	data, err := p.httpGet(ctx, url, map[string]string{
		"Authorization": "Token " + apiKey,
	})
	if err != nil {
		return "", err
	}

	state, _ := data["state"].(string)
	switch state {
	case "success":
		creations, _ := data["creations"].([]any)
		if len(creations) > 0 {
			creation, _ := creations[0].(map[string]any)
			videoURL, _ := creation["url"].(string)
			return videoURL, nil
		}
		return "", nil
	case "failed":
		errMsg, _ := data["err_msg"].(string)
		return "", fmt.Errorf("Vidu task failed: %s", errMsg)
	default:
		return "", nil
	}
}

// --- HTTP Helpers ---

func (p *Poller) httpGet(ctx context.Context, url string, headers map[string]string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return data, nil
}

func extractFALResultURL(data map[string]any, mediaType string) string {
	switch mediaType {
	case "IMAGE":
		images, _ := data["images"].([]any)
		if len(images) > 0 {
			img, _ := images[0].(map[string]any)
			url, _ := img["url"].(string)
			return url
		}
	case "VIDEO":
		video, _ := data["video"].(map[string]any)
		url, _ := video["url"].(string)
		return url
	}
	return ""
}
