package translation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"COM3D2TranslateTool/internal/model"
)

type OpenAIResponsesTranslator struct{}

func (OpenAIResponsesTranslator) Name() string {
	return "openai-responses"
}

func (OpenAIResponsesTranslator) Translate(ctx context.Context, req Request) ([]Result, error) {
	settings := model.NormalizeTranslationSettings(req.Settings)
	cfg := settings.OpenAIResponses
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("openai responses api key is required")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("openai responses model is required")
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
	body["instructions"] = prompt
	body["input"] = userPayload
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
		body["max_output_tokens"] = *cfg.MaxOutputTokens
	}
	if effort := strings.TrimSpace(cfg.ReasoningEffort); effort != "" {
		body["reasoning"] = map[string]any{
			"effort": effort,
		}
	}

	var response struct {
		Status            string `json:"status"`
		OutputText        string `json:"output_text"`
		IncompleteDetails *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Output []struct {
			Type    string `json:"type"`
			Status  string `json:"status"`
			Content []struct {
				Type    string `json:"type"`
				Text    string `json:"text"`
				Refusal string `json:"refusal"`
			} `json:"content"`
		} `json:"output"`
	}

	client, err := newHTTPClient(settings.Proxy, cfg.TimeoutSeconds, 120*time.Second)
	if err != nil {
		return nil, err
	}
	endpoint := resolveEndpoint(
		cfg.BaseURL,
		"https://api.openai.com/v1/responses",
		"/responses",
	)
	headers := map[string]string{
		"Authorization": "Bearer " + cfg.APIKey,
	}
	EmitLLMRequestLog(ctx, "openai-responses", endpoint, cfg.Model, prompt, userPayload, req)
	if err := doJSONRequest(ctx, client, "POST", endpoint, headers, body, &response); err != nil {
		EmitLLMErrorLog(ctx, "openai-responses", "request", req, err)
		return nil, err
	}
	if err := validateResponsesAPIResult(response.Status, response.Error, response.IncompleteDetails); err != nil {
		EmitLLMErrorLog(ctx, "openai-responses", "response", req, err)
		return nil, err
	}

	text := strings.TrimSpace(response.OutputText)
	if text == "" {
		text = extractResponsesText(response.Output)
	}
	if text == "" {
		err := fmt.Errorf("openai responses returned no text output")
		EmitLLMErrorLog(ctx, "openai-responses", "response", req, err)
		return nil, err
	}
	EmitLLMResponseLog(ctx, "openai-responses", req, text)
	results, err := resultsFromPayload(req.Items, text)
	if err != nil {
		EmitLLMErrorLog(ctx, "openai-responses", "parse", req, err)
		return nil, err
	}
	return results, nil
}

func validateResponsesAPIResult(
	status string,
	apiError *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	},
	incomplete *struct {
		Reason string `json:"reason"`
	},
) error {
	if apiError != nil {
		details := strings.TrimSpace(apiError.Message)
		if code := strings.TrimSpace(apiError.Code); code != "" {
			if details == "" {
				details = code
			} else {
				details = code + ": " + details
			}
		}
		if details == "" {
			details = "unknown error"
		}
		return fmt.Errorf("openai responses error: %s", details)
	}

	switch strings.TrimSpace(status) {
	case "", "completed":
		return nil
	case "failed", "cancelled":
		return fmt.Errorf("openai responses status: %s", status)
	case "incomplete":
		reason := ""
		if incomplete != nil {
			reason = strings.TrimSpace(incomplete.Reason)
		}
		if reason == "" {
			return fmt.Errorf("openai responses status: incomplete")
		}
		return fmt.Errorf("openai responses incomplete: %s", reason)
	default:
		return fmt.Errorf("openai responses status: %s", status)
	}
}

func extractResponsesText(output []struct {
	Type   string `json:"type"`
	Status string `json:"status"`
	Content []struct {
		Type    string `json:"type"`
		Text    string `json:"text"`
		Refusal string `json:"refusal"`
	} `json:"content"`
}) string {
	parts := make([]string, 0)
	for _, item := range output {
		for _, content := range item.Content {
			if content.Type == "output_text" || content.Type == "text" {
				if strings.TrimSpace(content.Text) != "" {
					parts = append(parts, content.Text)
				}
			}
			if content.Type == "refusal" && strings.TrimSpace(content.Refusal) != "" {
				parts = append(parts, content.Refusal)
			}
		}
	}
	return strings.Join(parts, "\n")
}
