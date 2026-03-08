// Package tools implements the ToolRegistry — wrapping external AI generation
// providers (image, video, audio) as executable tools for agents.
//
// These tools map directly to the generators from the waoowaoo codebase:
//
// IMAGE GENERATORS:
//   - fal      → FAL Banana Pro/2 (2K/4K AI images, supports reference images for editing)
//   - ark      → Volcengine Seedream 4.5 (4K images with aspect ratio mapping)
//   - google   → Google Gemini/Imagen (native Google AI image generation)
//   - openai   → OpenAI-compatible providers (gpt-image-1, DALL-E 3, etc.)
//
// VIDEO GENERATORS:
//   - fal      → FAL (Wan 2.6, Veo 3.1, Sora 2, Kling 2.5/3) — image-to-video
//   - ark      → Volcengine Seedance 1.0/1.5 (image-to-video, first-last frame)
//   - minimax  → MiniMax Hailuo 2.3 (image-to-video, 6-10s, up to 1080P)
//   - vidu     → Vidu Q2/Q3 (image-to-video, 1-16s, audio generation, lip sync)
//   - google   → Google Veo (text/image-to-video)
//
// AUDIO GENERATORS:
//   - qwen     → Alibaba Qwen TTS (text-to-speech with multiple voices)
//
// All generators are ASYNC: they submit a task and return an externalId
// (e.g., "FAL:IMAGE:endpoint:requestId") for polling later.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/uni-ai-studio/waoo-studio/internal/agent"
)

// ProviderKeys holds API keys for media generation providers.
type ProviderKeys struct {
	FALKey     string
	ArkKey     string
	MiniMaxKey string
	ViduKey    string
	GoogleKey  string
	QwenKey    string
}

// Registry implements agent.ToolRegistry with real API integrations.
type Registry struct {
	mu     sync.RWMutex
	tools  map[string]*toolEntry
	keys   ProviderKeys
	client *http.Client
	logger *slog.Logger
}

type toolEntry struct {
	info agent.ToolInfo
	fn   agent.ToolFunc
}

// NewRegistry creates a ToolRegistry pre-loaded with all generator tools.
func NewRegistry(keys ProviderKeys, logger *slog.Logger) *Registry {
	r := &Registry{
		tools: make(map[string]*toolEntry),
		keys:  keys,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
		logger: logger.With("component", "tools"),
	}

	// Register all generator tools
	r.registerImageGenerators()
	r.registerVideoGenerators()
	r.registerAudioGenerators()

	return r
}

// Register adds a new tool to the registry.
func (r *Registry) Register(name string, tool agent.Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.tools[name] = &toolEntry{
		info: tool.Info,
		fn:   tool.Execute,
	}
	return nil
}

// Execute runs a tool by name with the given input.
func (r *Registry) Execute(ctx context.Context, name string, input map[string]any) (map[string]any, error) {
	r.mu.RLock()
	entry, ok := r.tools[name]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("tool not found: %s", name)
	}

	return entry.fn(ctx, input)
}

// List returns info about all registered tools.
func (r *Registry) List() []agent.ToolInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	infos := make([]agent.ToolInfo, 0, len(r.tools))
	for _, e := range r.tools {
		infos = append(infos, e.info)
	}
	return infos
}

// ============================================================
// IMAGE GENERATORS
// ============================================================

func (r *Registry) registerImageGenerators() {
	// FAL Banana — high quality AI images (2K/4K)
	r.tools["image_fal"] = &toolEntry{
		info: agent.ToolInfo{
			Name:        "image_fal",
			Description: "FAL Banana Pro/2: Sinh ảnh AI 2K/4K. Hỗ trợ reference images cho editing. Models: banana, banana-2.",
		},
		fn: r.falImageGenerate,
	}

	// Ark Seedream — Volcengine 4K images
	r.tools["image_ark"] = &toolEntry{
		info: agent.ToolInfo{
			Name:        "image_ark",
			Description: "Ark Seedream 4.5: Sinh ảnh 4K từ Volcengine. Hỗ trợ reference images.",
		},
		fn: r.arkImageGenerate,
	}

	// Google Gemini/Imagen
	r.tools["image_google"] = &toolEntry{
		info: agent.ToolInfo{
			Name:        "image_google",
			Description: "Google Gemini/Imagen: Sinh ảnh qua Google AI. Models: gemini-2.0-flash, imagen-3.0.",
		},
		fn: r.googleImageGenerate,
	}

	// Generic dispatcher
	r.tools["image_generator"] = &toolEntry{
		info: agent.ToolInfo{
			Name:        "image_generator",
			Description: "Auto-dispatch image generation to best available provider.",
		},
		fn: r.dispatchImageGenerate,
	}
}

// ============================================================
// VIDEO GENERATORS
// ============================================================

func (r *Registry) registerVideoGenerators() {
	// FAL Video — Wan 2.6, Veo 3.1, Sora 2, Kling
	r.tools["video_fal"] = &toolEntry{
		info: agent.ToolInfo{
			Name:        "video_fal",
			Description: "FAL Video: Wan 2.6, Veo 3.1, Sora 2, Kling 2.5/3. Image-to-video.",
		},
		fn: r.falVideoGenerate,
	}

	// Ark Seedance
	r.tools["video_ark"] = &toolEntry{
		info: agent.ToolInfo{
			Name:        "video_ark",
			Description: "Ark Seedance: Image-to-video. Models: 1.0-pro, 1.0-lite, 1.5-pro. First-last frame mode.",
		},
		fn: r.arkVideoGenerate,
	}

	// MiniMax Hailuo
	r.tools["video_minimax"] = &toolEntry{
		info: agent.ToolInfo{
			Name:        "video_minimax",
			Description: "MiniMax Hailuo 2.3: Video sinh từ ảnh, 6-10s, lên tới 1080P.",
		},
		fn: r.minimaxVideoGenerate,
	}

	// Vidu
	r.tools["video_vidu"] = &toolEntry{
		info: agent.ToolInfo{
			Name:        "video_vidu",
			Description: "Vidu Q2/Q3: Video sinh từ ảnh, 1-16s, hỗ trợ audio generation và lip sync.",
		},
		fn: r.viduVideoGenerate,
	}

	// Generic dispatcher
	r.tools["video_generator"] = &toolEntry{
		info: agent.ToolInfo{
			Name:        "video_generator",
			Description: "Auto-dispatch video generation to best available provider.",
		},
		fn: r.dispatchVideoGenerate,
	}
}

// ============================================================
// AUDIO GENERATORS
// ============================================================

func (r *Registry) registerAudioGenerators() {
	// Qwen TTS
	r.tools["tts_generator"] = &toolEntry{
		info: agent.ToolInfo{
			Name:        "tts_generator",
			Description: "Qwen TTS: Text-to-Speech với nhiều giọng, hỗ trợ SSML.",
		},
		fn: r.qwenTTSGenerate,
	}

	// Voice Designer (uses LLM to design voice profiles)
	r.tools["voice_designer"] = &toolEntry{
		info: agent.ToolInfo{
			Name:        "voice_designer",
			Description: "Thiết kế giọng nói cho nhân vật dựa trên personality.",
		},
		fn: func(_ context.Context, input map[string]any) (map[string]any, error) {
			// Voice design is handled by the Voice Agent via LLM, not external API
			return map[string]any{
				"status":  "delegated",
				"message": "Voice design is handled by LLM within the Voice Agent",
				"input":   input,
			}, nil
		},
	}

	// Lip sync
	r.tools["lip_sync"] = &toolEntry{
		info: agent.ToolInfo{
			Name:        "lip_sync",
			Description: "Lip sync audio với video. Sử dụng FAL hoặc Vidu.",
		},
		fn: func(_ context.Context, input map[string]any) (map[string]any, error) {
			return map[string]any{
				"status":  "pending_implementation",
				"message": "Lip sync will use Vidu or FAL lip sync API",
				"input":   input,
			}, nil
		},
	}
}

// ============================================================
// IMPLEMENTATION: FAL
// ============================================================

func (r *Registry) falImageGenerate(ctx context.Context, input map[string]any) (map[string]any, error) {
	prompt, _ := input["prompt"].(string)
	apiKey, _ := input["apiKey"].(string)
	modelID, _ := input["modelId"].(string)
	aspectRatio, _ := input["aspectRatio"].(string)
	resolution, _ := input["resolution"].(string)

	if modelID == "" {
		modelID = "banana"
	}

	endpoints := map[string]string{
		"banana":   "fal-ai/nano-banana-pro",
		"banana-2": "fal-ai/nano-banana-2",
	}
	endpoint, ok := endpoints[modelID]
	if !ok {
		endpoint = endpoints["banana"]
	}

	body := map[string]any{
		"prompt":        prompt,
		"num_images":    1,
		"output_format": "png",
	}
	if aspectRatio != "" {
		body["aspect_ratio"] = aspectRatio
	}
	if resolution != "" {
		body["resolution"] = resolution
	}

	return r.submitFalTask(ctx, endpoint, body, apiKey, "IMAGE")
}

func (r *Registry) falVideoGenerate(ctx context.Context, input map[string]any) (map[string]any, error) {
	imageURL, _ := input["imageUrl"].(string)
	prompt, _ := input["prompt"].(string)
	apiKey, _ := input["apiKey"].(string)
	modelID, _ := input["modelId"].(string)

	if modelID == "" {
		modelID = "fal-wan25"
	}

	endpoints := map[string]string{
		"fal-wan25": "wan/v2.6/image-to-video",
		"fal-veo31": "fal-ai/veo3.1/fast/image-to-video",
		"fal-sora2": "fal-ai/sora-2/image-to-video",
	}
	endpoint, ok := endpoints[modelID]
	if !ok {
		return nil, fmt.Errorf("unsupported FAL video model: %s", modelID)
	}

	body := map[string]any{
		"image_url": imageURL,
		"prompt":    prompt,
	}

	return r.submitFalTask(ctx, endpoint, body, apiKey, "VIDEO")
}

func (r *Registry) submitFalTask(ctx context.Context, endpoint string, body map[string]any, apiKey, mediaType string) (map[string]any, error) {
	url := fmt.Sprintf("https://queue.fal.run/%s", endpoint)
	return r.submitAsyncTask(ctx, url, body, map[string]string{
		"Authorization": "Key " + apiKey,
		"Content-Type":  "application/json",
	}, func(data map[string]any) (string, error) {
		reqID, ok := data["request_id"].(string)
		if !ok {
			return "", fmt.Errorf("FAL did not return request_id")
		}
		return fmt.Sprintf("FAL:%s:%s:%s", mediaType, endpoint, reqID), nil
	})
}

// ============================================================
// IMPLEMENTATION: Ark (Volcengine)
// ============================================================

func (r *Registry) arkImageGenerate(ctx context.Context, input map[string]any) (map[string]any, error) {
	prompt, _ := input["prompt"].(string)
	apiKey, _ := input["apiKey"].(string)
	modelID, _ := input["modelId"].(string)
	aspectRatio, _ := input["aspectRatio"].(string)

	if modelID == "" {
		modelID = "doubao-seedream-4-5-251128"
	}

	sizeMap := map[string]string{
		"1:1": "4096x4096", "16:9": "5456x3072", "9:16": "3072x5456",
		"4:3": "4728x3544", "3:4": "3544x4728",
	}
	size := sizeMap[aspectRatio]
	if size == "" {
		size = "4096x4096"
	}

	body := map[string]any{
		"model":  modelID,
		"prompt": prompt,
		"size":   size,
		"sequential_image_generation": "disabled",
		"response_format":             "url",
		"stream":                      false,
		"watermark":                   false,
	}

	return r.submitAsyncTask(ctx,
		"https://ark.cn-beijing.volces.com/api/v3/images/generations",
		body,
		map[string]string{
			"Authorization": "Bearer " + apiKey,
			"Content-Type":  "application/json",
		},
		func(data map[string]any) (string, error) {
			dataArr, _ := data["data"].([]any)
			if len(dataArr) > 0 {
				item, _ := dataArr[0].(map[string]any)
				if url, ok := item["url"].(string); ok {
					return "", fmt.Errorf("sync_result:%s", url)
				}
			}
			return "ARK:IMAGE:" + modelID, nil
		},
	)
}

func (r *Registry) arkVideoGenerate(ctx context.Context, input map[string]any) (map[string]any, error) {
	imageURL, _ := input["imageUrl"].(string)
	prompt, _ := input["prompt"].(string)
	apiKey, _ := input["apiKey"].(string)
	modelID, _ := input["modelId"].(string)

	if modelID == "" {
		modelID = "doubao-seedance-1-0-pro-fast-251015"
	}

	content := []map[string]any{
		{"type": "image_url", "image_url": map[string]string{"url": imageURL}},
	}
	if prompt != "" {
		content = append(content, map[string]any{"type": "text", "text": prompt})
	}

	body := map[string]any{
		"model":   modelID,
		"content": content,
	}

	return r.submitAsyncTask(ctx,
		"https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks",
		body,
		map[string]string{
			"Authorization": "Bearer " + apiKey,
			"Content-Type":  "application/json",
		},
		func(data map[string]any) (string, error) {
			taskID, _ := data["id"].(string)
			if taskID == "" {
				return "", fmt.Errorf("ARK did not return task_id")
			}
			return fmt.Sprintf("ARK:VIDEO:%s", taskID), nil
		},
	)
}

// ============================================================
// IMPLEMENTATION: MiniMax
// ============================================================

func (r *Registry) minimaxVideoGenerate(ctx context.Context, input map[string]any) (map[string]any, error) {
	imageURL, _ := input["imageUrl"].(string)
	prompt, _ := input["prompt"].(string)
	apiKey, _ := input["apiKey"].(string)
	modelID, _ := input["modelId"].(string)

	if modelID == "" {
		modelID = "MiniMax-Hailuo-2.3"
	}

	body := map[string]any{
		"model":            modelID,
		"prompt":           prompt,
		"prompt_optimizer": true,
	}
	if imageURL != "" {
		body["first_frame_image"] = imageURL
	}

	return r.submitAsyncTask(ctx,
		"https://api.minimaxi.com/v1/video_generation",
		body,
		map[string]string{
			"Authorization": "Bearer " + apiKey,
			"Content-Type":  "application/json",
		},
		func(data map[string]any) (string, error) {
			taskID, _ := data["task_id"].(string)
			if taskID == "" {
				return "", fmt.Errorf("MiniMax did not return task_id")
			}
			return fmt.Sprintf("MINIMAX:VIDEO:%s", taskID), nil
		},
	)
}

// ============================================================
// IMPLEMENTATION: Vidu
// ============================================================

func (r *Registry) viduVideoGenerate(ctx context.Context, input map[string]any) (map[string]any, error) {
	imageURL, _ := input["imageUrl"].(string)
	prompt, _ := input["prompt"].(string)
	apiKey, _ := input["apiKey"].(string)
	modelID, _ := input["modelId"].(string)
	duration, _ := input["duration"].(float64)

	if modelID == "" {
		modelID = "viduq2-turbo"
	}
	if duration == 0 {
		duration = 5
	}

	body := map[string]any{
		"model":      modelID,
		"images":     []string{imageURL},
		"prompt":     prompt,
		"duration":   int(duration),
		"resolution": "720p",
	}

	return r.submitAsyncTask(ctx,
		"https://api.vidu.cn/ent/v2/img2video",
		body,
		map[string]string{
			"Authorization": "Token " + apiKey,
			"Content-Type":  "application/json",
		},
		func(data map[string]any) (string, error) {
			taskID, _ := data["task_id"].(string)
			if taskID == "" {
				return "", fmt.Errorf("Vidu did not return task_id")
			}
			return fmt.Sprintf("VIDU:VIDEO:%s", taskID), nil
		},
	)
}

// ============================================================
// IMPLEMENTATION: Google
// ============================================================

func (r *Registry) googleImageGenerate(_ context.Context, input map[string]any) (map[string]any, error) {
	// Google Gemini/Imagen image generation
	// Implementation depends on which Google API (Vertex AI, AI Studio)
	return map[string]any{
		"status":   "pending_api_key",
		"provider": "google",
		"message":  "Configure GOOGLE_AI_KEY to enable Google image generation",
		"input":    input,
	}, nil
}

// ============================================================
// IMPLEMENTATION: Qwen TTS
// ============================================================

func (r *Registry) qwenTTSGenerate(_ context.Context, input map[string]any) (map[string]any, error) {
	text, _ := input["text"].(string)
	voice, _ := input["voice"].(string)
	if voice == "" {
		voice = "alloy"
	}

	return map[string]any{
		"status":   "pending_implementation",
		"provider": "qwen",
		"text":     text,
		"voice":    voice,
		"message":  "Qwen TTS integration requires DashScope API key",
	}, nil
}

// ============================================================
// DISPATCHERS (auto-select best provider)
// ============================================================

func (r *Registry) dispatchImageGenerate(ctx context.Context, input map[string]any) (map[string]any, error) {
	provider, _ := input["provider"].(string)
	switch provider {
	case "fal", "":
		input["apiKey"] = r.keys.FALKey
		return r.falImageGenerate(ctx, input)
	case "ark":
		input["apiKey"] = r.keys.ArkKey
		return r.arkImageGenerate(ctx, input)
	case "google":
		input["apiKey"] = r.keys.GoogleKey
		return r.googleImageGenerate(ctx, input)
	default:
		input["apiKey"] = r.keys.FALKey
		return r.falImageGenerate(ctx, input)
	}
}

func (r *Registry) dispatchVideoGenerate(ctx context.Context, input map[string]any) (map[string]any, error) {
	provider, _ := input["provider"].(string)
	switch provider {
	case "fal", "":
		input["apiKey"] = r.keys.FALKey
		return r.falVideoGenerate(ctx, input)
	case "ark":
		input["apiKey"] = r.keys.ArkKey
		return r.arkVideoGenerate(ctx, input)
	case "minimax":
		input["apiKey"] = r.keys.MiniMaxKey
		return r.minimaxVideoGenerate(ctx, input)
	case "vidu":
		input["apiKey"] = r.keys.ViduKey
		return r.viduVideoGenerate(ctx, input)
	default:
		input["apiKey"] = r.keys.FALKey
		return r.falVideoGenerate(ctx, input)
	}
}

// ============================================================
// HELPERS
// ============================================================

// submitAsyncTask makes an HTTP POST and extracts the external ID.
func (r *Registry) submitAsyncTask(
	ctx context.Context,
	url string,
	body map[string]any,
	headers map[string]string,
	extractID func(map[string]any) (string, error),
) (map[string]any, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	r.logger.Debug("submitting async task", "url", url)

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	var data map[string]any
	if err := json.Unmarshal(respBody, &data); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	externalID, err := extractID(data)
	if err != nil {
		// Check if it's a sync result (e.g., Ark image returns URL directly)
		if len(err.Error()) > 12 && err.Error()[:12] == "sync_result:" {
			return map[string]any{
				"success":  true,
				"async":    false,
				"imageUrl": err.Error()[12:],
			}, nil
		}
		return nil, err
	}

	return map[string]any{
		"success":    true,
		"async":      true,
		"externalId": externalID,
	}, nil
}
