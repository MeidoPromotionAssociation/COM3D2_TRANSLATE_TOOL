package translation

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"COM3D2TranslateTool/internal/model"
)

const (
	asrTestAudioFilename = "__asr_test_silence__.wav"
	asrTestResponseLimit = 32 * 1024
)

func TestASRTranscription(ctx context.Context, proxy model.ProxyConfig, config model.ASRConfig) (model.ASRTestResult, error) {
	endpoint := asrSingleEndpoint(config.BaseURL)
	if endpoint == "" {
		return model.ASRTestResult{}, fmt.Errorf("asr base url is required")
	}

	client, err := newHTTPClient(proxy, config.TimeoutSeconds, 60*time.Second)
	if err != nil {
		return model.ASRTestResult{}, err
	}

	audioPayload, err := buildSilentWAV(16000, time.Second)
	if err != nil {
		return model.ASRTestResult{}, err
	}

	payload, contentType, err := buildASRMultipartPayloadFromBytes(asrTestAudioFilename, audioPayload, config)
	if err != nil {
		return model.ASRTestResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return model.ASRTestResult{}, err
	}
	req.Header.Set("Content-Type", contentType)

	resolvedProxy := ""
	proxyFunc, err := resolveProxyFunc(proxy)
	if err != nil {
		return model.ASRTestResult{}, err
	}
	if proxyFunc != nil {
		proxyURL, err := proxyFunc(req)
		if err != nil {
			return model.ASRTestResult{}, err
		}
		if proxyURL != nil {
			resolvedProxy = proxyURL.String()
		}
	}

	startedAt := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return model.ASRTestResult{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, asrTestResponseLimit))
	if err != nil {
		return model.ASRTestResult{}, err
	}

	finalURL := endpoint
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}

	result := model.ASRTestResult{
		Endpoint:      endpoint,
		FinalURL:      finalURL,
		ProxyMode:     normalizeProxyMode(proxy.Mode),
		ResolvedProxy: resolvedProxy,
		StatusCode:    resp.StatusCode,
		Status:        resp.Status,
		ResponseTime:  time.Since(startedAt).Milliseconds(),
		ResponseBody:  truncateTranslateLogContent(string(bytes.TrimSpace(body))),
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, &httpStatusError{
			StatusCode: resp.StatusCode,
			Body:       string(bytes.TrimSpace(body)),
		}
	}

	trimmedBody := bytes.TrimSpace(body)
	if len(trimmedBody) == 0 {
		return result, fmt.Errorf("asr returned an empty response body")
	}

	var parsed AudioTranscriptionResult
	if err := json.Unmarshal(trimmedBody, &parsed); err != nil {
		return result, fmt.Errorf("failed to decode asr response: %w; body: %s", err, strings.TrimSpace(string(trimmedBody)))
	}

	result.ResponseLanguage = strings.TrimSpace(parsed.Language)
	result.ResponseText = strings.TrimSpace(parsed.Text)
	return result, nil
}

func buildSilentWAV(sampleRate int, duration time.Duration) ([]byte, error) {
	if sampleRate <= 0 {
		return nil, fmt.Errorf("sample rate must be positive")
	}
	if duration <= 0 {
		return nil, fmt.Errorf("duration must be positive")
	}

	sampleCount := int((duration * time.Duration(sampleRate)) / time.Second)
	if sampleCount <= 0 {
		sampleCount = sampleRate
	}

	const (
		numChannels   = 1
		bitsPerSample = 16
		audioFormat   = 1
	)

	blockAlign := numChannels * bitsPerSample / 8
	byteRate := sampleRate * blockAlign
	dataSize := sampleCount * blockAlign

	var buffer bytes.Buffer
	buffer.Grow(44 + dataSize)

	if _, err := buffer.WriteString("RIFF"); err != nil {
		return nil, err
	}
	if err := binary.Write(&buffer, binary.LittleEndian, uint32(36+dataSize)); err != nil {
		return nil, err
	}
	if _, err := buffer.WriteString("WAVE"); err != nil {
		return nil, err
	}
	if _, err := buffer.WriteString("fmt "); err != nil {
		return nil, err
	}
	if err := binary.Write(&buffer, binary.LittleEndian, uint32(16)); err != nil {
		return nil, err
	}
	if err := binary.Write(&buffer, binary.LittleEndian, uint16(audioFormat)); err != nil {
		return nil, err
	}
	if err := binary.Write(&buffer, binary.LittleEndian, uint16(numChannels)); err != nil {
		return nil, err
	}
	if err := binary.Write(&buffer, binary.LittleEndian, uint32(sampleRate)); err != nil {
		return nil, err
	}
	if err := binary.Write(&buffer, binary.LittleEndian, uint32(byteRate)); err != nil {
		return nil, err
	}
	if err := binary.Write(&buffer, binary.LittleEndian, uint16(blockAlign)); err != nil {
		return nil, err
	}
	if err := binary.Write(&buffer, binary.LittleEndian, uint16(bitsPerSample)); err != nil {
		return nil, err
	}
	if _, err := buffer.WriteString("data"); err != nil {
		return nil, err
	}
	if err := binary.Write(&buffer, binary.LittleEndian, uint32(dataSize)); err != nil {
		return nil, err
	}

	if _, err := buffer.Write(make([]byte, dataSize)); err != nil {
		return nil, err
	}

	return buffer.Bytes(), nil
}
