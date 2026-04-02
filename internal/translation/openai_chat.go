package translation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"COM3D2TranslateTool/internal/model"
)

type OpenAIChatTranslator struct{}

func (OpenAIChatTranslator) Name() string {
	return "openai-chat"
}

func (OpenAIChatTranslator) Translate(ctx context.Context, req Request) ([]Result, error) {
	settings := model.NormalizeTranslationSettings(req.Settings)
	cfg := settings.OpenAIChat
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("openai chat api key is required")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("openai chat model is required")
	}
	if len(req.Items) == 0 {
		return nil, nil
	}

	userPayload, err := buildLLMUserPayload(req)
	if err != nil {
		return nil, err
	}

	body, err := mergeExtraJSON(cfg.ExtraJSON)
	if err != nil {
		return nil, err
	}
	prompt := resolveLLMPrompt(req, cfg.Prompt)
	body["model"] = cfg.Model
	body["messages"] = []map[string]any{
		{
			"role":    "system",
			"content": prompt,
		},
		{
			"role":    "user",
			"content": userPayload,
		},
	}
	if cfg.Temperature != nil {
		body["temperature"] = *cfg.Temperature
	}
	if cfg.TopP != nil {
		body["top_p"] = *cfg.TopP
	}
	if cfg.PresencePenalty != nil {
		body["presence_penalty"] = *cfg.PresencePenalty
	}
	if cfg.FrequencyPenalty != nil {
		body["frequency_penalty"] = *cfg.FrequencyPenalty
	}
	if cfg.MaxOutputTokens != nil {
		body["max_completion_tokens"] = *cfg.MaxOutputTokens
	}
	if effort := strings.TrimSpace(cfg.ReasoningEffort); effort != "" {
		body["reasoning_effort"] = effort
	}

	var response struct {
		Choices []struct {
			Message struct {
				Content any `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	client, err := newHTTPClient(settings.Proxy, cfg.TimeoutSeconds, 120*time.Second)
	if err != nil {
		return nil, err
	}
	endpoint := resolveEndpoint(
		cfg.BaseURL,
		"https://api.openai.com/v1/chat/completions",
		"/chat/completions",
	)
	headers := map[string]string{
		"Authorization": "Bearer " + cfg.APIKey,
	}
	EmitLLMRequestLog(ctx, "openai-chat", endpoint, cfg.Model, prompt, userPayload, req)
	if err := doJSONRequest(ctx, client, "POST", endpoint, headers, body, &response); err != nil {
		EmitLLMErrorLog(ctx, "openai-chat", "request", req, err)
		return nil, err
	}
	if len(response.Choices) == 0 {
		err := fmt.Errorf("openai chat returned no choices")
		EmitLLMErrorLog(ctx, "openai-chat", "response", req, err)
		return nil, err
	}

	text, err := extractChatCompletionText(response.Choices[0].Message.Content)
	if err != nil {
		EmitLLMErrorLog(ctx, "openai-chat", "extract", req, err)
		return nil, err
	}
	EmitLLMResponseLog(ctx, "openai-chat", req, text)
	results, err := resultsFromPayload(req.Items, text)
	if err != nil {
		EmitLLMErrorLog(ctx, "openai-chat", "parse", req, err)
		return nil, err
	}
	return results, nil
}

func extractChatCompletionText(content any) (string, error) {
	switch value := content.(type) {
	case string:
		return value, nil
	case []any:
		parts := make([]string, 0, len(value))
		for _, part := range value {
			text, ok := chatContentPartText(part)
			if ok && text != "" {
				parts = append(parts, text)
			}
		}
		if len(parts) == 0 {
			return "", fmt.Errorf("chat completion did not include text content")
		}
		return strings.Join(parts, "\n"), nil
	default:
		raw, err := json.Marshal(value)
		if err != nil {
			return "", fmt.Errorf("unsupported chat completion content type")
		}
		return string(raw), nil
	}
}

func chatContentPartText(value any) (string, bool) {
	part, ok := value.(map[string]any)
	if !ok {
		return "", false
	}
	for _, key := range []string{"text", "output_text"} {
		if text, ok := part[key].(string); ok {
			return text, true
		}
	}
	return "", false
}
