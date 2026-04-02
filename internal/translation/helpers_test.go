package translation

import (
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
