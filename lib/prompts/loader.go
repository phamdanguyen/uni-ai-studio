// Package prompts provides a file-based prompt loader with template rendering
// and in-memory caching for the WAOO Studio agent system.
//
// Prompts are stored as .txt files under lib/prompts/{category}/{name}.en.txt
// and can contain template variables in the form {variable_name} that are
// replaced at render time.
package prompts

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// promptsDir is resolved once at init time relative to this source file.
var promptsDir string

func init() {
	_, thisFile, _, _ := runtime.Caller(0)
	promptsDir = filepath.Dir(thisFile)
}

// cache stores loaded prompt templates to avoid repeated disk reads.
var cache sync.Map // map[string]string

// cacheKey builds a unique key for the cache.
func cacheKey(category, name, lang string) string {
	return category + "/" + name + "." + lang
}

// Load reads a prompt file from disk by category, name, and language.
// Files are expected at lib/prompts/{category}/{name}.{lang}.txt
//
// Results are cached in memory after the first load.
func Load(category, name, lang string) (string, error) {
	key := cacheKey(category, name, lang)

	// Check cache first
	if v, ok := cache.Load(key); ok {
		return v.(string), nil
	}

	// Build file path
	filename := fmt.Sprintf("%s.%s.txt", name, lang)
	path := filepath.Join(promptsDir, category, filename)

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("prompts.Load(%s/%s.%s): %w", category, name, lang, err)
	}

	content := strings.TrimSpace(string(data))
	cache.Store(key, content)
	return content, nil
}

// MustLoad loads a prompt file and panics if it cannot be found.
// Use this for prompts that are required at startup.
func MustLoad(category, name string) string {
	content, err := Load(category, name, "en")
	if err != nil {
		panic(fmt.Sprintf("required prompt not found: %s/%s — %v", category, name, err))
	}
	return content
}

// Render replaces all {variable} placeholders in the template with
// corresponding values from the vars map.
//
// Variables not present in vars are left as-is (no error).
func Render(template string, vars map[string]string) string {
	result := template
	for k, v := range vars {
		result = strings.ReplaceAll(result, "{"+k+"}", v)
	}
	return result
}

// LoadAndRender is a convenience function that loads a prompt and immediately
// renders it with the provided variables.
func LoadAndRender(category, name string, vars map[string]string) (string, error) {
	tmpl, err := Load(category, name, "en")
	if err != nil {
		return "", err
	}
	return Render(tmpl, vars), nil
}

// MustLoadAndRender loads and renders a prompt, panicking on load failure.
func MustLoadAndRender(category, name string, vars map[string]string) string {
	tmpl := MustLoad(category, name)
	return Render(tmpl, vars)
}

// Categories
const (
	CategoryNovelPromotion    = "novel-promotion"
	CategoryCharacterReference = "character-reference"
)

// Prompt names — Novel Promotion pipeline
const (
	// Director / Analysis prompts
	PromptEpisodeSplit         = "episode_split"
	PromptClipSegmentation     = "agent_clip"
	PromptScreenplayConversion = "screenplay_conversion"

	// Character prompts
	PromptCharacterProfile           = "agent_character_profile"
	PromptCharacterVisual            = "agent_character_visual"
	PromptCharacterCreate            = "character_create"
	PromptCharacterModify            = "character_modify"
	PromptCharacterRegenerate        = "character_regenerate"
	PromptCharacterDescriptionUpdate = "character_description_update"

	// Location prompts
	PromptSelectLocation           = "select_location"
	PromptLocationCreate           = "location_create"
	PromptLocationModify           = "location_modify"
	PromptLocationRegenerate       = "location_regenerate"
	PromptLocationDescriptionUpdate = "location_description_update"

	// Storyboard prompts
	PromptStoryboardPlan     = "agent_storyboard_plan"
	PromptStoryboardDetail   = "agent_storyboard_detail"
	PromptStoryboardInsert   = "agent_storyboard_insert"
	PromptCinematographer    = "agent_cinematographer"
	PromptActingDirection    = "agent_acting_direction"
	PromptShotVariantAnalysis = "agent_shot_variant_analysis"
	PromptShotVariantGenerate = "agent_shot_variant_generate"

	// Media prompts
	PromptSinglePanelImage = "single_panel_image"
	PromptImagePromptModify = "image_prompt_modify"
	PromptStoryboardEdit   = "storyboard_edit"

	// Voice prompts
	PromptVoiceAnalysis = "voice_analysis"
)

// Prompt names — Character Reference
const (
	PromptImageToDescription  = "character_image_to_description"
	PromptReferenceToSheet    = "character_reference_to_sheet"
)
