package translation

import (
	"encoding/json"
	"strings"
	"testing"

	"COM3D2TranslateTool/internal/model"
)

func TestResolveLLMPromptIncludesOnlyRelevantJSONGlossaryEntries(t *testing.T) {
	req := Request{
		Settings: model.TranslationSettings{
			SourceLanguage: "ja",
			TargetLanguage: "zh-CN",
			Glossary: `[
  {"source":"maid","preferred":"女仆","note":"use this consistently"},
  {"source":"unused","preferred":"ignore me"},
  {"source":"[SF]","note":"current heroine in this scene"}
]`,
		},
		Items: []Item{
			{
				Role:       "[SF]",
				SourceText: "the maid arrives",
			},
		},
		TargetField: "translated",
	}

	prompt := resolveLLMPrompt(req, "")
	if !strings.Contains(prompt, "\"source\": \"maid\"") {
		t.Fatalf("expected matching source glossary entry, got %q", prompt)
	}
	if !strings.Contains(prompt, "\"source\": \"[SF]\"") {
		t.Fatalf("expected speaker-matching glossary entry, got %q", prompt)
	}
	if strings.Contains(prompt, "\"source\": \"unused\"") {
		t.Fatalf("did not expect unmatched glossary entry, got %q", prompt)
	}
}

func TestResolveLLMPromptSupportsLegacyGlossaryFormat(t *testing.T) {
	req := Request{
		Settings: model.TranslationSettings{
			SourceLanguage: "ja",
			TargetLanguage: "zh-CN",
			Glossary:       "maid\t女仆\tuse this consistently\nunused\tignore me\n",
		},
		Items: []Item{
			{
				SourceText: "the maid arrives",
			},
		},
		TargetField: "translated",
	}

	prompt := resolveLLMPrompt(req, "")
	if !strings.Contains(prompt, "\"source\": \"maid\"") {
		t.Fatalf("expected matching legacy glossary entry, got %q", prompt)
	}
	if !strings.Contains(prompt, "\"preferred\": \"女仆\"") {
		t.Fatalf("expected preferred translation from legacy glossary, got %q", prompt)
	}
	if strings.Contains(prompt, "\"source\": \"unused\"") {
		t.Fatalf("did not expect unmatched legacy glossary entry, got %q", prompt)
	}
}

func TestRenderPromptTemplateGlossaryPlaceholderUsesFilteredJSON(t *testing.T) {
	req := Request{
		Settings: model.TranslationSettings{
			SourceLanguage: "ja",
			TargetLanguage: "zh-CN",
			Glossary: `[
  {"source":"maid","preferred":"女仆"},
  {"source":"unused","preferred":"ignore me"}
]`,
		},
		Items: []Item{
			{
				SourceText: "maid",
			},
		},
		TargetField: "translated",
	}

	rendered := renderPromptTemplate(req, "Task\n\n{{glossary}}", glossaryInstruction(req))
	if !strings.Contains(rendered, "\"source\": \"maid\"") {
		t.Fatalf("expected filtered glossary JSON in custom prompt, got %q", rendered)
	}
	if strings.Contains(rendered, "\"source\": \"unused\"") {
		t.Fatalf("did not expect unmatched glossary JSON in custom prompt, got %q", rendered)
	}
}

func TestNormalizeTargetFieldSupportsSourceText(t *testing.T) {
	if got := NormalizeTargetField("source_text"); got != "source_text" {
		t.Fatalf("expected source_text target field, got %q", got)
	}
	if got := targetFieldLabel("source_text"); got != "source_text" {
		t.Fatalf("expected source_text label, got %q", got)
	}
}

func TestResolveLLMPromptAddsASRWarningForRecognizedPlayvoiceText(t *testing.T) {
	req := Request{
		Settings: model.TranslationSettings{
			SourceLanguage: "ja",
			TargetLanguage: "zh-CN",
		},
		Items: []Item{
			{
				Type:       "playvoice_notext",
				VoiceID:    "V_0001",
				SourceFile: "scene01.ks",
				SourceText: "音声识别得到的文本",
			},
		},
		TargetField: "translated",
	}

	prompt := resolveLLMPrompt(req, "")
	if !strings.Contains(prompt, "source_text_is_asr=true") {
		t.Fatalf("expected ASR warning in prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, "speech recognition") {
		t.Fatalf("expected speech recognition guidance in prompt, got %q", prompt)
	}
}

func TestBuildLLMUserPayloadMarksASRDerivedSourceText(t *testing.T) {
	req := Request{
		Settings: model.TranslationSettings{
			SourceLanguage: "ja",
			TargetLanguage: "zh-CN",
		},
		Items: []Item{
			{
				ID:         1,
				Type:       "playvoice_notext",
				VoiceID:    "V_0001",
				SourceText: "识别文本",
			},
			{
				ID:         2,
				Type:       "dialogue",
				SourceText: "普通文本",
			},
		},
		TargetField: "translated",
	}

	raw, err := buildLLMUserPayload(req)
	if err != nil {
		t.Fatalf("build llm user payload: %v", err)
	}

	var payload struct {
		Items []struct {
			ID              int64 `json:"id"`
			SourceTextIsASR bool  `json:"source_text_is_asr"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(payload.Items) != 2 {
		t.Fatalf("expected 2 items in payload, got %#v", payload.Items)
	}
	if !payload.Items[0].SourceTextIsASR {
		t.Fatalf("expected first item to be marked as ASR-derived, got %#v", payload.Items)
	}
	if payload.Items[1].SourceTextIsASR {
		t.Fatalf("expected second item to remain unmarked, got %#v", payload.Items)
	}
}
