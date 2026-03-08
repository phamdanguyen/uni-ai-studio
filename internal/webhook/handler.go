// Package webhook handles async task completion callbacks from providers.
// Providers like FAL and Vidu can send webhooks instead of requiring polling.
package webhook

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

// CompletionEvent is emitted when a webhook confirms task completion.
type CompletionEvent struct {
	Provider   string         `json:"provider"`
	ExternalID string         `json:"externalId"`
	Status     string         `json:"status"` // "completed", "failed"
	ResultURL  string         `json:"resultUrl,omitempty"`
	Error      string         `json:"error,omitempty"`
	RawData    map[string]any `json:"rawData,omitempty"`
}

// Handler processes webhooks from AI generation providers.
type Handler struct {
	logger    *slog.Logger
	callbacks []func(CompletionEvent)
	secret    string // HMAC secret for webhook verification
}

// NewHandler creates a webhook handler.
func NewHandler(secret string, logger *slog.Logger) *Handler {
	return &Handler{
		secret: secret,
		logger: logger.With("component", "webhook"),
	}
}

// OnComplete registers a callback for webhook events.
func (h *Handler) OnComplete(cb func(CompletionEvent)) {
	h.callbacks = append(h.callbacks, cb)
}

// ServeHTTP implements http.Handler for incoming webhooks.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	if provider == "" {
		provider = r.URL.Query().Get("provider")
	}

	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	h.logger.Info("webhook received", "provider", provider)

	var event CompletionEvent
	var err error

	switch strings.ToLower(provider) {
	case "fal":
		event, err = parseFALWebhook(body)
	case "vidu":
		event, err = parseViduWebhook(body)
	case "minimax":
		event, err = parseMinimaxWebhook(body)
	case "ark":
		event, err = parseArkWebhook(body)
	default:
		http.Error(w, "unknown provider", http.StatusBadRequest)
		return
	}

	if err != nil {
		h.logger.Error("webhook parse error", "provider", provider, "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	event.Provider = provider
	event.RawData = body

	for _, cb := range h.callbacks {
		cb(event)
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "accepted"})
}

// --- Provider Parsers ---

func parseFALWebhook(data map[string]any) (CompletionEvent, error) {
	reqID, _ := data["request_id"].(string)
	status, _ := data["status"].(string)

	event := CompletionEvent{ExternalID: reqID}

	switch status {
	case "OK", "COMPLETED":
		event.Status = "completed"
		payload, _ := data["payload"].(map[string]any)
		images, _ := payload["images"].([]any)
		if len(images) > 0 {
			img, _ := images[0].(map[string]any)
			event.ResultURL, _ = img["url"].(string)
		}
		video, _ := payload["video"].(map[string]any)
		if url, ok := video["url"].(string); ok {
			event.ResultURL = url
		}
	default:
		event.Status = "failed"
		event.Error = fmt.Sprintf("FAL status: %s", status)
	}

	return event, nil
}

func parseViduWebhook(data map[string]any) (CompletionEvent, error) {
	taskID, _ := data["task_id"].(string)
	state, _ := data["state"].(string)

	event := CompletionEvent{ExternalID: fmt.Sprintf("VIDU:VIDEO:%s", taskID)}

	switch state {
	case "success":
		event.Status = "completed"
		creations, _ := data["creations"].([]any)
		if len(creations) > 0 {
			c, _ := creations[0].(map[string]any)
			event.ResultURL, _ = c["url"].(string)
		}
	default:
		event.Status = "failed"
		event.Error, _ = data["err_msg"].(string)
	}

	return event, nil
}

func parseMinimaxWebhook(data map[string]any) (CompletionEvent, error) {
	taskID, _ := data["task_id"].(string)
	status, _ := data["status"].(string)

	event := CompletionEvent{ExternalID: fmt.Sprintf("MINIMAX:VIDEO:%s", taskID)}

	switch status {
	case "Success":
		event.Status = "completed"
		fileID, _ := data["file_id"].(string)
		event.ResultURL = fmt.Sprintf("https://api.minimaxi.com/v1/files/retrieve?file_id=%s", fileID)
	default:
		event.Status = "failed"
		baseResp, _ := data["base_resp"].(map[string]any)
		event.Error, _ = baseResp["status_msg"].(string)
	}

	return event, nil
}

func parseArkWebhook(data map[string]any) (CompletionEvent, error) {
	taskID, _ := data["id"].(string)
	status, _ := data["status"].(string)

	event := CompletionEvent{ExternalID: fmt.Sprintf("ARK:VIDEO:%s", taskID)}

	switch status {
	case "succeeded":
		event.Status = "completed"
		content, _ := data["content"].(map[string]any)
		event.ResultURL, _ = content["video_url"].(string)
	default:
		event.Status = "failed"
		errInfo, _ := data["error"].(map[string]any)
		event.Error, _ = errInfo["message"].(string)
	}

	return event, nil
}
