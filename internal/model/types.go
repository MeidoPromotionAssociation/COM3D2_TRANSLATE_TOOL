package model

type Settings struct {
	ArcScanDir  string              `json:"arcScanDir"`
	WorkDir     string              `json:"workDir"`
	ImportDir   string              `json:"importDir"`
	ExportDir   string              `json:"exportDir"`
	Translation TranslationSettings `json:"translation"`
}

type TranslationSettings struct {
	ActiveTranslator string                `json:"activeTranslator"`
	SourceLanguage   string                `json:"sourceLanguage"`
	TargetLanguage   string                `json:"targetLanguage"`
	Glossary         string                `json:"glossary"`
	RetryCount       int                   `json:"retryCount"`
	Proxy            ProxyConfig           `json:"proxy"`
	Google           GoogleTranslateConfig `json:"google"`
	Baidu            BaiduTranslateConfig  `json:"baidu"`
	ASR              ASRConfig             `json:"asr"`
	OpenAIChat       OpenAIProviderConfig  `json:"openAIChat"`
	OpenAIResponses  OpenAIProviderConfig  `json:"openAIResponses"`
}

type ProxyConfig struct {
	Mode string `json:"mode"`
	URL  string `json:"url"`
}

type ProxyTestResult struct {
	TargetURL     string `json:"targetUrl"`
	FinalURL      string `json:"finalUrl"`
	ProxyMode     string `json:"proxyMode"`
	ResolvedProxy string `json:"resolvedProxy"`
	StatusCode    int    `json:"statusCode"`
	Status        string `json:"status"`
	BytesRead     int64  `json:"bytesRead"`
}

type ASRTestResult struct {
	Endpoint         string `json:"endpoint"`
	FinalURL         string `json:"finalUrl"`
	ProxyMode        string `json:"proxyMode"`
	ResolvedProxy    string `json:"resolvedProxy"`
	StatusCode       int    `json:"statusCode"`
	Status           string `json:"status"`
	ResponseTime     int64  `json:"responseTime"`
	ResponseLanguage string `json:"responseLanguage"`
	ResponseText     string `json:"responseText"`
	ResponseBody     string `json:"responseBody"`
}

type GoogleTranslateConfig struct {
	BaseURL        string `json:"baseUrl"`
	APIKey         string `json:"apiKey"`
	Format         string `json:"format"`
	Model          string `json:"model"`
	BatchSize      int    `json:"batchSize"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
}

type BaiduTranslateConfig struct {
	BaseURL        string `json:"baseUrl"`
	AppID          string `json:"appId"`
	Secret         string `json:"secret"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
}

type ASRConfig struct {
	BaseURL        string `json:"baseUrl"`
	Language       string `json:"language"`
	Prompt         string `json:"prompt"`
	BatchSize      int    `json:"batchSize"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
	Concurrency    int    `json:"concurrency"`
}

type OpenAIProviderConfig struct {
	BaseURL          string   `json:"baseUrl"`
	APIKey           string   `json:"apiKey"`
	Model            string   `json:"model"`
	Prompt           string   `json:"prompt"`
	BatchSize        int      `json:"batchSize"`
	Concurrency      int      `json:"concurrency"`
	TimeoutSeconds   int      `json:"timeoutSeconds"`
	Temperature      *float64 `json:"temperature"`
	TopP             *float64 `json:"topP"`
	PresencePenalty  *float64 `json:"presencePenalty"`
	FrequencyPenalty *float64 `json:"frequencyPenalty"`
	MaxOutputTokens  *int     `json:"maxOutputTokens"`
	ReasoningEffort  string   `json:"reasoningEffort"`
	ExtraJSON        string   `json:"extraJson"`
}

type ArcFile struct {
	ID            int64  `json:"id"`
	Filename      string `json:"filename"`
	Path          string `json:"path"`
	Status        string `json:"status"`
	LastError     string `json:"lastError"`
	DiscoveredAt  string `json:"discoveredAt"`
	ParsedAt      string `json:"parsedAt"`
	LastScannedAt string `json:"lastScannedAt"`
}

type Entry struct {
	ID               int64  `json:"id"`
	Type             string `json:"type"`
	VoiceID          string `json:"voiceId"`
	Role             string `json:"role"`
	SourceArc        string `json:"sourceArc"`
	SourceFile       string `json:"sourceFile"`
	SourceText       string `json:"sourceText"`
	TranslatedText   string `json:"translatedText"`
	PolishedText     string `json:"polishedText"`
	TranslatorStatus string `json:"translatorStatus"`
	CreatedAt        string `json:"createdAt"`
	UpdatedAt        string `json:"updatedAt"`
}

type EntryQuery struct {
	Search           string `json:"search"`
	SourceArc        string `json:"sourceArc"`
	SourceFile       string `json:"sourceFile"`
	Type             string `json:"type"`
	Status           string `json:"status"`
	UntranslatedOnly bool   `json:"untranslatedOnly"`
	Limit            int    `json:"limit"`
	Offset           int    `json:"offset"`
}

type EntryList struct {
	Items []Entry `json:"items"`
	Total int     `json:"total"`
}

type FilterOptions struct {
	Arcs     []string `json:"arcs"`
	Files    []string `json:"files"`
	Types    []string `json:"types"`
	Statuses []string `json:"statuses"`
}

type UpdateEntryInput struct {
	ID               int64  `json:"id"`
	TranslatedText   string `json:"translatedText"`
	PolishedText     string `json:"polishedText"`
	TranslatorStatus string `json:"translatorStatus"`
}

type BatchUpdateInput struct {
	IDs              []int64 `json:"ids"`
	TranslatedText   string  `json:"translatedText"`
	PolishedText     string  `json:"polishedText"`
	TranslatorStatus string  `json:"translatorStatus"`
}

type FilterBatchStatusInput struct {
	Query            EntryQuery `json:"query"`
	TranslatorStatus string     `json:"translatorStatus"`
}

type BatchUpdateResult struct {
	Updated int `json:"updated"`
}

type BatchDeleteResult struct {
	Deleted int `json:"deleted"`
}

type ScanResult struct {
	Scanned     int      `json:"scanned"`
	NewArcCount int      `json:"newArcCount"`
	ParsedCount int      `json:"parsedCount"`
	FailedCount int      `json:"failedCount"`
	Messages    []string `json:"messages"`
}

type ParseResult struct {
	ArcFilename string `json:"arcFilename"`
	EntryCount  int    `json:"entryCount"`
	Message     string `json:"message"`
}

type ReparseFailedResult struct {
	TotalFailed   int      `json:"totalFailed"`
	ReparsedCount int      `json:"reparsedCount"`
	FailedCount   int      `json:"failedCount"`
	Messages      []string `json:"messages"`
}

type ReparseAllResult struct {
	TotalArcs     int      `json:"totalArcs"`
	ReparsedCount int      `json:"reparsedCount"`
	FailedCount   int      `json:"failedCount"`
	SkippedCount  int      `json:"skippedCount"`
	Messages      []string `json:"messages"`
}

type ImportRequest struct {
	Importer       string `json:"importer"`
	RootDir        string `json:"rootDir"`
	AllowOverwrite bool   `json:"allowOverwrite"`
}

type ImportResult struct {
	Importer       string   `json:"importer"`
	FilesProcessed int      `json:"filesProcessed"`
	TotalLines     int      `json:"totalLines"`
	Inserted       int      `json:"inserted"`
	Updated        int      `json:"updated"`
	Skipped        int      `json:"skipped"`
	Unmatched      int      `json:"unmatched"`
	ErrorLines     int      `json:"errorLines"`
	Messages       []string `json:"messages"`
}

type ImportProgress struct {
	Importer       string `json:"importer"`
	CurrentFile    string `json:"currentFile"`
	FilesProcessed int    `json:"filesProcessed"`
	TotalLines     int    `json:"totalLines"`
	Inserted       int    `json:"inserted"`
	Updated        int    `json:"updated"`
	Skipped        int    `json:"skipped"`
	Unmatched      int    `json:"unmatched"`
	ErrorLines     int    `json:"errorLines"`
	Phase          string `json:"phase"`
}

type ExportRequest struct {
	Exporter         string `json:"exporter"`
	OutputPath       string `json:"outputPath"`
	Search           string `json:"search"`
	SourceArc        string `json:"sourceArc"`
	SourceFile       string `json:"sourceFile"`
	Type             string `json:"type"`
	Status           string `json:"status"`
	UntranslatedOnly bool   `json:"untranslatedOnly"`
	SkipEmptyFinal   bool   `json:"skipEmptyFinal"`
}

type ExportResult struct {
	Exporter   string `json:"exporter"`
	OutputPath string `json:"outputPath"`
	Exported   int    `json:"exported"`
	Skipped    int    `json:"skipped"`
	Message    string `json:"message"`
}

type ExportProgress struct {
	Exporter      string `json:"exporter"`
	OutputPath    string `json:"outputPath"`
	ProcessedRows int    `json:"processedRows"`
	Exported      int    `json:"exported"`
	Skipped       int    `json:"skipped"`
	Phase         string `json:"phase"`
}

type TranslateRequest struct {
	Translator       string `json:"translator"`
	Search           string `json:"search"`
	SourceArc        string `json:"sourceArc"`
	SourceFile       string `json:"sourceFile"`
	Type             string `json:"type"`
	Status           string `json:"status"`
	UntranslatedOnly bool   `json:"untranslatedOnly"`
	AllowOverwrite   bool   `json:"allowOverwrite"`
	TargetField      string `json:"targetField"`
}

type TranslatorTestRequest struct {
	Translator  string              `json:"translator"`
	TargetField string              `json:"targetField"`
	Settings    TranslationSettings `json:"settings"`
}

type TranslatorTestResult struct {
	Translator   string `json:"translator"`
	TargetField  string `json:"targetField"`
	SourceText   string `json:"sourceText"`
	OutputText   string `json:"outputText"`
	ResponseTime int64  `json:"responseTime"`
}

type MaintenanceResult struct {
	DeletedInvisibleBlankEntries int `json:"deletedInvisibleBlankEntries"`
	FilledTranslatedEntries      int `json:"filledTranslatedEntries"`
}

type MaintenanceProgress struct {
	Operation            string `json:"operation"`
	CurrentSourceText    string `json:"currentSourceText"`
	TotalSourceTexts     int    `json:"totalSourceTexts"`
	ProcessedSourceTexts int    `json:"processedSourceTexts"`
	FilledEntries        int    `json:"filledEntries"`
	Phase                string `json:"phase"`
}

type TranslateResult struct {
	Translator  string   `json:"translator"`
	TargetField string   `json:"targetField"`
	Total       int      `json:"total"`
	Processed   int      `json:"processed"`
	Updated     int      `json:"updated"`
	Skipped     int      `json:"skipped"`
	Failed      int      `json:"failed"`
	Messages    []string `json:"messages"`
}

type TranslateProgress struct {
	Translator  string `json:"translator"`
	TargetField string `json:"targetField"`
	CurrentItem string `json:"currentItem"`
	Total       int    `json:"total"`
	Processed   int    `json:"processed"`
	Updated     int    `json:"updated"`
	Skipped     int    `json:"skipped"`
	Failed      int    `json:"failed"`
	Phase       string `json:"phase"`
}

type TranslateLog struct {
	Translator string `json:"translator"`
	Kind       string `json:"kind"`
	Title      string `json:"title"`
	Content    string `json:"content"`
	Timestamp  string `json:"timestamp"`
}

type SourceRecognitionRequest struct {
	Search           string `json:"search"`
	SourceArc        string `json:"sourceArc"`
	SourceFile       string `json:"sourceFile"`
	Type             string `json:"type"`
	Status           string `json:"status"`
	UntranslatedOnly bool   `json:"untranslatedOnly"`
	AllowOverwrite   bool   `json:"allowOverwrite"`
}

type SourceRecognitionResult struct {
	Provider  string   `json:"provider"`
	Total     int      `json:"total"`
	Processed int      `json:"processed"`
	Updated   int      `json:"updated"`
	Skipped   int      `json:"skipped"`
	Failed    int      `json:"failed"`
	Messages  []string `json:"messages"`
}

func DefaultTranslationSettings() TranslationSettings {
	return TranslationSettings{
		ActiveTranslator: "manual",
		SourceLanguage:   "ja",
		TargetLanguage:   "zh-CN",
		RetryCount:       1,
		Proxy: ProxyConfig{
			Mode: "system",
		},
		Google: GoogleTranslateConfig{
			BaseURL:        "https://translation.googleapis.com/language/translate/v2",
			Format:         "text",
			BatchSize:      32,
			TimeoutSeconds: 60,
		},
		Baidu: BaiduTranslateConfig{
			BaseURL:        "https://fanyi-api.baidu.com/api/trans/vip/translate",
			TimeoutSeconds: 60,
		},
		ASR: ASRConfig{
			BaseURL:        "http://127.0.0.1:8000/v1/audio/transcriptions",
			Language:       "Japanese",
			BatchSize:      4,
			TimeoutSeconds: 600,
			Concurrency:    1,
		},
		OpenAIChat: OpenAIProviderConfig{
			BaseURL:        "https://api.openai.com/v1",
			Model:          "gpt-5-mini",
			BatchSize:      32,
			Concurrency:    1,
			TimeoutSeconds: 120,
		},
		OpenAIResponses: OpenAIProviderConfig{
			BaseURL:        "https://api.openai.com/v1",
			Model:          "gpt-5-mini",
			BatchSize:      32,
			Concurrency:    1,
			TimeoutSeconds: 120,
		},
	}
}

func NormalizeTranslationSettings(settings TranslationSettings) TranslationSettings {
	defaults := DefaultTranslationSettings()

	if settings.ActiveTranslator == "" {
		settings.ActiveTranslator = defaults.ActiveTranslator
	}
	if settings.SourceLanguage == "" {
		settings.SourceLanguage = defaults.SourceLanguage
	}
	if settings.TargetLanguage == "" {
		settings.TargetLanguage = defaults.TargetLanguage
	}
	if settings.RetryCount < 0 {
		settings.RetryCount = defaults.RetryCount
	}
	switch settings.Proxy.Mode {
	case "system", "direct", "custom":
	default:
		settings.Proxy.Mode = defaults.Proxy.Mode
	}

	if settings.Google.BaseURL == "" {
		settings.Google.BaseURL = defaults.Google.BaseURL
	}
	if settings.Google.Format == "" {
		settings.Google.Format = defaults.Google.Format
	}
	if settings.Google.BatchSize <= 0 {
		settings.Google.BatchSize = defaults.Google.BatchSize
	}
	if settings.Google.TimeoutSeconds <= 0 {
		settings.Google.TimeoutSeconds = defaults.Google.TimeoutSeconds
	}

	if settings.Baidu.BaseURL == "" {
		settings.Baidu.BaseURL = defaults.Baidu.BaseURL
	}
	if settings.Baidu.TimeoutSeconds <= 0 {
		settings.Baidu.TimeoutSeconds = defaults.Baidu.TimeoutSeconds
	}

	if settings.ASR.BaseURL == "" {
		settings.ASR.BaseURL = defaults.ASR.BaseURL
	}
	if settings.ASR.Language == "" {
		settings.ASR.Language = defaults.ASR.Language
	}
	if settings.ASR.BatchSize <= 0 {
		settings.ASR.BatchSize = defaults.ASR.BatchSize
	}
	if settings.ASR.TimeoutSeconds <= 0 {
		settings.ASR.TimeoutSeconds = defaults.ASR.TimeoutSeconds
	}
	if settings.ASR.Concurrency <= 0 {
		settings.ASR.Concurrency = defaults.ASR.Concurrency
	}

	if settings.OpenAIChat.BaseURL == "" {
		settings.OpenAIChat.BaseURL = defaults.OpenAIChat.BaseURL
	}
	if settings.OpenAIChat.Model == "" {
		settings.OpenAIChat.Model = defaults.OpenAIChat.Model
	}
	if settings.OpenAIChat.BatchSize <= 0 {
		settings.OpenAIChat.BatchSize = defaults.OpenAIChat.BatchSize
	}
	if settings.OpenAIChat.Concurrency <= 0 {
		settings.OpenAIChat.Concurrency = defaults.OpenAIChat.Concurrency
	}
	if settings.OpenAIChat.TimeoutSeconds <= 0 {
		settings.OpenAIChat.TimeoutSeconds = defaults.OpenAIChat.TimeoutSeconds
	}

	if settings.OpenAIResponses.BaseURL == "" {
		settings.OpenAIResponses.BaseURL = defaults.OpenAIResponses.BaseURL
	}
	if settings.OpenAIResponses.Model == "" {
		settings.OpenAIResponses.Model = defaults.OpenAIResponses.Model
	}
	if settings.OpenAIResponses.BatchSize <= 0 {
		settings.OpenAIResponses.BatchSize = defaults.OpenAIResponses.BatchSize
	}
	if settings.OpenAIResponses.Concurrency <= 0 {
		settings.OpenAIResponses.Concurrency = defaults.OpenAIResponses.Concurrency
	}
	if settings.OpenAIResponses.TimeoutSeconds <= 0 {
		settings.OpenAIResponses.TimeoutSeconds = defaults.OpenAIResponses.TimeoutSeconds
	}

	return settings
}
