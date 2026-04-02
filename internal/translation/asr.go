package translation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"COM3D2TranslateTool/internal/model"
)

const asrTranslatorName = "qwen3-asr"

type AudioTranscriptionResult struct {
	Text     string `json:"text"`
	Language string `json:"language"`
}

type audioBatchTranscriptionResponse struct {
	Results []AudioTranscriptionResult `json:"results"`
}

func ASRTranslatorName() string {
	return asrTranslatorName
}

func TranscribeAudioFile(ctx context.Context, proxy model.ProxyConfig, config model.ASRConfig, filePath string) (AudioTranscriptionResult, error) {
	endpoint := asrSingleEndpoint(config.BaseURL)
	if endpoint == "" {
		return AudioTranscriptionResult{}, fmt.Errorf("asr base url is required")
	}

	client, err := newHTTPClient(proxy, config.TimeoutSeconds, 10*time.Minute)
	if err != nil {
		return AudioTranscriptionResult{}, err
	}

	payload, contentType, err := buildASRMultipartPayload(filePath, config)
	if err != nil {
		return AudioTranscriptionResult{}, err
	}

	var response AudioTranscriptionResult
	if err := doBytesRequest(ctx, client, "POST", endpoint, contentType, nil, payload, &response); err != nil {
		return AudioTranscriptionResult{}, err
	}
	return response, nil
}

func TranscribeAudioFiles(ctx context.Context, proxy model.ProxyConfig, config model.ASRConfig, filePaths []string) ([]AudioTranscriptionResult, error) {
	if len(filePaths) == 0 {
		return nil, nil
	}
	if len(filePaths) == 1 {
		result, err := TranscribeAudioFile(ctx, proxy, config, filePaths[0])
		if err != nil {
			return nil, err
		}
		return []AudioTranscriptionResult{result}, nil
	}

	endpoint := asrBatchEndpoint(config.BaseURL)
	if endpoint == "" {
		return nil, fmt.Errorf("asr base url is required")
	}

	client, err := newHTTPClient(proxy, config.TimeoutSeconds, 10*time.Minute)
	if err != nil {
		return nil, err
	}

	payload, contentType, err := buildASRBatchMultipartPayload(filePaths, config)
	if err != nil {
		return nil, err
	}

	var response audioBatchTranscriptionResponse
	if err := doBytesRequest(ctx, client, "POST", endpoint, contentType, nil, payload, &response); err != nil {
		return nil, err
	}
	if len(response.Results) != len(filePaths) {
		return nil, fmt.Errorf("asr batch returned %d results for %d files", len(response.Results), len(filePaths))
	}
	return response.Results, nil
}

func buildASRMultipartPayload(filePath string, config model.ASRConfig) ([]byte, string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if err := writeASRSharedFields(writer, config); err != nil {
		return nil, "", err
	}

	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return nil, "", err
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()

	if _, err := io.Copy(part, file); err != nil {
		return nil, "", err
	}

	if err := writer.Close(); err != nil {
		return nil, "", err
	}

	return body.Bytes(), writer.FormDataContentType(), nil
}

func buildASRMultipartPayloadFromBytes(filename string, data []byte, config model.ASRConfig) ([]byte, string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if err := writeASRSharedFields(writer, config); err != nil {
		return nil, "", err
	}

	part, err := writer.CreateFormFile("file", strings.TrimSpace(filename))
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(data); err != nil {
		return nil, "", err
	}

	if err := writer.Close(); err != nil {
		return nil, "", err
	}

	return body.Bytes(), writer.FormDataContentType(), nil
}

func buildASRBatchMultipartPayload(filePaths []string, config model.ASRConfig) ([]byte, string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if err := writeASRSharedFields(writer, config); err != nil {
		return nil, "", err
	}

	for _, filePath := range filePaths {
		part, err := writer.CreateFormFile("files", filepath.Base(filePath))
		if err != nil {
			return nil, "", err
		}

		file, err := os.Open(filePath)
		if err != nil {
			return nil, "", err
		}

		_, copyErr := io.Copy(part, file)
		closeErr := file.Close()
		if copyErr != nil {
			return nil, "", copyErr
		}
		if closeErr != nil {
			return nil, "", closeErr
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", err
	}

	return body.Bytes(), writer.FormDataContentType(), nil
}

func writeASRSharedFields(writer *multipart.Writer, config model.ASRConfig) error {
	if language := strings.TrimSpace(config.Language); language != "" {
		if err := writer.WriteField("language", language); err != nil {
			return err
		}
	}
	if prompt := strings.TrimSpace(config.Prompt); prompt != "" {
		if err := writer.WriteField("prompt", prompt); err != nil {
			return err
		}
	}
	return nil
}

func EmitASRRequestLog(ctx context.Context, endpoint string, entry model.Entry, audioPath string, config model.ASRConfig) {
	content := strings.TrimSpace(fmt.Sprintf(
		"Endpoint: %s\nArc: %s\nFile: %s\nVoice ID: %s\nAudio File: %s\nLanguage: %s\nPrompt: %s",
		endpoint,
		entry.SourceArc,
		entry.SourceFile,
		entry.VoiceID,
		audioPath,
		strings.TrimSpace(config.Language),
		strings.TrimSpace(config.Prompt),
	))

	EmitLog(ctx, model.TranslateLog{
		Translator: asrTranslatorName,
		Kind:       "request",
		Title:      "ASR Request",
		Content:    truncateTranslateLogContent(content),
	})
}

func EmitASRBatchRequestLog(ctx context.Context, endpoint string, entries []model.Entry, audioPaths []string, config model.ASRConfig) {
	lines := make([]string, 0, len(entries)+4)
	lines = append(lines, "Endpoint: "+endpoint)
	lines = append(lines, fmt.Sprintf("Batch Size: %d", len(entries)))
	lines = append(lines, "Language: "+strings.TrimSpace(config.Language))
	lines = append(lines, "Prompt: "+strings.TrimSpace(config.Prompt))
	lines = append(lines, "")
	for index, entry := range entries {
		audioPath := ""
		if index < len(audioPaths) {
			audioPath = audioPaths[index]
		}
		lines = append(lines, fmt.Sprintf("%d. %s | %s | %s | %s", index+1, entry.SourceArc, entry.SourceFile, entry.VoiceID, audioPath))
	}

	EmitLog(ctx, model.TranslateLog{
		Translator: asrTranslatorName,
		Kind:       "request",
		Title:      "ASR Batch Request",
		Content:    truncateTranslateLogContent(strings.Join(lines, "\n")),
	})
}

func EmitASRResponseLog(ctx context.Context, entry model.Entry, result AudioTranscriptionResult) {
	raw, _ := json.MarshalIndent(result, "", "  ")
	content := strings.TrimSpace(fmt.Sprintf(
		"Arc: %s\nFile: %s\nVoice ID: %s\n\n[ASR Output]\n%s",
		entry.SourceArc,
		entry.SourceFile,
		entry.VoiceID,
		string(raw),
	))

	EmitLog(ctx, model.TranslateLog{
		Translator: asrTranslatorName,
		Kind:       "response",
		Title:      "ASR Response",
		Content:    truncateTranslateLogContent(content),
	})
}

func EmitASRBatchResponseLog(ctx context.Context, entries []model.Entry, results []AudioTranscriptionResult) {
	body := struct {
		Count   int                        `json:"count"`
		Results []AudioTranscriptionResult `json:"results"`
	}{
		Count:   len(results),
		Results: results,
	}
	raw, _ := json.MarshalIndent(body, "", "  ")

	lines := make([]string, 0, len(entries)+2)
	lines = append(lines, fmt.Sprintf("Batch Size: %d", len(entries)))
	lines = append(lines, "")
	lines = append(lines, string(raw))

	EmitLog(ctx, model.TranslateLog{
		Translator: asrTranslatorName,
		Kind:       "response",
		Title:      "ASR Batch Response",
		Content:    truncateTranslateLogContent(strings.Join(lines, "\n")),
	})
}

func EmitASRErrorLog(ctx context.Context, entry model.Entry, stage string, err error) {
	if err == nil {
		return
	}

	content := strings.TrimSpace(fmt.Sprintf(
		"Stage: %s\nArc: %s\nFile: %s\nVoice ID: %s\nError: %v",
		stage,
		entry.SourceArc,
		entry.SourceFile,
		entry.VoiceID,
		err,
	))

	EmitLog(ctx, model.TranslateLog{
		Translator: asrTranslatorName,
		Kind:       "error",
		Title:      "ASR Error",
		Content:    truncateTranslateLogContent(content),
	})
}

func EmitASRBatchErrorLog(ctx context.Context, entries []model.Entry, stage string, err error) {
	if err == nil {
		return
	}

	lines := make([]string, 0, len(entries)+2)
	lines = append(lines, "Stage: "+stage)
	lines = append(lines, fmt.Sprintf("Batch Size: %d", len(entries)))
	for index, entry := range entries {
		lines = append(lines, fmt.Sprintf("%d. %s | %s | %s", index+1, entry.SourceArc, entry.SourceFile, entry.VoiceID))
	}
	lines = append(lines, "Error: "+err.Error())

	EmitLog(ctx, model.TranslateLog{
		Translator: asrTranslatorName,
		Kind:       "error",
		Title:      "ASR Batch Error",
		Content:    truncateTranslateLogContent(strings.Join(lines, "\n")),
	})
}

func asrSingleEndpoint(raw string) string {
	endpoint := strings.TrimSpace(raw)
	endpoint = strings.TrimSuffix(endpoint, "/batch")
	return endpoint
}

func asrBatchEndpoint(raw string) string {
	base := asrSingleEndpoint(raw)
	if base == "" {
		return ""
	}
	return strings.TrimSuffix(base, "/") + "/batch"
}
