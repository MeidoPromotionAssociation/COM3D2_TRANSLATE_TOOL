package translation

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"COM3D2TranslateTool/internal/model"
)

func TestTranscribeAudioFileSendsExpectedMultipartRequest(t *testing.T) {
	tempDir := t.TempDir()
	audioPath := filepath.Join(tempDir, "V_0001.ogg")
	if err := os.WriteFile(audioPath, []byte("dummy-audio"), 0o644); err != nil {
		t.Fatalf("write audio file: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST request, got %s", r.Method)
		}
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			t.Fatalf("parse multipart form: %v", err)
		}
		if got := r.FormValue("language"); got != "Japanese" {
			t.Fatalf("expected language field Japanese, got %q", got)
		}
		if got := r.FormValue("prompt"); got != "keep honorifics" {
			t.Fatalf("expected prompt field, got %q", got)
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("read uploaded file: %v", err)
		}
		defer file.Close()

		if header.Filename != "V_0001.ogg" {
			t.Fatalf("expected uploaded filename V_0001.ogg, got %q", header.Filename)
		}

		body, err := io.ReadAll(file)
		if err != nil {
			t.Fatalf("read uploaded body: %v", err)
		}
		if string(body) != "dummy-audio" {
			t.Fatalf("unexpected uploaded audio payload: %q", string(body))
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"recognized-line","language":"Japanese"}`))
	}))
	defer server.Close()

	result, err := TranscribeAudioFile(context.Background(), model.ProxyConfig{Mode: "direct"}, model.ASRConfig{
		BaseURL:        server.URL,
		Language:       "Japanese",
		Prompt:         "keep honorifics",
		TimeoutSeconds: 10,
	}, audioPath)
	if err != nil {
		t.Fatalf("TranscribeAudioFile failed: %v", err)
	}
	if result.Text != "recognized-line" || result.Language != "Japanese" {
		t.Fatalf("unexpected transcription result: %#v", result)
	}
}

func TestTranscribeAudioFilesUsesBatchEndpointAndFilesField(t *testing.T) {
	tempDir := t.TempDir()
	firstAudioPath := filepath.Join(tempDir, "V_0001.ogg")
	secondAudioPath := filepath.Join(tempDir, "V_0002.ogg")
	if err := os.WriteFile(firstAudioPath, []byte("audio-one"), 0o644); err != nil {
		t.Fatalf("write first audio file: %v", err)
	}
	if err := os.WriteFile(secondAudioPath, []byte("audio-two"), 0o644); err != nil {
		t.Fatalf("write second audio file: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions/batch" {
			t.Fatalf("expected batch endpoint path, got %q", r.URL.Path)
		}
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			t.Fatalf("parse multipart form: %v", err)
		}

		if got := r.MultipartForm.Value["language"]; len(got) != 1 || got[0] != "Japanese" {
			t.Fatalf("expected single shared language value, got %#v", got)
		}
		if got := r.MultipartForm.Value["prompt"]; len(got) != 1 || got[0] != "keep honorifics" {
			t.Fatalf("expected single shared prompt value, got %#v", got)
		}

		files := r.MultipartForm.File["files"]
		if len(files) != 2 {
			t.Fatalf("expected 2 files in batch form, got %d", len(files))
		}

		payloads := make([]string, 0, len(files))
		names := make([]string, 0, len(files))
		for _, header := range files {
			file, err := header.Open()
			if err != nil {
				t.Fatalf("open uploaded batch file: %v", err)
			}
			body, err := io.ReadAll(file)
			_ = file.Close()
			if err != nil {
				t.Fatalf("read uploaded batch file: %v", err)
			}
			names = append(names, header.Filename)
			payloads = append(payloads, string(body))
		}

		if strings.Join(names, ",") != "V_0001.ogg,V_0002.ogg" {
			t.Fatalf("unexpected uploaded filenames: %#v", names)
		}
		if strings.Join(payloads, ",") != "audio-one,audio-two" {
			t.Fatalf("unexpected uploaded payloads: %#v", payloads)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"text":"line-one","language":"Japanese"},{"text":"line-two","language":"Japanese"}]}`))
	}))
	defer server.Close()

	results, err := TranscribeAudioFiles(context.Background(), model.ProxyConfig{Mode: "direct"}, model.ASRConfig{
		BaseURL:        server.URL + "/v1/audio/transcriptions",
		Language:       "Japanese",
		Prompt:         "keep honorifics",
		BatchSize:      4,
		TimeoutSeconds: 10,
	}, []string{firstAudioPath, secondAudioPath})
	if err != nil {
		t.Fatalf("TranscribeAudioFiles failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 batch transcription results, got %#v", results)
	}
	if results[0].Text != "line-one" || results[1].Text != "line-two" {
		t.Fatalf("unexpected batch transcription results: %#v", results)
	}
}

func TestTestASRTranscriptionPostsSilentSampleToConfiguredEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST request, got %s", r.Method)
		}
		if r.URL.Path != "/v1/audio/transcriptions" {
			t.Fatalf("expected single endpoint path, got %q", r.URL.Path)
		}
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			t.Fatalf("parse multipart form: %v", err)
		}
		if got := r.FormValue("language"); got != "Japanese" {
			t.Fatalf("expected language field Japanese, got %q", got)
		}
		if got := r.FormValue("prompt"); got != "test prompt" {
			t.Fatalf("expected prompt field, got %q", got)
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("read uploaded test file: %v", err)
		}
		defer file.Close()

		if header.Filename != asrTestAudioFilename {
			t.Fatalf("expected uploaded filename %s, got %q", asrTestAudioFilename, header.Filename)
		}

		body, err := io.ReadAll(file)
		if err != nil {
			t.Fatalf("read uploaded test body: %v", err)
		}
		if len(body) <= 44 {
			t.Fatalf("expected wav payload larger than header, got %d bytes", len(body))
		}
		if string(body[:4]) != "RIFF" || string(body[8:12]) != "WAVE" {
			t.Fatalf("expected RIFF/WAVE header, got %q / %q", string(body[:4]), string(body[8:12]))
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"","language":"Japanese"}`))
	}))
	defer server.Close()

	result, err := TestASRTranscription(context.Background(), model.ProxyConfig{Mode: "direct"}, model.ASRConfig{
		BaseURL:        server.URL + "/v1/audio/transcriptions",
		Language:       "Japanese",
		Prompt:         "test prompt",
		TimeoutSeconds: 10,
	})
	if err != nil {
		t.Fatalf("TestASRTranscription failed: %v", err)
	}

	if result.Endpoint != server.URL+"/v1/audio/transcriptions" {
		t.Fatalf("unexpected endpoint: %#v", result)
	}
	if result.StatusCode != http.StatusOK || result.Status != "200 OK" {
		t.Fatalf("unexpected status: %#v", result)
	}
	if result.ProxyMode != "direct" || result.ResolvedProxy != "" {
		t.Fatalf("unexpected proxy info: %#v", result)
	}
	if result.ResponseLanguage != "Japanese" {
		t.Fatalf("unexpected response language: %#v", result)
	}
	if result.ResponseText != "" {
		t.Fatalf("expected empty response text for silent sample, got %#v", result)
	}
	if !strings.Contains(result.ResponseBody, `"language":"Japanese"`) {
		t.Fatalf("unexpected response body: %#v", result)
	}
	if result.ResponseTime < 0 {
		t.Fatalf("unexpected response time: %#v", result)
	}
}
