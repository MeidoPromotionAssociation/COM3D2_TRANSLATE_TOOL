package translation

import (
	"context"
	"fmt"
	"strings"

	"COM3D2TranslateTool/internal/model"
)

const translateLogContentLimit = 32 * 1024

func EmitLLMRequestLog(ctx context.Context, translatorName, endpoint, modelName, prompt, userPayload string, req Request) {
	content := strings.TrimSpace(fmt.Sprintf(
		"Endpoint: %s\nModel: %s\nTarget Field: %s\nBatch: %s\n\n[System Prompt]\n%s\n\n[User Payload]\n%s",
		endpoint,
		modelName,
		NormalizeTargetField(req.TargetField),
		describeTranslationBatch(req.Items),
		strings.TrimSpace(prompt),
		strings.TrimSpace(userPayload),
	))

	EmitLog(ctx, model.TranslateLog{
		Translator: translatorName,
		Kind:       "request",
		Title:      "LLM Request",
		Content:    truncateTranslateLogContent(content),
	})
}

func EmitLLMResponseLog(ctx context.Context, translatorName string, req Request, output string) {
	content := strings.TrimSpace(fmt.Sprintf(
		"Batch: %s\n\n[Assistant Output]\n%s",
		describeTranslationBatch(req.Items),
		strings.TrimSpace(output),
	))

	EmitLog(ctx, model.TranslateLog{
		Translator: translatorName,
		Kind:       "response",
		Title:      "LLM Response",
		Content:    truncateTranslateLogContent(content),
	})
}

func EmitLLMErrorLog(ctx context.Context, translatorName, stage string, req Request, err error) {
	if err == nil {
		return
	}

	content := strings.TrimSpace(fmt.Sprintf(
		"Stage: %s\nBatch: %s\nError: %v",
		stage,
		describeTranslationBatch(req.Items),
		err,
	))

	EmitLog(ctx, model.TranslateLog{
		Translator: translatorName,
		Kind:       "error",
		Title:      "LLM Error",
		Content:    truncateTranslateLogContent(content),
	})
}

func describeTranslationBatch(items []Item) string {
	if len(items) == 0 {
		return "0 items"
	}

	commonArc := items[0].SourceArc
	commonFile := items[0].SourceFile
	for _, item := range items[1:] {
		if item.SourceArc != commonArc {
			commonArc = ""
		}
		if item.SourceFile != commonFile {
			commonFile = ""
		}
	}

	parts := make([]string, 0, 3)
	if strings.TrimSpace(commonArc) != "" {
		parts = append(parts, commonArc)
	}
	if strings.TrimSpace(commonFile) != "" {
		parts = append(parts, commonFile)
	}
	parts = append(parts, fmt.Sprintf("%d items", len(items)))
	return strings.Join(parts, " / ")
}

func truncateTranslateLogContent(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) <= translateLogContentLimit {
		return trimmed
	}
	return trimmed[:translateLogContentLimit] + "\n\n...[truncated]"
}
