package translation

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"strings"
	"time"

	"COM3D2TranslateTool/internal/model"
)

type GoogleTranslator struct{}

func (GoogleTranslator) Name() string {
	return "google-translate"
}

func (GoogleTranslator) Translate(ctx context.Context, req Request) ([]Result, error) {
	settings := model.NormalizeTranslationSettings(req.Settings)
	cfg := settings.Google
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("google translate api key is required")
	}
	if len(req.Items) == 0 {
		return nil, nil
	}

	endpoint := resolveEndpoint(
		cfg.BaseURL,
		"https://translation.googleapis.com/language/translate/v2",
		"/language/translate/v2",
	)
	parsedURL, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	queryValues := parsedURL.Query()
	queryValues.Set("key", cfg.APIKey)
	parsedURL.RawQuery = queryValues.Encode()

	payload := map[string]any{
		"q":      collectSourceTexts(req.Items),
		"target": normalizeGoogleLanguage(settings.TargetLanguage),
		"format": normalizeGoogleFormat(cfg.Format),
	}
	if source := normalizeGoogleLanguage(settings.SourceLanguage); source != "" && source != "auto" {
		payload["source"] = source
	}
	if modelName := strings.TrimSpace(cfg.Model); modelName != "" {
		payload["model"] = modelName
	}

	var response struct {
		Data struct {
			Translations []struct {
				TranslatedText string `json:"translatedText"`
			} `json:"translations"`
		} `json:"data"`
	}

	client, err := newHTTPClient(settings.Proxy, cfg.TimeoutSeconds, 60*time.Second)
	if err != nil {
		return nil, err
	}
	if err := doJSONRequest(ctx, client, "POST", parsedURL.String(), nil, payload, &response); err != nil {
		return nil, err
	}
	if len(response.Data.Translations) != len(req.Items) {
		return nil, fmt.Errorf("google translate returned %d items for %d inputs", len(response.Data.Translations), len(req.Items))
	}

	results := make([]Result, 0, len(req.Items))
	for index, item := range req.Items {
		results = append(results, Result{
			ID:   item.ID,
			Text: html.UnescapeString(response.Data.Translations[index].TranslatedText),
		})
	}
	return results, nil
}

func collectSourceTexts(items []Item) []string {
	values := make([]string, 0, len(items))
	for _, item := range items {
		values = append(values, item.SourceText)
	}
	return values
}

func normalizeGoogleLanguage(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "auto"
	}
	return trimmed
}

func normalizeGoogleFormat(value string) string {
	switch strings.TrimSpace(value) {
	case "html":
		return "html"
	default:
		return "text"
	}
}
