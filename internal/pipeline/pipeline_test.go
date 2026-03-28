package pipeline

import (
	"testing"
)

// --- splitIntoScenes tests ---

func TestSplitIntoScenes_ShortText(t *testing.T) {
	scenes := splitIntoScenes("Hello world", 5)
	if len(scenes) != 1 {
		t.Fatalf("expected 1 scene for short text, got %d", len(scenes))
	}
	if scenes[0] != "Hello world" {
		t.Fatalf("expected original text, got %q", scenes[0])
	}
}

func TestSplitIntoScenes_MultipleSentences(t *testing.T) {
	text := "This is the first sentence that is long enough. This is the second one that is also long enough. The third sentence is here too."
	scenes := splitIntoScenes(text, 10)
	if len(scenes) < 2 {
		t.Fatalf("expected at least 2 scenes, got %d", len(scenes))
	}
}

func TestSplitIntoScenes_MergesWhenOverMax(t *testing.T) {
	text := "First sentence is here now. Second sentence follows next. Third sentence is also here. Fourth sentence appears next. Fifth sentence is the last."
	scenes := splitIntoScenes(text, 2)
	if len(scenes) != 2 {
		t.Fatalf("expected 2 merged scenes, got %d", len(scenes))
	}
}

func TestSplitIntoScenes_Empty(t *testing.T) {
	scenes := splitIntoScenes("", 5)
	if len(scenes) != 1 {
		t.Fatalf("expected 1 scene for empty text, got %d", len(scenes))
	}
}

// --- stripCodeFences tests ---

func TestStripCodeFences_WithFences(t *testing.T) {
	input := "```json\n{\"key\": \"value\"}\n```"
	got := stripCodeFences(input)
	want := "{\"key\": \"value\"}"
	if got != want {
		t.Fatalf("stripCodeFences with fences = %q, want %q", got, want)
	}
}

func TestStripCodeFences_WithoutFences(t *testing.T) {
	input := `{"key": "value"}`
	got := stripCodeFences(input)
	if got != input {
		t.Fatalf("stripCodeFences without fences = %q, want %q", got, input)
	}
}

func TestStripCodeFences_OnlyOpeningFence(t *testing.T) {
	input := "```json\n{\"key\": \"value\"}"
	got := stripCodeFences(input)
	want := "{\"key\": \"value\"}"
	if got != want {
		t.Fatalf("stripCodeFences only opening = %q, want %q", got, want)
	}
}

// --- buildStageInput tests ---

func TestBuildStageInput(t *testing.T) {
	req := &PipelineRequest{
		Story:        "Once upon a time",
		InputType:    "novel",
		Budget:       "medium",
		QualityLevel: "standard",
		Analysis:     map[string]any{"theme": "adventure"},
		Characters:   map[string]any{"hero": "Alice"},
		Locations:    map[string]any{"castle": "big"},
		Clips:        []ClipData{{ID: "c1"}},
		Screenplays:  []ScreenplayData{{ClipID: "c1"}},
		Storyboard:   map[string]any{"panels": []any{}},
		Media:        map[string]any{"images": []any{}},
		Voices:       map[string]any{"narration": "done"},
	}

	tests := []struct {
		stage Stage
		key   string // check a key exists in output
	}{
		{StageAnalysis, "story"},
		{StagePlanning, "analysis"},
		{StageCharacters, "story"},
		{StageLocations, "analysis"},
		{StageSegmentation, "characters"},
		{StageScreenplay, "clips"},
		{StageStoryboard, "screenplays"},
		{StageMediaGen, "storyboard"},
		{StageQualityCheck, "media"},
		{StageVoice, "storyboard"},
		{StageAssembly, "voices"},
	}

	for _, tt := range tests {
		t.Run(string(tt.stage), func(t *testing.T) {
			result := buildStageInput(tt.stage, req)
			if result == nil {
				t.Fatalf("buildStageInput(%s) returned nil", tt.stage)
			}
			if _, ok := result[tt.key]; !ok {
				t.Fatalf("buildStageInput(%s) missing key %q", tt.stage, tt.key)
			}
		})
	}

	// Unknown stage
	if result := buildStageInput("nonexistent", req); result != nil {
		t.Fatalf("expected nil for unknown stage, got %v", result)
	}
}

// --- extractFALURL tests ---

func TestExtractFALURL_Image(t *testing.T) {
	data := map[string]any{
		"images": []any{
			map[string]any{"url": "https://example.com/img.png"},
		},
	}
	got := extractFALURL(data, "IMAGE")
	want := "https://example.com/img.png"
	if got != want {
		t.Fatalf("extractFALURL IMAGE = %q, want %q", got, want)
	}
}

func TestExtractFALURL_Video(t *testing.T) {
	data := map[string]any{
		"video": map[string]any{"url": "https://example.com/vid.mp4"},
	}
	got := extractFALURL(data, "VIDEO")
	want := "https://example.com/vid.mp4"
	if got != want {
		t.Fatalf("extractFALURL VIDEO = %q, want %q", got, want)
	}
}

func TestExtractFALURL_Empty(t *testing.T) {
	data := map[string]any{}
	got := extractFALURL(data, "IMAGE")
	if got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

// --- stageIndexOf tests ---

func TestStageIndexOf_Known(t *testing.T) {
	tests := []struct {
		stage Stage
		want  int
	}{
		{StageAnalysis, 0},
		{StageAssembly, 10},
		{StageMediaGen, 7},
	}
	for _, tt := range tests {
		got := stageIndexOf(tt.stage)
		if got != tt.want {
			t.Errorf("stageIndexOf(%q) = %d, want %d", tt.stage, got, tt.want)
		}
	}
}

func TestStageIndexOf_Unknown(t *testing.T) {
	got := stageIndexOf("nonexistent")
	if got != -1 {
		t.Fatalf("expected -1 for unknown stage, got %d", got)
	}
}

// --- NATSSubject tests ---

func TestNATSSubject(t *testing.T) {
	got := NATSSubject("proj-123")
	want := "pipeline.proj-123.events"
	if got != want {
		t.Fatalf("NATSSubject = %q, want %q", got, want)
	}
}

func TestNATSSubjectForType(t *testing.T) {
	got := NATSSubjectForType("proj-123", EventStageCompleted)
	want := "pipeline.proj-123.stage.completed"
	if got != want {
		t.Fatalf("NATSSubjectForType = %q, want %q", got, want)
	}
}
