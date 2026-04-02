export type ProxyConfig = {
    mode: string;
    url: string;
};

export type ProxyTestResult = {
    targetUrl: string;
    finalUrl: string;
    proxyMode: string;
    resolvedProxy: string;
    statusCode: number;
    status: string;
    bytesRead: number;
};

export type ASRTestResult = {
    endpoint: string;
    finalUrl: string;
    proxyMode: string;
    resolvedProxy: string;
    statusCode: number;
    status: string;
    responseTime: number;
    responseLanguage: string;
    responseText: string;
    responseBody: string;
};

export type TranslatorTestRequest = {
    translator: string;
    targetField: string;
    settings: TranslationSettings;
};

export type TranslatorTestResult = {
    translator: string;
    targetField: string;
    sourceText: string;
    outputText: string;
    responseTime: number;
};

export type MaintenanceResult = {
    deletedInvisibleBlankEntries: number;
};

export type GoogleTranslateConfig = {
    baseUrl: string;
    apiKey: string;
    format: string;
    model: string;
    batchSize: number;
    timeoutSeconds: number;
};

export type BaiduTranslateConfig = {
    baseUrl: string;
    appId: string;
    secret: string;
    timeoutSeconds: number;
};

export type ASRConfig = {
    baseUrl: string;
    language: string;
    prompt: string;
    batchSize: number;
    timeoutSeconds: number;
    concurrency: number;
};

export type OpenAIProviderConfig = {
    baseUrl: string;
    apiKey: string;
    model: string;
    prompt: string;
    batchSize: number;
    concurrency: number;
    timeoutSeconds: number;
    temperature: number | null;
    topP: number | null;
    presencePenalty: number | null;
    frequencyPenalty: number | null;
    maxOutputTokens: number | null;
    reasoningEffort: string;
    extraJson: string;
};

export type TranslationSettings = {
    activeTranslator: string;
    sourceLanguage: string;
    targetLanguage: string;
    glossary: string;
    proxy: ProxyConfig;
    google: GoogleTranslateConfig;
    baidu: BaiduTranslateConfig;
    asr: ASRConfig;
    openAIChat: OpenAIProviderConfig;
    openAIResponses: OpenAIProviderConfig;
};

export type Settings = {
    arcScanDir: string;
    workDir: string;
    importDir: string;
    exportDir: string;
    translation: TranslationSettings;
};

export type ArcFile = {
    id: number;
    filename: string;
    path: string;
    status: string;
    lastError: string;
    discoveredAt: string;
    parsedAt: string;
    lastScannedAt: string;
};

export type Entry = {
    id: number;
    type: string;
    voiceId: string;
    role: string;
    sourceArc: string;
    sourceFile: string;
    sourceText: string;
    translatedText: string;
    polishedText: string;
    translatorStatus: string;
    createdAt: string;
    updatedAt: string;
};

export type EntryQuery = {
    search: string;
    sourceArc: string;
    sourceFile: string;
    type: string;
    status: string;
    untranslatedOnly: boolean;
    limit: number;
    offset: number;
};

export type EntryList = {
    items: Entry[];
    total: number;
};

export type FilterOptions = {
    arcs: string[];
    files: string[];
    types: string[];
    statuses: string[];
};

export type UpdateEntryInput = {
    id: number;
    translatedText: string;
    polishedText: string;
    translatorStatus: string;
};

export type FilterBatchStatusInput = {
    query: EntryQuery;
    translatorStatus: string;
};

export type ScanResult = {
    scanned: number;
    newArcCount: number;
    parsedCount: number;
    failedCount: number;
    messages: string[];
};

export type ParseResult = {
    arcFilename: string;
    entryCount: number;
    message: string;
};

export type ReparseFailedResult = {
    totalFailed: number;
    reparsedCount: number;
    failedCount: number;
    messages: string[];
};

export type ReparseAllResult = {
    totalArcs: number;
    reparsedCount: number;
    failedCount: number;
    skippedCount: number;
    messages: string[];
};

export type BatchDeleteResult = {
    deleted: number;
};

export type ImportRequest = {
    importer: string;
    rootDir: string;
    allowOverwrite: boolean;
};

export type ImportResult = {
    importer: string;
    filesProcessed: number;
    totalLines: number;
    inserted: number;
    updated: number;
    skipped: number;
    unmatched: number;
    errorLines: number;
    messages: string[];
};

export type ImportProgress = {
    importer: string;
    currentFile: string;
    filesProcessed: number;
    totalLines: number;
    inserted: number;
    updated: number;
    skipped: number;
    unmatched: number;
    errorLines: number;
    phase: string;
};

export type ExportProgress = {
    exporter: string;
    outputPath: string;
    processedRows: number;
    exported: number;
    skipped: number;
    phase: string;
};

export type ExportRequest = {
    exporter: string;
    outputPath: string;
    search: string;
    sourceArc: string;
    sourceFile: string;
    type: string;
    status: string;
    untranslatedOnly: boolean;
    skipEmptyFinal: boolean;
};

export type ExportResult = {
    exporter: string;
    outputPath: string;
    exported: number;
    skipped: number;
    message: string;
};

export type TranslateRequest = {
    translator: string;
    search: string;
    sourceArc: string;
    sourceFile: string;
    type: string;
    status: string;
    untranslatedOnly: boolean;
    allowOverwrite: boolean;
    targetField: string;
};

export type TranslateResult = {
    translator: string;
    targetField: string;
    total: number;
    processed: number;
    updated: number;
    skipped: number;
    failed: number;
    messages: string[];
};

export type SourceRecognitionRequest = {
    search: string;
    sourceArc: string;
    sourceFile: string;
    type: string;
    status: string;
    untranslatedOnly: boolean;
    allowOverwrite: boolean;
};

export type SourceRecognitionResult = {
    provider: string;
    total: number;
    processed: number;
    updated: number;
    skipped: number;
    failed: number;
    messages: string[];
};

export type TranslateProgress = {
  translator: string;
  targetField: string;
  currentItem: string;
  total: number;
    processed: number;
    updated: number;
    skipped: number;
  failed: number;
  phase: string;
};

export type TranslateLog = {
  translator: string;
  kind: string;
  title: string;
  content: string;
  timestamp: string;
};
