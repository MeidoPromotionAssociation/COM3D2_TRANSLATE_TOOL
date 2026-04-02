package translation

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"COM3D2TranslateTool/internal/model"
)

type BaiduTranslator struct{}

func (BaiduTranslator) Name() string {
	return "baidu-translate"
}

func (BaiduTranslator) Translate(ctx context.Context, req Request) ([]Result, error) {
	settings := model.NormalizeTranslationSettings(req.Settings)
	cfg := settings.Baidu
	if strings.TrimSpace(cfg.AppID) == "" || strings.TrimSpace(cfg.Secret) == "" {
		return nil, fmt.Errorf("baidu translate app id and secret are required")
	}
	if len(req.Items) == 0 {
		return nil, nil
	}

	client, err := newHTTPClient(settings.Proxy, cfg.TimeoutSeconds, 60*time.Second)
	if err != nil {
		return nil, err
	}
	endpoint := resolveEndpoint(
		cfg.BaseURL,
		"https://fanyi-api.baidu.com/api/trans/vip/translate",
		"/api/trans/vip/translate",
	)
	results := make([]Result, 0, len(req.Items))

	for _, item := range req.Items {
		translated, err := baiduTranslateOne(ctx, client, endpoint, cfg, settings, item.SourceText)
		if err != nil {
			return nil, err
		}
		results = append(results, Result{ID: item.ID, Text: translated})
	}
	return results, nil
}

func baiduTranslateOne(
	ctx context.Context,
	client *http.Client,
	endpoint string,
	cfg model.BaiduTranslateConfig,
	settings model.TranslationSettings,
	sourceText string,
) (string, error) {
	salt := strconv.FormatInt(time.Now().UnixNano(), 10)
	form := url.Values{}
	form.Set("q", sourceText)
	form.Set("from", normalizeBaiduLanguage(settings.SourceLanguage))
	form.Set("to", normalizeBaiduLanguage(settings.TargetLanguage))
	form.Set("appid", cfg.AppID)
	form.Set("salt", salt)
	form.Set("sign", baiduSign(cfg.AppID, sourceText, salt, cfg.Secret))

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", readHTTPError(resp)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var parsed struct {
		ErrorCode    string `json:"error_code"`
		ErrorMessage string `json:"error_msg"`
		Results      []struct {
			Destination string `json:"dst"`
		} `json:"trans_result"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	if parsed.ErrorCode != "" && parsed.ErrorCode != "0" {
		if parsed.ErrorMessage == "" {
			parsed.ErrorMessage = "unknown error"
		}
		return "", fmt.Errorf("baidu translate error %s: %s", parsed.ErrorCode, parsed.ErrorMessage)
	}
	if len(parsed.Results) == 0 {
		return "", fmt.Errorf("baidu translate returned no results")
	}
	return parsed.Results[0].Destination, nil
}

func baiduSign(appID, query, salt, secret string) string {
	hash := md5.Sum([]byte(appID + query + salt + secret))
	return hex.EncodeToString(hash[:])
}

func normalizeBaiduLanguage(value string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	switch trimmed {
	case "", "auto":
		return "auto"
	case "ja", "jp", "jpn":
		return "jp"
	case "zh", "zh-cn", "zh-hans", "zh-hans-cn":
		return "zh"
	case "zh-tw", "zh-hant", "zh-hk":
		return "cht"
	default:
		return trimmed
	}
}
