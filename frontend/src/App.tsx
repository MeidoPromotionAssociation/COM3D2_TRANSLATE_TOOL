import {useEffect, useRef, useState} from "react";
import "./App.css";
import {api} from "./api";
import {useTranslation} from "react-i18next";
import {EventsOn} from "../wailsjs/runtime/runtime";
import {persistLanguage} from "./i18n";
import type {
  ASRConfig,
  ArcFile,
  BaiduTranslateConfig,
  Entry,
  EntryQuery,
  ExportProgress,
  ExportRequest,
  FilterOptions,
  GoogleTranslateConfig,
  ImportProgress,
  ImportRequest,
  OpenAIProviderConfig,
  Settings,
  SourceRecognitionRequest,
  TranslateLog,
  TranslateProgress,
  TranslateRequest,
  TranslationSettings,
  UpdateEntryInput,
} from "./types";

type Page = "translate" | "tools";
type OpenAISettingsKey = "openAIChat" | "openAIResponses";
type CancellableTask = "" | "scan" | "reparse" | "import" | "export" | "translation" | "sourceRecognition";
type GlossaryRow = {
    id: string;
    source: string;
    preferred: string;
    note: string;
};
type TranslateFn = (key: string, options?: Record<string, unknown>) => string;
const asrTranslatorName = "qwen3-asr";

const emptyOpenAIProvider: OpenAIProviderConfig = {
    baseUrl: "https://api.openai.com/v1",
    apiKey: "",
    model: "gpt-5.4",
    prompt: "",
    batchSize: 32,
    concurrency: 1,
    timeoutSeconds: 120,
    temperature: null,
    topP: null,
    presencePenalty: null,
    frequencyPenalty: null,
    maxOutputTokens: null,
    reasoningEffort: "xhigh",
    extraJson: "",
};

const emptyTranslationSettings: TranslationSettings = {
    activeTranslator: "manual",
    sourceLanguage: "ja",
    targetLanguage: "zh-CN",
    glossary: "",
    proxy: {
        mode: "system",
        url: "",
    },
    google: {
        baseUrl: "https://translation.googleapis.com/language/translate/v2",
        apiKey: "",
        format: "text",
        model: "",
        batchSize: 32,
        timeoutSeconds: 60,
    },
    baidu: {
        baseUrl: "https://fanyi-api.baidu.com/api/trans/vip/translate",
        appId: "",
        secret: "",
        timeoutSeconds: 60,
    },
    asr: {
        baseUrl: "http://127.0.0.1:8000/v1/audio/transcriptions",
        language: "Japanese",
        prompt: "",
        batchSize: 4,
        timeoutSeconds: 600,
        concurrency: 1,
    },
    openAIChat: {...emptyOpenAIProvider},
    openAIResponses: {...emptyOpenAIProvider},
};

const emptySettings: Settings = {
    arcScanDir: "",
    workDir: "",
    importDir: "",
    exportDir: "",
    translation: emptyTranslationSettings,
};

const entryPageSize = 200;
const entryAutosaveDelayMs = 700;
const settingsAutosaveDelayMs = 900;
const statusMessageMaxChars = 200_000;

const defaultQuery: EntryQuery = {
    search: "",
    sourceArc: "",
    sourceFile: "",
    type: "",
    status: "",
    untranslatedOnly: false,
    limit: entryPageSize,
    offset: 0,
};

const defaultStatuses = ["new", "translated", "polished", "reviewed"];
const defaultImporters = ["arc-ks-folder-text", "arc-source-text-file", "entry-jsonl", "ks-extract-csv", "translated-csv"];
const defaultExporters = ["tab-text", "entry-jsonl"];
const defaultTranslators = ["manual", "google-translate", "baidu-translate", "openai-chat", "openai-responses"];

function getErrorMessage(error: unknown) {
    if (error instanceof Error) {
        return error.message;
    }
    if (typeof error === "string") {
        return error;
    }
    return JSON.stringify(error);
}

function ensureArray<T>(value: T[] | null | undefined): T[] {
    return Array.isArray(value) ? value : [];
}

function formatSummary(lines: string[] | null | undefined) {
    return ensureArray(lines).filter(Boolean).join("\n");
}

function appendSummary(base: string, summary: string) {
  return summary ? `${base}\n${summary}` : base;
}

function trimStatusMessage(value: string) {
  if (value.length <= statusMessageMaxChars) {
    return value;
  }
  return "...\n" + value.slice(value.length - statusMessageMaxChars);
}

function joinPath(dir: string, name: string) {
    if (!dir) {
        return name;
    }
    const separator = dir.endsWith("\\") || dir.endsWith("/") ? "" : "\\";
    return `${dir}${separator}${name}`;
}

function defaultExportFilename(exporter: string) {
    if (exporter === "entry-jsonl") {
        return "translations.jsonl";
    }
    return "translations.txt";
}

function buildDefaultExportPath(exportDir: string, exporter: string) {
  return joinPath(exportDir, defaultExportFilename(exporter));
}

function displayPath(path: string, emptyLabel: string) {
    return path.trim() === "" ? emptyLabel : path;
}

function isQueryFiltered(query: EntryQuery) {
    return query.search.trim() !== "" ||
        query.sourceArc.trim() !== "" ||
        query.sourceFile.trim() !== "" ||
        query.type.trim() !== "" ||
        query.status.trim() !== "" ||
        query.untranslatedOnly;
}

function toUpdateEntryInput(entry: Entry): UpdateEntryInput {
  return {
    id: entry.id,
    translatedText: entry.translatedText,
    polishedText: entry.polishedText,
    translatorStatus: entry.translatorStatus,
  };
}

function formatTranslateLog(log: TranslateLog) {
  const pieces = [
    `[${log.timestamp || ""}]`,
    log.translator || "translator",
    log.title || log.kind || "log",
  ].filter(Boolean);

  const header = pieces.join(" ");
  const content = (log.content || "").trim();
  return content ? `${header}\n${content}` : header;
}

function importerUsesFileSource(name: string) {
    return name === "ks-extract-csv" || name === "entry-jsonl";
}

function exporterSupportsSkipEmptyFinal(name: string) {
    return name === "tab-text";
}

function isAutomaticTranslator(name: string) {
    return name !== "manual";
}

function getOpenAISettingsKey(name: string): OpenAISettingsKey | null {
    if (name === "openai-chat") {
        return "openAIChat";
    }
    if (name === "openai-responses") {
        return "openAIResponses";
    }
    return null;
}

function normalizeOpenAIProvider(config: Partial<OpenAIProviderConfig> | undefined): OpenAIProviderConfig {
    return {
        ...emptyOpenAIProvider,
        ...config,
    };
}

function normalizeTranslationSettings(config: Partial<TranslationSettings> | undefined): TranslationSettings {
    return {
        ...emptyTranslationSettings,
        ...config,
        proxy: {
            ...emptyTranslationSettings.proxy,
            ...config?.proxy,
        },
        google: {
            ...emptyTranslationSettings.google,
            ...config?.google,
        },
        baidu: {
            ...emptyTranslationSettings.baidu,
            ...config?.baidu,
        },
        asr: {
            ...emptyTranslationSettings.asr,
            ...config?.asr,
        },
        openAIChat: normalizeOpenAIProvider(config?.openAIChat),
        openAIResponses: normalizeOpenAIProvider(config?.openAIResponses),
    };
}

function normalizeSettings(settings: Partial<Settings> | null | undefined): Settings {
    return {
        ...emptySettings,
        ...settings,
        translation: normalizeTranslationSettings(settings?.translation),
    };
}

function getImportSourceDialog(name: string, t: TranslateFn) {
    if (name === "ks-extract-csv") {
        return {
            title: t("dialogs.selectKsExtractCsv"),
            displayName: t("dialogs.csvFiles"),
            pattern: "*.csv",
        };
    }
    if (name === "entry-jsonl") {
        return {
            title: t("dialogs.selectEntryJSONL"),
            displayName: t("dialogs.jsonlFiles"),
            pattern: "*.jsonl",
        };
    }
    if (name === "translated-csv") {
        return {
            title: t("dialogs.selectTranslatedCsv"),
            displayName: t("dialogs.csvFiles"),
            pattern: "*.csv",
        };
    }
    return {
        title: t("dialogs.selectArcSourceTextFile"),
        displayName: t("dialogs.textFiles"),
        pattern: "*.txt",
    };
}

function getExportSaveDialog(name: string, t: TranslateFn) {
    if (name === "entry-jsonl") {
        return {
            title: t("dialogs.selectExportFile"),
            displayName: t("dialogs.jsonlFiles"),
            pattern: "*.jsonl",
            defaultFilename: defaultExportFilename(name),
        };
    }
    return {
        title: t("dialogs.selectExportFile"),
        displayName: t("dialogs.textFiles"),
        pattern: "*.txt",
        defaultFilename: defaultExportFilename(name),
    };
}

function getGlossaryImportDialog(t: TranslateFn) {
    return {
        title: t("dialogs.selectGlossaryImportFile"),
        displayName: t("dialogs.jsonFiles"),
        pattern: "*.json",
    };
}

function getGlossaryExportDialog(t: TranslateFn) {
    return {
        title: t("dialogs.selectGlossaryExportFile"),
        displayName: t("dialogs.jsonFiles"),
        pattern: "*.json",
        defaultFilename: "glossary.json",
    };
}

function formatImportPhase(phase: string, t: TranslateFn) {
    switch (phase) {
        case "starting":
            return t("importProgress.phase.starting");
        case "running":
            return t("importProgress.phase.running");
        case "committing":
            return t("importProgress.phase.committing");
        case "completed":
            return t("importProgress.phase.completed");
        case "failed":
            return t("importProgress.phase.failed");
        default:
            return phase || t("importProgress.phase.pending");
    }
}

function formatExportPhase(phase: string, t: TranslateFn) {
    switch (phase) {
        case "starting":
            return t("exportProgress.phase.starting");
        case "running":
            return t("exportProgress.phase.running");
        case "flushing":
            return t("exportProgress.phase.flushing");
        case "completed":
            return t("exportProgress.phase.completed");
        case "failed":
            return t("exportProgress.phase.failed");
        default:
            return phase || t("exportProgress.phase.pending");
    }
}

function formatTranslatePhase(phase: string, t: TranslateFn) {
    switch (phase) {
        case "starting":
            return t("translateProgress.phase.starting");
        case "running":
            return t("translateProgress.phase.running");
        case "stopped":
            return t("translateProgress.phase.stopped");
        case "completed":
            return t("translateProgress.phase.completed");
        case "failed":
            return t("translateProgress.phase.failed");
        default:
            return phase || t("translateProgress.phase.pending");
    }
}

function translateStatus(value: string, t: TranslateFn) {
    return t(`status.${value}`, {defaultValue: value});
}

function translateArcStatus(value: string, t: TranslateFn) {
    return t(`arcStatus.${value}`, {defaultValue: value});
}

function translateImporterLabel(value: string, t: TranslateFn) {
    return t(`importers.${value}.label`, {defaultValue: value});
}

function translateImporterHelp(value: string, t: TranslateFn) {
    return t(`importers.${value}.help`, {defaultValue: value});
}

function translateExporterLabel(value: string, t: TranslateFn) {
    return t(`exporters.${value}.label`, {defaultValue: value});
}

function translateExporterHelp(value: string, t: TranslateFn) {
    return t(`exporters.${value}.help`, {defaultValue: t("exportSection.help")});
}

function translateTranslatorLabel(value: string, t: TranslateFn) {
    return t(`translators.${value}.label`, {defaultValue: value});
}

function translateTranslatorHelp(value: string, t: TranslateFn) {
    return t(`translators.${value}.help`, {defaultValue: value});
}

function translateTargetFieldLabel(value: string, t: TranslateFn) {
    return t(`translationTarget.${value}`, {defaultValue: value});
}

function optionalNumberToInput(value: number | null | undefined) {
    return value == null ? "" : String(value);
}

function parseOptionalNumber(value: string) {
    const trimmed = value.trim();
    if (trimmed === "") {
        return null;
    }
    const parsed = Number(trimmed);
    return Number.isFinite(parsed) ? parsed : null;
}

function parseInteger(value: string, fallback: number) {
    const parsed = Number(value);
    if (!Number.isFinite(parsed)) {
        return fallback;
    }
    return Math.max(1, Math.round(parsed));
}

function isCanceledError(error: unknown) {
    const message = getErrorMessage(error).toLowerCase();
    return message.includes("context canceled") ||
        message.includes("context cancelled") ||
        message.includes("operation canceled") ||
        message.includes("operation cancelled");
}

function createGlossaryRow(value: Partial<Omit<GlossaryRow, "id">> = {}): GlossaryRow {
    return {
        id: `glossary-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
        source: value.source ?? "",
        preferred: value.preferred ?? "",
        note: value.note ?? "",
    };
}

function glossaryRowsFromJSONValue(value: unknown): GlossaryRow[] {
    if (Array.isArray(value)) {
        return value
            .map((item) => {
                if (!item || typeof item !== "object") {
                    return null;
                }
                const body = item as Record<string, unknown>;
                const source = typeof body.source === "string"
                    ? body.source
                    : typeof body.term === "string"
                        ? body.term
                        : typeof body.match === "string"
                            ? body.match
                            : typeof body.speaker === "string"
                                ? body.speaker
                                : typeof body.role === "string"
                                    ? body.role
                                    : typeof body.voice_id === "string"
                                        ? body.voice_id
                                        : typeof body.type === "string"
                                            ? body.type
                                            : typeof body.source_arc === "string"
                                                ? body.source_arc
                                                : typeof body.source_file === "string"
                                                    ? body.source_file
                                                    : "";
                const preferred = typeof body.preferred === "string"
                    ? body.preferred
                    : typeof body.target === "string"
                        ? body.target
                        : typeof body.translation === "string"
                            ? body.translation
                            : "";
                const noteParts: string[] = [];
                if (typeof body.note === "string" && body.note.trim() !== "") {
                    noteParts.push(body.note);
                }
                if (typeof body.speaker === "string" && body.speaker.trim() !== "" && source !== body.speaker) {
                    noteParts.push(`speaker=${body.speaker}`);
                }
                if (typeof body.voice_id === "string" && body.voice_id.trim() !== "" && source !== body.voice_id) {
                    noteParts.push(`voice_id=${body.voice_id}`);
                }
                if (typeof body.type === "string" && body.type.trim() !== "" && source !== body.type) {
                    noteParts.push(`type=${body.type}`);
                }
                if (typeof body.source_arc === "string" && body.source_arc.trim() !== "" && source !== body.source_arc) {
                    noteParts.push(`source_arc=${body.source_arc}`);
                }
                if (typeof body.source_file === "string" && body.source_file.trim() !== "" && source !== body.source_file) {
                    noteParts.push(`source_file=${body.source_file}`);
                }
                const note = noteParts.join(" | ");
                if (source.trim() === "" && preferred.trim() === "" && note.trim() === "") {
                    return null;
                }
                return createGlossaryRow({source, preferred, note});
            })
            .filter((row): row is GlossaryRow => row !== null);
    }

    if (value && typeof value === "object" && Array.isArray((value as {entries?: unknown[]}).entries)) {
        return glossaryRowsFromJSONValue((value as {entries: unknown[]}).entries);
    }

    if (value && typeof value === "object") {
        return Object.entries(value as Record<string, unknown>)
            .map(([source, preferred]) => createGlossaryRow({
                source,
                preferred: typeof preferred === "string" ? preferred : "",
            }));
    }

    return [];
}

function glossaryRowsFromLegacyText(raw: string): GlossaryRow[] {
    return raw
        .split(/\r?\n/)
        .map((line) => line.trim())
        .filter((line) => line !== "" && !line.startsWith("#") && !line.startsWith("//"))
        .map((line) => {
            if (line.includes("\t")) {
                const parts = line.split("\t");
                return createGlossaryRow({
                    source: parts[0]?.trim() ?? "",
                    preferred: parts[1]?.trim() ?? "",
                    note: parts.slice(2).join("\t").trim(),
                });
            }
            if (line.includes("=>")) {
                const [source, rest] = line.split("=>", 2);
                const [preferred, note] = rest.split("|", 2);
                return createGlossaryRow({
                    source: source.trim(),
                    preferred: preferred?.trim() ?? "",
                    note: note?.trim() ?? "",
                });
            }
            if (line.includes("->")) {
                const [source, rest] = line.split("->", 2);
                const [preferred, note] = rest.split("|", 2);
                return createGlossaryRow({
                    source: source.trim(),
                    preferred: preferred?.trim() ?? "",
                    note: note?.trim() ?? "",
                });
            }
            if (line.includes("|")) {
                const [source, note] = line.split("|", 2);
                return createGlossaryRow({
                    source: source.trim(),
                    note: note?.trim() ?? "",
                });
            }
            return createGlossaryRow({source: line});
        });
}

function glossaryRowsFromValue(raw: string) {
    const trimmed = raw.trim().replace(/^\uFEFF/, "");
    if (trimmed === "") {
        return [createGlossaryRow()];
    }

    try {
        const parsed = JSON.parse(trimmed);
        const rows = glossaryRowsFromJSONValue(parsed);
        if (rows.length > 0) {
            return rows;
        }
    } catch {
        // fall through to legacy text parsing
    }

    const rows = glossaryRowsFromLegacyText(trimmed);
    return rows.length > 0 ? rows : [createGlossaryRow()];
}

function glossaryRowsToValue(rows: GlossaryRow[]) {
    const entries = rows
        .map((row) => ({
            source: row.source.trim(),
            preferred: row.preferred.trim(),
            note: row.note.trim(),
        }))
        .filter((row) => row.source !== "")
        .map((row) => {
            const body: Record<string, string> = {source: row.source};
            if (row.preferred !== "") {
                body.preferred = row.preferred;
            }
            if (row.note !== "") {
                body.note = row.note;
            }
            return body;
        });

    if (entries.length === 0) {
        return "";
    }
    return JSON.stringify(entries, null, 2);
}

function glossaryRowsToFileContent(rows: GlossaryRow[]) {
    const value = glossaryRowsToValue(rows);
    return value === "" ? "[]\n" : `${value}\n`;
}

function countGlossaryEntries(rows: GlossaryRow[]) {
    return rows.filter((row) => row.source.trim() !== "").length;
}

function serializeSettings(value: Settings) {
    return JSON.stringify(normalizeSettings(value));
}

function App() {
    const {t, i18n} = useTranslation();
    const [page, setPage] = useState<Page>("translate");
    const [settings, setSettings] = useState<Settings>(emptySettings);
    const [importSourcePath, setImportSourcePath] = useState("");
    const [exportPath, setExportPath] = useState("");
    const [query, setQuery] = useState<EntryQuery>(defaultQuery);
    const [arcs, setArcs] = useState<ArcFile[]>([]);
    const [entries, setEntries] = useState<Entry[]>([]);
    const [entryTotal, setEntryTotal] = useState(0);
    const [filters, setFilters] = useState<FilterOptions>({
        arcs: [],
        files: [],
        types: [],
        statuses: defaultStatuses,
    });
    const [importers, setImporters] = useState<string[]>([]);
    const [exporters, setExporters] = useState<string[]>([]);
    const [translators, setTranslators] = useState<string[]>([]);
    const [selectedImporter, setSelectedImporter] = useState("arc-ks-folder-text");
    const [selectedExporter, setSelectedExporter] = useState("tab-text");
    const [allowOverwrite, setAllowOverwrite] = useState(true);
    const [skipEmptyFinal, setSkipEmptyFinal] = useState(true);
    const [translationTargetField, setTranslationTargetField] = useState("translated");
    const [translationAllowOverwrite, setTranslationAllowOverwrite] = useState(false);
    const [sourceRecognitionAllowOverwrite, setSourceRecognitionAllowOverwrite] = useState(false);
    const [batchStatus, setBatchStatus] = useState("reviewed");
    const [importProgress, setImportProgress] = useState<ImportProgress | null>(null);
    const [exportProgress, setExportProgress] = useState<ExportProgress | null>(null);
    const [translateProgress, setTranslateProgress] = useState<TranslateProgress | null>(null);
    const [busy, setBusy] = useState(false);
    const [activeTask, setActiveTask] = useState<CancellableTask>("");
    const [stopRequested, setStopRequested] = useState(false);
    const [testingProxy, setTestingProxy] = useState(false);
    const [testingASR, setTestingASR] = useState(false);
    const [testingTranslator, setTestingTranslator] = useState(false);
    const [glossaryRows, setGlossaryRows] = useState<GlossaryRow[]>(() => [createGlossaryRow()]);
    const [message, setMessage] = useState("");
    const autosaveTimersRef = useRef<Map<number, ReturnType<typeof setTimeout>>>(new Map());
    const autosavePendingRef = useRef<Map<number, UpdateEntryInput>>(new Map());
    const autosaveInFlightRef = useRef<Map<number, Promise<void>>>(new Map());
    const settingsRef = useRef(settings);
    const settingsAutosaveTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const settingsAutosaveInFlightRef = useRef<Promise<void> | null>(null);
    const settingsAutosaveQueuedRef = useRef(false);
    const settingsAutosaveReadyRef = useRef(false);
    const settingsSavedSnapshotRef = useRef(serializeSettings(emptySettings));
    const currentLanguage = i18n.resolvedLanguage === "zh" ? "zh" : "en";
    const currentTranslator = settings.translation.activeTranslator || "manual";
    const currentOpenAISettingsKey = getOpenAISettingsKey(currentTranslator);
    const currentImportPath = importSourcePath || settings.importDir;
    const statusOptions = filters.statuses.length > 0 ? filters.statuses : defaultStatuses;
    const batchTranslationProgress =
        translateProgress && translateProgress.translator !== asrTranslatorName
            ? translateProgress
            : null;
    const sourceRecognitionProgress =
        translateProgress && translateProgress.translator === asrTranslatorName
            ? translateProgress
            : null;
    const failedArcCount = arcs.filter((arc) => arc.status === "failed").length;
    const canRunFilteredBatchActions = isQueryFiltered(query) && entryTotal > 0;
    const pageSize = query.limit > 0 ? query.limit : entryPageSize;
    const totalPages = Math.max(1, Math.ceil(entryTotal / pageSize));
    const currentPage = Math.min(totalPages, Math.floor(query.offset / pageSize) + 1);
    const pageStart = entryTotal === 0 ? 0 : query.offset + 1;
    const pageEnd = entryTotal === 0 ? 0 : Math.min(query.offset + entries.length, entryTotal);

  async function changeLanguage(nextLanguage: string) {
    await i18n.changeLanguage(nextLanguage);
    persistLanguage(nextLanguage);
  }

  function replaceStatusMessage(text: string) {
    setMessage(trimStatusMessage(text.trim()));
  }

  function appendStatusMessage(text: string) {
    const next = text.trim();
    if (next === "") {
      return;
    }
    setMessage((current) => trimStatusMessage(current.trim() === "" ? next : `${current}\n\n${next}`));
  }

  function clearAutosaveTimer(id: number) {
        const timer = autosaveTimersRef.current.get(id);
        if (timer) {
            clearTimeout(timer);
            autosaveTimersRef.current.delete(id);
        }
    }

    function clearSettingsAutosaveTimer() {
        if (settingsAutosaveTimerRef.current) {
            clearTimeout(settingsAutosaveTimerRef.current);
            settingsAutosaveTimerRef.current = null;
        }
    }

    async function flushSettingsAutosave() {
        clearSettingsAutosaveTimer();

        if (!settingsAutosaveReadyRef.current) {
            return;
        }

        const currentSnapshot = serializeSettings(settingsRef.current);
        if (currentSnapshot === settingsSavedSnapshotRef.current) {
            return;
        }

        if (settingsAutosaveInFlightRef.current) {
            settingsAutosaveQueuedRef.current = true;
            return;
        }

        const nextSettings = normalizeSettings(settingsRef.current);
        const promise = (async () => {
            try {
                await api.saveSettings(nextSettings);
                settingsSavedSnapshotRef.current = serializeSettings(nextSettings);
            } catch (error) {
                replaceStatusMessage(t("messages.settingsAutoSaveFailed", {error: getErrorMessage(error)}));
            } finally {
                settingsAutosaveInFlightRef.current = null;
                if (settingsAutosaveQueuedRef.current) {
                    settingsAutosaveQueuedRef.current = false;
                    void flushSettingsAutosave();
                }
            }
        })();

        settingsAutosaveInFlightRef.current = promise;
        await promise;
    }

    async function flushEntryAutosave(id: number) {
        clearAutosaveTimer(id);

        const currentInFlight = autosaveInFlightRef.current.get(id);
        if (currentInFlight) {
            await currentInFlight;
            return;
        }

        const input = autosavePendingRef.current.get(id);
        if (!input) {
            return;
        }
        autosavePendingRef.current.delete(id);

        const promise = (async () => {
            try {
                await api.updateEntry(input);
            } catch (error) {
                replaceStatusMessage(t("messages.autoSaveFailed", {id, error: getErrorMessage(error)}));
            } finally {
                autosaveInFlightRef.current.delete(id);
                if (autosavePendingRef.current.has(id)) {
                    clearAutosaveTimer(id);
                    autosaveTimersRef.current.set(id, setTimeout(() => {
                        void flushEntryAutosave(id);
                    }, entryAutosaveDelayMs));
                }
            }
        })();

        autosaveInFlightRef.current.set(id, promise);
        await promise;
    }

    function queueEntryAutosave(entry: Entry) {
        autosavePendingRef.current.set(entry.id, toUpdateEntryInput(entry));
        clearAutosaveTimer(entry.id);
        autosaveTimersRef.current.set(entry.id, setTimeout(() => {
            void flushEntryAutosave(entry.id);
        }, entryAutosaveDelayMs));
    }

    async function flushAllAutosaves() {
        const ids = new Set<number>([
            ...autosavePendingRef.current.keys(),
            ...autosaveInFlightRef.current.keys(),
        ]);

        for (const id of autosaveTimersRef.current.keys()) {
            clearAutosaveTimer(id);
        }

        for (const id of ids) {
            await flushEntryAutosave(id);
            const inFlight = autosaveInFlightRef.current.get(id);
            if (inFlight) {
                await inFlight;
            }
            if (autosavePendingRef.current.has(id)) {
                clearAutosaveTimer(id);
                await flushEntryAutosave(id);
            }
        }
    }

    async function refreshArcs() {
        setArcs(ensureArray(await api.listArcs()));
    }

    async function refreshEntries(nextQuery = query) {
        const sanitizedQuery = {
            ...nextQuery,
            limit: nextQuery.limit > 0 ? nextQuery.limit : entryPageSize,
            offset: Math.max(0, nextQuery.offset),
        };
        const response = await api.listEntries(sanitizedQuery);
        if (response.total > 0 && sanitizedQuery.offset >= response.total) {
            const lastOffset = Math.max(0, Math.floor((response.total - 1) / sanitizedQuery.limit) * sanitizedQuery.limit);
            if (lastOffset !== sanitizedQuery.offset) {
                const adjustedQuery = {...sanitizedQuery, offset: lastOffset};
                setQuery(adjustedQuery);
                const adjustedResponse = await api.listEntries(adjustedQuery);
                setEntries(ensureArray(adjustedResponse.items));
                setEntryTotal(adjustedResponse.total);
                return adjustedResponse.total;
            }
        }
        setEntries(ensureArray(response.items));
        setEntryTotal(response.total);
        return response.total;
    }

    async function refreshFilters() {
        const nextFilters = await api.getFilterOptions();
        const statuses = ensureArray(nextFilters?.statuses);
        setFilters({
            arcs: ensureArray(nextFilters?.arcs),
            files: ensureArray(nextFilters?.files),
            types: ensureArray(nextFilters?.types),
            statuses: statuses.length > 0 ? statuses : defaultStatuses,
        });
    }

    async function bootstrap() {
        setBusy(true);
        try {
            const [settingsResult, importersResult, exportersResult, translatorsResult] = await Promise.allSettled([
                api.getSettings(),
                api.listImporters(),
                api.listExporters(),
                api.listTranslators(),
            ]);

            const messages: string[] = [];

            const normalizedImporters =
                importersResult.status === "fulfilled"
                    ? ensureArray(importersResult.value)
                    : [];
            const nextImporters = normalizedImporters.length > 0 ? normalizedImporters : defaultImporters;
            setImporters(nextImporters);
            setSelectedImporter(nextImporters[0]);
            if (importersResult.status === "rejected") {
                messages.push(t("messages.listImportersFailed", {error: getErrorMessage(importersResult.reason)}));
            }

            const normalizedExporters =
                exportersResult.status === "fulfilled"
                    ? ensureArray(exportersResult.value)
                    : [];
            const nextExporters = normalizedExporters.length > 0 ? normalizedExporters : defaultExporters;
            setExporters(nextExporters);
            setSelectedExporter(nextExporters[0]);
            if (exportersResult.status === "rejected") {
                messages.push(t("messages.listExportersFailed", {error: getErrorMessage(exportersResult.reason)}));
            }

            const normalizedTranslators =
                translatorsResult.status === "fulfilled"
                    ? ensureArray(translatorsResult.value)
                    : [];
            const nextTranslators = normalizedTranslators.length > 0 ? normalizedTranslators : defaultTranslators;
            setTranslators(nextTranslators);
            if (translatorsResult.status === "rejected") {
                messages.push(t("messages.listTranslatorsFailed", {error: getErrorMessage(translatorsResult.reason)}));
            }

            if (settingsResult.status === "fulfilled") {
                const nextSettings = normalizeSettings(settingsResult.value);
                const activeTranslator =
                    nextSettings.translation.activeTranslator && nextTranslators.includes(nextSettings.translation.activeTranslator)
                        ? nextSettings.translation.activeTranslator
                        : nextTranslators[0];
                const initializedSettings = {
                    ...nextSettings,
                    translation: {
                        ...nextSettings.translation,
                        activeTranslator,
                    },
                };

                settingsSavedSnapshotRef.current = serializeSettings(initializedSettings);
                setSettings(initializedSettings);
                setGlossaryRows(glossaryRowsFromValue(nextSettings.translation.glossary));
                setImportSourcePath(nextSettings.importDir);
                setExportPath(buildDefaultExportPath(nextSettings.exportDir, nextExporters[0]));
            } else {
                settingsSavedSnapshotRef.current = serializeSettings(emptySettings);
                messages.push(t("messages.getSettingsFailed", {error: getErrorMessage(settingsResult.reason)}));
            }

            await Promise.all([refreshArcs(), refreshEntries(defaultQuery), refreshFilters()]);
            replaceStatusMessage(messages.length > 0 ? messages.join("\n") : t("common.ready"));
        } catch (error) {
            replaceStatusMessage(getErrorMessage(error));
        } finally {
            settingsRef.current = settings;
            settingsAutosaveReadyRef.current = true;
            setBusy(false);
        }
    }

    useEffect(() => {
        bootstrap();
    }, []);

    useEffect(() => {
        document.documentElement.lang = currentLanguage;
        document.title = t("common.appName");
    }, [currentLanguage, t]);

    useEffect(() => {
        if (translators.length > 0 && !translators.includes(settings.translation.activeTranslator)) {
            setSettings((current) => ({
                ...current,
                translation: {
                    ...current.translation,
                    activeTranslator: translators[0],
                },
            }));
        }
    }, [settings.translation.activeTranslator, translators]);

    useEffect(() => {
        settingsRef.current = settings;
        if (!settingsAutosaveReadyRef.current) {
            return;
        }
        if (serializeSettings(settings) === settingsSavedSnapshotRef.current) {
            clearSettingsAutosaveTimer();
            return;
        }

        clearSettingsAutosaveTimer();
        settingsAutosaveTimerRef.current = setTimeout(() => {
            void flushSettingsAutosave();
        }, settingsAutosaveDelayMs);
    }, [settings]);

    useEffect(() => {
        const unsubscribe = EventsOn("import:progress", (...data: unknown[]) => {
            const next = data[0] as ImportProgress | undefined;
            if (next) {
                setImportProgress(next);
            }
        });
        return unsubscribe;
    }, []);

    useEffect(() => {
        const unsubscribe = EventsOn("export:progress", (...data: unknown[]) => {
            const next = data[0] as ExportProgress | undefined;
            if (next) {
                setExportProgress(next);
            }
        });
        return unsubscribe;
    }, []);

  useEffect(() => {
    const unsubscribe = EventsOn("translate:progress", (...data: unknown[]) => {
      const next = data[0] as TranslateProgress | undefined;
      if (next) {
        setTranslateProgress(next);
            }
    });
    return unsubscribe;
  }, []);

  useEffect(() => {
    const unsubscribe = EventsOn("translate:log", (...data: unknown[]) => {
      const next = data[0] as TranslateLog | undefined;
      if (next) {
        appendStatusMessage(formatTranslateLog(next));
      }
    });
    return unsubscribe;
  }, []);

  useEffect(() => () => {
        clearSettingsAutosaveTimer();
        settingsAutosaveReadyRef.current = false;
        for (const timer of autosaveTimersRef.current.values()) {
            clearTimeout(timer);
        }
        autosaveTimersRef.current.clear();
        autosavePendingRef.current.clear();
    }, []);

    async function runBusyTask(task: () => Promise<void>) {
        setBusy(true);
        try {
            await task();
        } catch (error) {
            replaceStatusMessage(getErrorMessage(error));
        } finally {
            setBusy(false);
        }
    }

    async function runCancellableTask(task: CancellableTask, work: () => Promise<void>) {
        setBusy(true);
        setActiveTask(task);
        setStopRequested(false);
        try {
            await work();
        } catch (error) {
            if (isCanceledError(error)) {
                appendStatusMessage(t("messages.taskStopped"));
            } else {
                replaceStatusMessage(getErrorMessage(error));
            }
        } finally {
            setBusy(false);
            setActiveTask("");
            setStopRequested(false);
        }
    }

    async function stopCurrentTask() {
        if (!activeTask || stopRequested) {
            return;
        }

        setStopRequested(true);
        try {
            const stopped = await api.stopCurrentTask();
            if (stopped) {
                appendStatusMessage(t("messages.stopRequested"));
                return;
            }
            setStopRequested(false);
        } catch (error) {
            setStopRequested(false);
            replaceStatusMessage(getErrorMessage(error));
        }
    }

    function updateQueryField<K extends keyof EntryQuery>(key: K, value: EntryQuery[K]) {
        setQuery((current) => ({...current, [key]: value}));
    }

    function updateEntryField(id: number, key: "translatedText" | "polishedText" | "translatorStatus", value: string) {
        let nextEntry: Entry | null = null;
        setEntries((current) =>
            current.map((entry) => {
                if (entry.id !== id) {
                    return entry;
                }
                nextEntry = {...entry, [key]: value};
                return nextEntry;
            }),
        );
        if (nextEntry) {
            queueEntryAutosave(nextEntry);
        }
    }

    function updateTranslationField<K extends keyof TranslationSettings>(key: K, value: TranslationSettings[K]) {
        setSettings((current) => ({
            ...current,
            translation: {
                ...current.translation,
                [key]: value,
            },
        }));
    }

    function updateGoogleField<K extends keyof GoogleTranslateConfig>(key: K, value: GoogleTranslateConfig[K]) {
        setSettings((current) => ({
            ...current,
            translation: {
                ...current.translation,
                google: {
                    ...current.translation.google,
                    [key]: value,
                },
            },
        }));
    }

    function updateBaiduField<K extends keyof BaiduTranslateConfig>(key: K, value: BaiduTranslateConfig[K]) {
        setSettings((current) => ({
            ...current,
            translation: {
                ...current.translation,
                baidu: {
                    ...current.translation.baidu,
                    [key]: value,
                },
            },
        }));
    }

    function updateASRField<K extends keyof ASRConfig>(key: K, value: ASRConfig[K]) {
        setSettings((current) => ({
            ...current,
            translation: {
                ...current.translation,
                asr: {
                    ...current.translation.asr,
                    [key]: value,
                },
            },
        }));
    }

    function updateOpenAIField<K extends keyof OpenAIProviderConfig>(
        provider: OpenAISettingsKey,
        key: K,
        value: OpenAIProviderConfig[K],
    ) {
        setSettings((current) => ({
            ...current,
            translation: {
                ...current.translation,
                [provider]: {
                    ...current.translation[provider],
                    [key]: value,
                },
            },
        }));
    }

    function applyGlossaryRows(nextRows: GlossaryRow[]) {
        const sanitized = nextRows.length > 0 ? nextRows : [createGlossaryRow()];
        setGlossaryRows(sanitized);
        setSettings((current) => ({
            ...current,
            translation: {
                ...current.translation,
                glossary: glossaryRowsToValue(sanitized),
            },
        }));
    }

    function updateGlossaryRow(id: string, key: keyof Omit<GlossaryRow, "id">, value: string) {
        applyGlossaryRows(
            glossaryRows.map((row) => row.id === id ? {...row, [key]: value} : row),
        );
    }

    function addGlossaryRow() {
        applyGlossaryRows([...glossaryRows, createGlossaryRow()]);
    }

    function removeGlossaryRow(id: string) {
        applyGlossaryRows(glossaryRows.filter((row) => row.id !== id));
    }

    async function chooseDirectory(
        currentPath: string,
        title: string,
        onSelected: (selected: string) => void,
    ) {
        const selected = await api.browseDirectory(title, currentPath);
        if (selected) {
            onSelected(selected);
        }
    }

    async function chooseSettingsDirectory(key: keyof Settings, title: string) {
        await chooseDirectory(settings[key] as string, title, (selected) => {
            const previousExportDir = settings.exportDir;
            setSettings((current) => ({...current, [key]: selected}));

            if (key === "importDir" && importSourcePath.trim() === "") {
                setImportSourcePath(selected);
            }

            if (key === "exportDir") {
                const previousDefault = buildDefaultExportPath(previousExportDir, selectedExporter);
                const nextDefault = buildDefaultExportPath(selected, selectedExporter);
                setExportPath((current) => {
                    if (current.trim() === "" || current === previousDefault) {
                        return nextDefault;
                    }
                    return current;
                });
            }
        });
    }

    async function chooseImportSource() {
        if (importerUsesFileSource(selectedImporter)) {
            const dialog = getImportSourceDialog(selectedImporter, t);
            const selected = await api.browseOpenFile(
                dialog.title,
                importSourcePath || settings.importDir,
                dialog.displayName,
                dialog.pattern,
            );
            if (selected) {
                setImportSourcePath(selected);
            }
            return;
        }

        await chooseDirectory(
            importSourcePath || settings.importDir,
            t("importSection.selectSourceDirectory"),
            setImportSourcePath,
        );
    }

    async function chooseExportFile() {
        const dialog = getExportSaveDialog(selectedExporter, t);
        const selected = await api.browseSaveFileWithFilter(
            dialog.title,
            exportPath || buildDefaultExportPath(settings.exportDir, selectedExporter),
            dialog.defaultFilename,
            dialog.displayName,
            dialog.pattern,
        );
        if (selected) {
            setExportPath(selected);
        }
    }

    async function importGlossary() {
        await runBusyTask(async () => {
            const dialog = getGlossaryImportDialog(t);
            const selected = await api.browseOpenFile(
                dialog.title,
                settings.importDir,
                dialog.displayName,
                dialog.pattern,
            );
            if (!selected) {
                return;
            }

            const content = await api.readTextFile(selected);
            const nextRows = glossaryRowsFromValue(content);
            applyGlossaryRows(nextRows);
            replaceStatusMessage(t("messages.glossaryImported", {
                path: selected,
                count: countGlossaryEntries(nextRows),
            }));
        });
    }

    async function exportGlossary() {
        await runBusyTask(async () => {
            const dialog = getGlossaryExportDialog(t);
            const selected = await api.browseSaveFileWithFilter(
                dialog.title,
                joinPath(settings.exportDir, dialog.defaultFilename),
                dialog.defaultFilename,
                dialog.displayName,
                dialog.pattern,
            );
            if (!selected) {
                return;
            }

            await api.writeTextFile(selected, glossaryRowsToFileContent(glossaryRows));
            replaceStatusMessage(t("messages.glossaryExported", {
                path: selected,
                count: countGlossaryEntries(glossaryRows),
            }));
        });
    }

    async function testProxy() {
        setTestingProxy(true);
        try {
            const result = await api.testProxy(settings.translation.proxy);
            const resolvedProxy = result.resolvedProxy.trim() !== ""
                ? result.resolvedProxy
                : t("translationSettings.proxy.direct");
            replaceStatusMessage(t("messages.proxyTestSuccess", {
                targetUrl: result.targetUrl,
                finalUrl: result.finalUrl,
                status: result.status,
                proxyMode: result.proxyMode,
                proxy: resolvedProxy,
            }));
        } catch (error) {
            replaceStatusMessage(t("messages.proxyTestFailed", {error: getErrorMessage(error)}));
        } finally {
            setTestingProxy(false);
        }
    }

    async function testCurrentTranslator() {
        if (!isAutomaticTranslator(currentTranslator)) {
            replaceStatusMessage(t("messages.translatorTestManual"));
            return;
        }

        setTestingTranslator(true);
        try {
            const result = await api.testTranslator({
                translator: currentTranslator,
                targetField: translationTargetField,
                settings: settings.translation,
            });
            replaceStatusMessage(t("messages.translatorTestSuccess", {
                translator: translateTranslatorLabel(result.translator, t),
                sourceText: result.sourceText,
                outputText: result.outputText,
                responseTime: result.responseTime,
                targetField: translateTargetFieldLabel(result.targetField, t),
            }));
        } catch (error) {
            replaceStatusMessage(t("messages.translatorTestFailed", {
                translator: translateTranslatorLabel(currentTranslator, t),
                error: getErrorMessage(error),
            }));
        } finally {
            setTestingTranslator(false);
        }
    }

    async function testASR() {
        setTestingASR(true);
        try {
            const result = await api.testASR(settings.translation);
            const resolvedProxy = result.resolvedProxy.trim() !== ""
                ? result.resolvedProxy
                : t("translationSettings.proxy.direct");
            const responseText = result.responseText.trim() !== ""
                ? result.responseText
                : t("messages.asrTestSilentSample");
            const responseLanguage = result.responseLanguage.trim() !== ""
                ? result.responseLanguage
                : "-";
            replaceStatusMessage(t("messages.asrTestSuccess", {
                endpoint: result.endpoint,
                finalUrl: result.finalUrl,
                status: result.status,
                proxyMode: result.proxyMode,
                proxy: resolvedProxy,
                responseTime: result.responseTime,
                language: responseLanguage,
                text: responseText,
                responseBody: result.responseBody || "{}",
            }));
        } catch (error) {
            replaceStatusMessage(t("messages.asrTestFailed", {error: getErrorMessage(error)}));
        } finally {
            setTestingASR(false);
        }
    }

    async function scanArcs() {
        await runCancellableTask("scan", async () => {
            const result = await api.scanArcs();
            await Promise.all([refreshArcs(), refreshEntries(), refreshFilters()]);
            setMessage(appendSummary(
                t("messages.scanComplete", {
                    scanned: result.scanned,
                    newArcCount: result.newArcCount,
                    parsedCount: result.parsedCount,
                    failedCount: result.failedCount,
                }),
                formatSummary(result.messages),
            ));
        });
    }

    async function reparseArc(arcID: number) {
        await runCancellableTask("reparse", async () => {
            await flushAllAutosaves();
            const result = await api.reparseArc(arcID);
            await Promise.all([refreshArcs(), refreshEntries(), refreshFilters()]);
            setMessage(result.message || t("messages.reparsed", {arcFilename: result.arcFilename}));
        });
    }

    async function reparseAllArcs() {
        await runCancellableTask("reparse", async () => {
            await flushAllAutosaves();
            const result = await api.reparseAllArcs();
            await Promise.all([refreshArcs(), refreshEntries(), refreshFilters()]);
            setMessage(appendSummary(
                t("messages.reparseAllComplete", {
                    totalArcs: result.totalArcs,
                    reparsedCount: result.reparsedCount,
                    failedCount: result.failedCount,
                    skippedCount: result.skippedCount,
                }),
                formatSummary(ensureArray(result.messages).slice(0, 8)),
            ));
        });
    }

    async function reparseFailedArcs() {
        await runCancellableTask("reparse", async () => {
            await flushAllAutosaves();
            const result = await api.reparseFailedArcs();
            await Promise.all([refreshArcs(), refreshEntries(), refreshFilters()]);
            setMessage(appendSummary(
                t("messages.reparseFailedComplete", {
                    totalFailed: result.totalFailed,
                    reparsedCount: result.reparsedCount,
                    failedCount: result.failedCount,
                }),
                formatSummary(ensureArray(result.messages).slice(0, 8)),
            ));
        });
    }

    async function applyFilters() {
        await runBusyTask(async () => {
            await flushAllAutosaves();
            const nextQuery = {...query, offset: 0};
            setQuery(nextQuery);
            const total = await refreshEntries(nextQuery);
            setMessage(t("messages.loadedEntries", {limit: query.limit, total}));
        });
    }

    async function changeEntryPage(nextPage: number) {
        const safePage = Math.min(totalPages, Math.max(1, nextPage));
        const nextQuery = {
            ...query,
            offset: (safePage - 1) * pageSize,
        };

        await runBusyTask(async () => {
            await flushAllAutosaves();
            setQuery(nextQuery);
            await refreshEntries(nextQuery);
        });
    }

    async function applyFilteredStatusUpdate() {
        await runBusyTask(async () => {
            await flushAllAutosaves();
            const result = await api.batchUpdateEntryStatusByQuery({
                query,
                translatorStatus: batchStatus,
            });
            await Promise.all([refreshEntries(), refreshFilters()]);
            setMessage(t("messages.batchStatusUpdated", {
                updated: result.updated,
                status: translateStatus(batchStatus, t),
            }));
        });
    }

    async function clearFilteredTranslations() {
        if (!window.confirm(t("entryTable.clearTranslationsConfirm", {total: entryTotal}))) {
            return;
        }

        await runBusyTask(async () => {
            await flushAllAutosaves();
            const result = await api.clearEntryTranslationsByQuery(query);
            await Promise.all([refreshEntries(), refreshFilters()]);
            setMessage(t("messages.clearTranslationsComplete", {updated: result.updated}));
        });
    }

    async function runImport() {
        const request: ImportRequest = {
            importer: selectedImporter,
            rootDir: importSourcePath || settings.importDir,
            allowOverwrite,
        };

        setImportProgress(null);
        await runCancellableTask("import", async () => {
            const result = await api.runImport(request);
            await Promise.all([refreshEntries(), refreshFilters()]);
            setMessage(appendSummary(
                t("messages.importComplete", {
                    filesProcessed: result.filesProcessed,
                    totalLines: result.totalLines,
                    inserted: result.inserted,
                    updated: result.updated,
                    skipped: result.skipped,
                    unmatched: result.unmatched,
                    errorLines: result.errorLines,
                }),
                formatSummary(ensureArray(result.messages).slice(0, 8)),
            ));
        });
    }

    async function runExport() {
        const request: ExportRequest = {
            exporter: selectedExporter,
            outputPath: exportPath || buildDefaultExportPath(settings.exportDir, selectedExporter),
            search: query.search,
            sourceArc: query.sourceArc,
            sourceFile: query.sourceFile,
            type: query.type,
            status: query.status,
            untranslatedOnly: query.untranslatedOnly,
            skipEmptyFinal,
        };

        setExportProgress(null);
        await runCancellableTask("export", async () => {
            await flushAllAutosaves();
            const result = await api.runExport(request);
            setMessage(
                t("messages.exportComplete", {
                    exported: result.exported,
                    skipped: result.skipped,
                    outputPath: result.outputPath,
                }),
            );
        });
    }

    async function runMaintenanceCleanup() {
        await runBusyTask(async () => {
            await flushAllAutosaves();
            replaceStatusMessage(t("messages.maintenanceStarted"));
            const result = await api.runMaintenance();
            await Promise.all([refreshEntries(), refreshFilters()]);
            const deleted = result.deletedInvisibleBlankEntries ?? 0;
            if (deleted > 0) {
                setMessage(t("messages.maintenanceCleanedInvisibleBlankEntries", {count: deleted}));
                return;
            }
            setMessage(t("messages.maintenanceNoChanges"));
        });
    }

    async function runTranslation() {
        const request: TranslateRequest = {
            translator: currentTranslator,
            search: query.search,
            sourceArc: query.sourceArc,
            sourceFile: query.sourceFile,
            type: query.type,
            status: query.status,
            untranslatedOnly: query.untranslatedOnly,
            allowOverwrite: translationAllowOverwrite,
            targetField: translationTargetField,
        };

        setTranslateProgress(null);
        replaceStatusMessage(t("messages.translateStarted", {translator: translateTranslatorLabel(currentTranslator, t)}));
        await runCancellableTask("translation", async () => {
            await flushAllAutosaves();
            const result = await api.runTranslation(request);
            await Promise.all([refreshEntries(), refreshFilters()]);
            appendStatusMessage(appendSummary(
              t("messages.translateComplete", {
                total: result.total,
                processed: result.processed,
                updated: result.updated,
                skipped: result.skipped,
                failed: result.failed,
              }),
              formatSummary(ensureArray(result.messages).slice(0, 8)),
            ));
        });
    }

    async function runSourceRecognition() {
        const request: SourceRecognitionRequest = {
            search: query.search,
            sourceArc: query.sourceArc,
            sourceFile: query.sourceFile,
            type: query.type,
            status: query.status,
            untranslatedOnly: query.untranslatedOnly,
            allowOverwrite: sourceRecognitionAllowOverwrite,
        };

        setTranslateProgress(null);
        replaceStatusMessage(t("messages.sourceRecognitionStarted", {
            provider: translateTranslatorLabel(asrTranslatorName, t),
        }));
        await runCancellableTask("sourceRecognition", async () => {
            await flushAllAutosaves();
            const result = await api.runSourceRecognition(request);
            await Promise.all([refreshEntries(), refreshFilters()]);
            appendStatusMessage(appendSummary(
                t("messages.sourceRecognitionComplete", {
                    provider: translateTranslatorLabel(result.provider, t),
                    total: result.total,
                    processed: result.processed,
                    updated: result.updated,
                    skipped: result.skipped,
                    failed: result.failed,
                }),
                formatSummary(ensureArray(result.messages).slice(0, 8)),
            ));
        });
    }

    return (
        <div id="app-shell">
            <header className="topbar">
                <div className="brand">
                    <p className="eyebrow">{t("common.workflow")}</p>
                    <h1>{t("common.appName")}</h1>
                </div>
                <nav className="page-tabs">
                    <button
                        className={page === "translate" ? "tab active" : "tab"}
                        disabled={busy}
                        onClick={() => setPage("translate")}
                    >
                        {t("page.translate")}
                    </button>
                    <button
                        className={page === "tools" ? "tab active" : "tab"}
                        disabled={busy}
                        onClick={() => setPage("tools")}
                    >
                        {t("page.tools")}
                    </button>
                </nav>
                <div className="language-switcher">
                    <span>{t("language.label")}</span>
                    <select value={currentLanguage} onChange={(event) => void changeLanguage(event.target.value)}>
                        <option value="zh">{t("language.zh")}</option>
                        <option value="en">{t("language.en")}</option>
                    </select>
                </div>
                <div className="topbar-stats">
                    <span>{t("topbar.arcCount", {count: arcs.length})}</span>
                    <span>{t("topbar.entryCount", {count: entryTotal})}</span>
                    <span>{busy ? t("common.busy") : t("common.idle")}</span>
                    {activeTask && (
                        <button
                            className="danger-button"
                            disabled={stopRequested}
                            onClick={stopCurrentTask}
                        >
                            {stopRequested ? t("common.stopping") : t("common.stop")}
                        </button>
                    )}
                </div>
            </header>

            {page === "translate" ? (
                <>
                    <section className="panel compact-panel">
                        <div className="panel-title-row">
                            <h2>{t("filters.title")}</h2>
                            <div className="button-row">
                                <button disabled={busy} onClick={applyFilters}>{t("filters.refreshEntries")}</button>
                                <button className="accent" disabled={busy}
                                        onClick={scanArcs}>{t("filters.scanNewArc")}</button>
                            </div>
                        </div>
                        <div className="filters-grid">
                            <label>
                                <span>{t("filters.search")}</span>
                                <input value={query.search}
                                       onChange={(event) => updateQueryField("search", event.target.value)}/>
                            </label>
                            <label>
                                <span>{t("filters.arc")}</span>
                                <select value={query.sourceArc}
                                        onChange={(event) => updateQueryField("sourceArc", event.target.value)}>
                                    <option value="">{t("common.all")}</option>
                                    {filters.arcs.map((value) => (
                                        <option key={value} value={value}>{value}</option>
                                    ))}
                                </select>
                            </label>
                            <label>
                                <span>{t("filters.sourceFile")}</span>
                                <input
                                    value={query.sourceFile}
                                    placeholder={t("filters.sourceFileHint")}
                                    onChange={(event) => updateQueryField("sourceFile", event.target.value)}
                                />
                            </label>
                            <label>
                                <span>{t("filters.type")}</span>
                                <select value={query.type}
                                        onChange={(event) => updateQueryField("type", event.target.value)}>
                                    <option value="">{t("common.all")}</option>
                                    {filters.types.map((value) => (
                                        <option key={value} value={value}>{value}</option>
                                    ))}
                                </select>
                            </label>
                            <label>
                                <span>{t("filters.status")}</span>
                                <select value={query.status}
                                        onChange={(event) => updateQueryField("status", event.target.value)}>
                                    <option value="">{t("common.all")}</option>
                                    {statusOptions.map((value) => (
                                        <option key={value} value={value}>{translateStatus(value, t)}</option>
                                    ))}
                                </select>
                            </label>
                            <label className="checkline">
                                <input
                                    type="checkbox"
                                    checked={query.untranslatedOnly}
                                    onChange={(event) => updateQueryField("untranslatedOnly", event.target.checked)}
                                />
                                <span>{t("filters.onlyUntranslated")}</span>
                            </label>
                        </div>
                    </section>

                    <section className="panel compact-panel translation-panel-top" hidden>
                        <div className="panel-title-row">
                            <h2>{t("translationSection.title")}</h2>
                            <button
                                className="accent"
                                disabled={busy || !isAutomaticTranslator(currentTranslator)}
                                onClick={runTranslation}
                            >
                                {t("translationSection.run")}
                            </button>
                        </div>
                        <div className="translate-grid">
                            <label>
                                <span>{t("translationSection.provider")}</span>
                                <select
                                    value={currentTranslator}
                                    onChange={(event) => updateTranslationField("activeTranslator", event.target.value)}
                                >
                                    {translators.map((name) => (
                                        <option key={name} value={name}>{translateTranslatorLabel(name, t)}</option>
                                    ))}
                                </select>
                            </label>
                            <label>
                                <span>{t("translationSection.targetField")}</span>
                                <select value={translationTargetField}
                                        onChange={(event) => setTranslationTargetField(event.target.value)}>
                                    <option value="translated">{translateTargetFieldLabel("translated", t)}</option>
                                    <option value="polished">{translateTargetFieldLabel("polished", t)}</option>
                                </select>
                            </label>
                            <label>
                                <span>{t("translationSection.languagePair")}</span>
                                <div className="readonly-chip">
                                    {settings.translation.sourceLanguage || "auto"} → {settings.translation.targetLanguage || "auto"}
                                </div>
                            </label>
                        </div>
                        <label className="checkline">
                            <input
                                type="checkbox"
                                checked={translationAllowOverwrite}
                                onChange={(event) => setTranslationAllowOverwrite(event.target.checked)}
                            />
                            <span>{t("translationSection.allowOverwrite")}</span>
                        </label>
                        <p className="help">
                            {isAutomaticTranslator(currentTranslator)
                                ? t("translationSection.help", {provider: translateTranslatorLabel(currentTranslator, t)})
                                : t("translationSection.manualHelp")}
                        </p>
                        <p className="help">{translateTranslatorHelp(currentTranslator, t)}</p>
                        {batchTranslationProgress && (
                            <div className="import-progress-card">
                                <div className="import-progress-header">
                                    <span
                                        className={`phase-pill ${batchTranslationProgress.phase}`}>{formatTranslatePhase(batchTranslationProgress.phase, t)}</span>
                                    <span
                                        className="import-progress-importer">{translateTranslatorLabel(batchTranslationProgress.translator, t)}</span>
                                </div>
                                <div className="import-progress-file">
                                    {batchTranslationProgress.currentItem || t("translateProgress.waitingItem")}
                                </div>
                                <div className="import-progress-grid translate-progress-grid">
                                    <div>
                                        <strong>{batchTranslationProgress.total}</strong>
                                        <span>{t("translateProgress.total")}</span>
                                    </div>
                                    <div>
                                        <strong>{batchTranslationProgress.processed}</strong>
                                        <span>{t("translateProgress.processed")}</span>
                                    </div>
                                    <div>
                                        <strong>{batchTranslationProgress.updated}</strong>
                                        <span>{t("translateProgress.updated")}</span>
                                    </div>
                                    <div>
                                        <strong>{batchTranslationProgress.skipped}</strong>
                                        <span>{t("translateProgress.skipped")}</span>
                                    </div>
                                    <div>
                                        <strong>{batchTranslationProgress.failed}</strong>
                                        <span>{t("translateProgress.failed")}</span>
                                    </div>
                                    <div>
                                        <strong>{translateTargetFieldLabel(batchTranslationProgress.targetField, t)}</strong>
                                        <span>{t("translateProgress.targetField")}</span>
                                    </div>
                                </div>
                            </div>
                        )}
                    </section>

                    <section className="workspace">
                        <div className="panel entry-panel">
                            <div className="panel-title-row">
                                <h2>{t("entryTable.title")}</h2>
                                <div className="button-row">
                                    <span className="pill">{entryTotal}</span>
                                </div>
                            </div>
                            <div className="entry-batch-row">
                                <p className="help entry-batch-help">
                                    {t("entryTable.autoSaveHelp")}
                                    {" "}
                                    {t("entryTable.batchHelp")}
                                </p>
                                <div className="entry-batch-controls">
                                    <select value={batchStatus}
                                            onChange={(event) => setBatchStatus(event.target.value)}>
                                        {statusOptions.map((value) => (
                                            <option key={value} value={value}>{translateStatus(value, t)}</option>
                                        ))}
                                    </select>
                                    <button disabled={busy || !canRunFilteredBatchActions}
                                            onClick={applyFilteredStatusUpdate}>
                                        {t("entryTable.applyFilteredStatus")}
                                    </button>
                                    <button
                                        className="danger-button"
                                        disabled={busy || !canRunFilteredBatchActions}
                                        onClick={clearFilteredTranslations}
                                    >
                                        {t("entryTable.clearFilteredTranslations")}
                                    </button>
                                </div>
                            </div>
                            <div className="list-frame">
                                <table className="grid-table">
                                    <thead>
                                    <tr>
                                        <th>{t("entryTable.context")}</th>
                                        <th>{t("entryTable.original")}</th>
                                        <th>{t("entryTable.translated")}</th>
                                        <th>{t("entryTable.polished")}</th>
                                        <th>{t("entryTable.status")}</th>
                                    </tr>
                                    </thead>
                                    <tbody>
                                    {entries.map((entry) => (
                                        <tr key={entry.id}>
                                            <td className="context-cell">
                                                <div className="cell-primary">{entry.sourceArc}</div>
                                                <div className="cell-secondary">{entry.sourceFile}</div>
                                                <div className="cell-secondary">
                                                    {entry.type}
                                                    {entry.role ? ` / ${entry.role}` : ""}
                                                    {entry.voiceId ? ` / ${entry.voiceId}` : ""}
                                                </div>
                                            </td>
                                            <td className="text-cell">{entry.sourceText}</td>
                                            <td>
                          <textarea
                              value={entry.translatedText}
                              onChange={(event) => updateEntryField(entry.id, "translatedText", event.target.value)}
                          />
                                            </td>
                                            <td>
                          <textarea
                              value={entry.polishedText}
                              onChange={(event) => updateEntryField(entry.id, "polishedText", event.target.value)}
                          />
                                            </td>
                                            <td>
                                                <select
                                                    value={entry.translatorStatus}
                                                    onChange={(event) => updateEntryField(entry.id, "translatorStatus", event.target.value)}
                                                >
                                                    {statusOptions.map((value) => (
                                                        <option key={value}
                                                                value={value}>{translateStatus(value, t)}</option>
                                                    ))}
                                                </select>
                                            </td>
                                        </tr>
                                    ))}
                                    </tbody>
                                </table>
                            </div>
                            <div className="entry-footer">
                                <div className="entry-toolbar">
                                    <span className="page-chip">{t("entryTable.pageInfo", {
                                        page: currentPage,
                                        totalPages
                                    })}</span>
                                    <span className="page-chip">{t("entryTable.rangeInfo", {
                                        from: pageStart,
                                        to: pageEnd,
                                        total: entryTotal
                                    })}</span>
                                    <button disabled={busy || currentPage <= 1}
                                            onClick={() => changeEntryPage(currentPage - 1)}>
                                        {t("entryTable.previousPage")}
                                    </button>
                                    <button disabled={busy || currentPage >= totalPages || entryTotal === 0}
                                            onClick={() => changeEntryPage(currentPage + 1)}>
                                        {t("entryTable.nextPage")}
                                    </button>
                                </div>
                            </div>
                        </div>

                        <div className="panel arc-panel">
                            <div className="panel-title-row">
                                <h2>{t("arcTable.title")}</h2>
                                <div className="button-row">
                                    <span className="pill">{arcs.length}</span>
                                    <button disabled={busy || arcs.length === 0} onClick={reparseAllArcs}>
                                        {t("arcTable.reparseAll", {count: arcs.length})}
                                    </button>
                                    <button disabled={busy || failedArcCount === 0} onClick={reparseFailedArcs}>
                                        {t("arcTable.reparseFailed", {count: failedArcCount})}
                                    </button>
                                </div>
                            </div>
                            <div className="list-frame">
                                <table className="grid-table compact">
                                    <thead>
                                    <tr>
                                        <th>{t("arcTable.filename")}</th>
                                        <th>{t("arcTable.status")}</th>
                                        <th>{t("arcTable.parsedAt")}</th>
                                        <th>{t("arcTable.action")}</th>
                                    </tr>
                                    </thead>
                                    <tbody>
                                    {arcs.map((arc) => (
                                        <tr key={arc.id}>
                                            <td>
                                                <div className="cell-primary">{arc.filename}</div>
                                                <div className="cell-secondary">{arc.path}</div>
                                                {arc.lastError && <div className="cell-error">{arc.lastError}</div>}
                                            </td>
                                            <td>{translateArcStatus(arc.status, t)}</td>
                                            <td>{arc.parsedAt || "-"}</td>
                                            <td>
                                                <button disabled={busy}
                                                        onClick={() => reparseArc(arc.id)}>{t("arcTable.reparse")}</button>
                                            </td>
                                        </tr>
                                    ))}
                                    </tbody>
                                </table>
                            </div>
                        </div>

                        <div className="panel translation-panel">
                            <div className="panel-title-row">
                                <h2>{t("translationSection.title")}</h2>
                                <div className="button-row">
                                    <button
                                        className="accent"
                                        disabled={busy || !isAutomaticTranslator(currentTranslator)}
                                        onClick={runTranslation}
                                    >
                                        {t("translationSection.run")}
                                    </button>
                                    {activeTask === "translation" && (
                                        <button
                                            className="danger-button"
                                            disabled={stopRequested}
                                            onClick={stopCurrentTask}
                                        >
                                            {stopRequested ? t("common.stopping") : t("common.stop")}
                                        </button>
                                    )}
                                </div>
                            </div>
                            <div className="translate-grid">
                                <label>
                                    <span>{t("translationSection.provider")}</span>
                                    <select
                                        value={currentTranslator}
                                        onChange={(event) => updateTranslationField("activeTranslator", event.target.value)}
                                    >
                                        {translators.map((name) => (
                                            <option key={name} value={name}>{translateTranslatorLabel(name, t)}</option>
                                        ))}
                                    </select>
                                </label>
                                <label>
                                    <span>{t("translationSection.targetField")}</span>
                                    <select value={translationTargetField}
                                            onChange={(event) => setTranslationTargetField(event.target.value)}>
                                        <option value="translated">{translateTargetFieldLabel("translated", t)}</option>
                                        <option value="polished">{translateTargetFieldLabel("polished", t)}</option>
                                    </select>
                                </label>
                                <label>
                                    <span>{t("translationSection.languagePair")}</span>
                                    <div className="readonly-chip">
                                        {settings.translation.sourceLanguage || "auto"} 鈫?{settings.translation.targetLanguage || "auto"}
                                    </div>
                                </label>
                            </div>
                            <label className="checkline">
                                <input
                                    type="checkbox"
                                    checked={translationAllowOverwrite}
                                    onChange={(event) => setTranslationAllowOverwrite(event.target.checked)}
                                />
                                <span>{t("translationSection.allowOverwrite")}</span>
                            </label>
                            <p className="help">
                                {isAutomaticTranslator(currentTranslator)
                                    ? t("translationSection.help", {provider: translateTranslatorLabel(currentTranslator, t)})
                                    : t("translationSection.manualHelp")}
                            </p>
                            <p className="help">{translateTranslatorHelp(currentTranslator, t)}</p>
                            {batchTranslationProgress && (
                                <div className="import-progress-card">
                                    <div className="import-progress-header">
                                        <span
                                            className={`phase-pill ${batchTranslationProgress.phase}`}>{formatTranslatePhase(batchTranslationProgress.phase, t)}</span>
                                        <span
                                            className="import-progress-importer">{translateTranslatorLabel(batchTranslationProgress.translator, t)}</span>
                                    </div>
                                    <div className="import-progress-file">
                                        {batchTranslationProgress.currentItem || t("translateProgress.waitingItem")}
                                    </div>
                                    <div className="import-progress-grid translate-progress-grid">
                                        <div>
                                            <strong>{batchTranslationProgress.total}</strong>
                                            <span>{t("translateProgress.total")}</span>
                                        </div>
                                        <div>
                                            <strong>{batchTranslationProgress.processed}</strong>
                                            <span>{t("translateProgress.processed")}</span>
                                        </div>
                                        <div>
                                            <strong>{batchTranslationProgress.updated}</strong>
                                            <span>{t("translateProgress.updated")}</span>
                                        </div>
                                        <div>
                                            <strong>{batchTranslationProgress.skipped}</strong>
                                            <span>{t("translateProgress.skipped")}</span>
                                        </div>
                                        <div>
                                            <strong>{batchTranslationProgress.failed}</strong>
                                            <span>{t("translateProgress.failed")}</span>
                                        </div>
                                        <div>
                                            <strong>{translateTargetFieldLabel(batchTranslationProgress.targetField, t)}</strong>
                                            <span>{t("translateProgress.targetField")}</span>
                                        </div>
                                    </div>
                                </div>
                            )}
                        </div>

                        <div className="panel translation-panel">
                            <div className="panel-title-row">
                                <h2>{t("sourceRecognitionSection.title")}</h2>
                                <div className="button-row">
                                    <button
                                        className="accent"
                                        disabled={busy}
                                        onClick={runSourceRecognition}
                                    >
                                        {t("sourceRecognitionSection.run")}
                                    </button>
                                    {activeTask === "sourceRecognition" && (
                                        <button
                                            className="danger-button"
                                            disabled={stopRequested}
                                            onClick={stopCurrentTask}
                                        >
                                            {stopRequested ? t("common.stopping") : t("common.stop")}
                                        </button>
                                    )}
                                </div>
                            </div>
                            <div className="translate-grid">
                                <label>
                                    <span>{t("sourceRecognitionSection.provider")}</span>
                                    <div className="readonly-chip">{translateTranslatorLabel(asrTranslatorName, t)}</div>
                                </label>
                                <label>
                                    <span>{t("sourceRecognitionSection.targetField")}</span>
                                    <div className="readonly-chip">{translateTargetFieldLabel("source_text", t)}</div>
                                </label>
                                <label>
                                    <span>{t("sourceRecognitionSection.language")}</span>
                                    <div className="readonly-chip">{settings.translation.asr.language || "-"}</div>
                                </label>
                            </div>
                            <label className="checkline">
                                <input
                                    type="checkbox"
                                    checked={sourceRecognitionAllowOverwrite}
                                    onChange={(event) => setSourceRecognitionAllowOverwrite(event.target.checked)}
                                />
                                <span>{t("sourceRecognitionSection.allowOverwrite")}</span>
                            </label>
                            <p className="help">{t("sourceRecognitionSection.help")}</p>
                            {sourceRecognitionProgress && (
                                <div className="import-progress-card">
                                    <div className="import-progress-header">
                                        <span
                                            className={`phase-pill ${sourceRecognitionProgress.phase}`}>{formatTranslatePhase(sourceRecognitionProgress.phase, t)}</span>
                                        <span
                                            className="import-progress-importer">{translateTranslatorLabel(sourceRecognitionProgress.translator, t)}</span>
                                    </div>
                                    <div className="import-progress-file">
                                        {sourceRecognitionProgress.currentItem || t("translateProgress.waitingItem")}
                                    </div>
                                    <div className="import-progress-grid translate-progress-grid">
                                        <div>
                                            <strong>{sourceRecognitionProgress.total}</strong>
                                            <span>{t("translateProgress.total")}</span>
                                        </div>
                                        <div>
                                            <strong>{sourceRecognitionProgress.processed}</strong>
                                            <span>{t("translateProgress.processed")}</span>
                                        </div>
                                        <div>
                                            <strong>{sourceRecognitionProgress.updated}</strong>
                                            <span>{t("translateProgress.updated")}</span>
                                        </div>
                                        <div>
                                            <strong>{sourceRecognitionProgress.skipped}</strong>
                                            <span>{t("translateProgress.skipped")}</span>
                                        </div>
                                        <div>
                                            <strong>{sourceRecognitionProgress.failed}</strong>
                                            <span>{t("translateProgress.failed")}</span>
                                        </div>
                                        <div>
                                            <strong>{translateTargetFieldLabel(sourceRecognitionProgress.targetField, t)}</strong>
                                            <span>{t("translateProgress.targetField")}</span>
                                        </div>
                                    </div>
                                </div>
                            )}
                        </div>
                    </section>
                </>
            ) : (
                <section className="tools-layout">
                    <div className="main-stack">
                        <div className="panel">
                            <div className="panel-title-row">
                                <h2>{t("settings.title")}</h2>
                                <span className="panel-meta">{t("settings.autoSaveHelp")}</span>
                            </div>
                            <div className="path-list">
                                <div className="path-row">
                                    <div className="path-meta">
                                        <span className="path-label">{t("settings.arcScanDirectory")}</span>
                                        <div
                                            className="path-value">{displayPath(settings.arcScanDir, t("settings.noArcScanDirectory"))}</div>
                                    </div>
                                    <button disabled={busy}
                                            onClick={() => chooseSettingsDirectory("arcScanDir", t("settings.selectArcScanDirectory"))}>{t("common.choose")}</button>
                                </div>
                                <div className="path-row">
                                    <div className="path-meta">
                                        <span className="path-label">{t("settings.workDirectory")}</span>
                                        <div
                                            className="path-value">{displayPath(settings.workDir, t("settings.noWorkDirectory"))}</div>
                                    </div>
                                    <button disabled={busy}
                                            onClick={() => chooseSettingsDirectory("workDir", t("settings.selectWorkDirectory"))}>{t("common.choose")}</button>
                                </div>
                                <div className="path-row">
                                    <div className="path-meta">
                                        <span className="path-label">{t("settings.defaultImportDirectory")}</span>
                                        <div
                                            className="path-value">{displayPath(settings.importDir, t("settings.noDefaultImportDirectory"))}</div>
                                    </div>
                                    <button disabled={busy}
                                            onClick={() => chooseSettingsDirectory("importDir", t("settings.selectDefaultImportDirectory"))}>{t("common.choose")}</button>
                                </div>
                                <div className="path-row">
                                    <div className="path-meta">
                                        <span className="path-label">{t("settings.defaultExportDirectory")}</span>
                                        <div
                                            className="path-value">{displayPath(settings.exportDir, t("settings.noDefaultExportDirectory"))}</div>
                                    </div>
                                    <button disabled={busy}
                                            onClick={() => chooseSettingsDirectory("exportDir", t("settings.selectDefaultExportDirectory"))}>{t("common.choose")}</button>
                                </div>
                            </div>
                        </div>

                        <div className="panel">
                            <div className="panel-title-row">
                                <h2>{t("translationSettings.title")}</h2>
                                <span className="panel-meta">{t("settings.autoSaveHelp")}</span>
                            </div>
                            <div className="config-grid">
                                <label>
                                    <span>{t("translationSettings.activeProvider")}</span>
                                    <select
                                        value={currentTranslator}
                                        onChange={(event) => updateTranslationField("activeTranslator", event.target.value)}
                                    >
                                        {translators.map((name) => (
                                            <option key={name} value={name}>{translateTranslatorLabel(name, t)}</option>
                                        ))}
                                    </select>
                                </label>
                                <label>
                                    <span>{t("translationSettings.sourceLanguage")}</span>
                                    <input
                                        value={settings.translation.sourceLanguage}
                                        onChange={(event) => updateTranslationField("sourceLanguage", event.target.value)}
                                    />
                                </label>
                                <label>
                                    <span>{t("translationSettings.targetLanguage")}</span>
                                    <input
                                        value={settings.translation.targetLanguage}
                                        onChange={(event) => updateTranslationField("targetLanguage", event.target.value)}
                                    />
                                </label>
                                <label>
                                    <span>{t("translationSettings.proxy.mode")}</span>
                                    <select
                                        value={settings.translation.proxy.mode}
                                        onChange={(event) => updateTranslationField("proxy", {
                                            ...settings.translation.proxy,
                                            mode: event.target.value,
                                        })}
                                    >
                                        <option value="system">{t("translationSettings.proxy.system")}</option>
                                        <option value="direct">{t("translationSettings.proxy.direct")}</option>
                                        <option value="custom">{t("translationSettings.proxy.custom")}</option>
                                    </select>
                                </label>
                                {settings.translation.proxy.mode === "custom" && (
                                    <label className="wide-field">
                                        <span>{t("translationSettings.proxy.url")}</span>
                                        <input
                                            value={settings.translation.proxy.url}
                                            placeholder="http://127.0.0.1:7890"
                                            onChange={(event) => updateTranslationField("proxy", {
                                                ...settings.translation.proxy,
                                                url: event.target.value,
                                            })}
                                        />
                                    </label>
                                )}
                                <div className="wide-field proxy-actions">
                                    <button type="button" disabled={busy || testingProxy} onClick={testProxy}>
                                        {testingProxy
                                            ? t("translationSettings.proxy.testing")
                                            : t("translationSettings.proxy.test")}
                                    </button>
                                </div>
                                <div className="wide-field glossary-field">
                                    <span>{t("translationSettings.glossary")}</span>
                                    <div className="glossary-editor">
                                        <div className="glossary-grid glossary-grid-header">
                                            <strong>{t("translationSettings.glossarySource")}</strong>
                                            <strong>{t("translationSettings.glossaryPreferred")}</strong>
                                            <strong>{t("translationSettings.glossaryNote")}</strong>
                                            <span />
                                        </div>
                                        {glossaryRows.map((row) => (
                                            <div key={row.id} className="glossary-grid">
                                                <input
                                                    value={row.source}
                                                    onChange={(event) => updateGlossaryRow(row.id, "source", event.target.value)}
                                                />
                                                <input
                                                    value={row.preferred}
                                                    onChange={(event) => updateGlossaryRow(row.id, "preferred", event.target.value)}
                                                />
                                                <input
                                                    value={row.note}
                                                    onChange={(event) => updateGlossaryRow(row.id, "note", event.target.value)}
                                                />
                                                <button type="button" disabled={busy || glossaryRows.length <= 1}
                                                        onClick={() => removeGlossaryRow(row.id)}>
                                                    {t("translationSettings.removeGlossaryRow")}
                                                </button>
                                            </div>
                                        ))}
                                        <div className="glossary-actions">
                                            <button type="button" disabled={busy} onClick={importGlossary}>
                                                {t("translationSettings.importGlossary")}
                                            </button>
                                            <button type="button" disabled={busy} onClick={exportGlossary}>
                                                {t("translationSettings.exportGlossary")}
                                            </button>
                                            <button type="button" disabled={busy} onClick={addGlossaryRow}>
                                                {t("translationSettings.addGlossaryRow")}
                                            </button>
                                        </div>
                                    </div>
                                </div>
                            </div>
                            <p className="help">{t("translationSettings.proxy.help")}</p>
                            <p className="help">{t("translationSettings.glossaryHelp")}</p>
                            <p className="help">{translateTranslatorHelp(currentTranslator, t)}</p>

                            {currentTranslator === "google-translate" && (
                                <div className="config-grid">
                                    <label className="wide-field">
                                        <span>{t("translationSettings.google.baseUrl")}</span>
                                        <input
                                            value={settings.translation.google.baseUrl}
                                            onChange={(event) => updateGoogleField("baseUrl", event.target.value)}
                                        />
                                    </label>
                                    <label>
                                        <span>{t("translationSettings.google.apiKey")}</span>
                                        <input
                                            type="password"
                                            value={settings.translation.google.apiKey}
                                            onChange={(event) => updateGoogleField("apiKey", event.target.value)}
                                        />
                                    </label>
                                    <label>
                                        <span>{t("translationSettings.google.format")}</span>
                                        <select
                                            value={settings.translation.google.format}
                                            onChange={(event) => updateGoogleField("format", event.target.value)}
                                        >
                                            <option value="text">text</option>
                                            <option value="html">html</option>
                                        </select>
                                    </label>
                                    <label>
                                        <span>{t("translationSettings.google.model")}</span>
                                        <input
                                            value={settings.translation.google.model}
                                            onChange={(event) => updateGoogleField("model", event.target.value)}
                                        />
                                    </label>
                                    <label>
                                        <span>{t("translationSettings.google.batchSize")}</span>
                                        <input
                                            type="number"
                                            min={1}
                                            value={settings.translation.google.batchSize}
                                            onChange={(event) => updateGoogleField("batchSize", parseInteger(event.target.value, 32))}
                                        />
                                    </label>
                                    <label>
                                        <span>{t("translationSettings.google.timeoutSeconds")}</span>
                                        <input
                                            type="number"
                                            min={1}
                                            value={settings.translation.google.timeoutSeconds}
                                            onChange={(event) => updateGoogleField("timeoutSeconds", parseInteger(event.target.value, 60))}
                                        />
                                    </label>
                                </div>
                            )}

                            {currentTranslator === "baidu-translate" && (
                                <div className="config-grid">
                                    <label className="wide-field">
                                        <span>{t("translationSettings.baidu.baseUrl")}</span>
                                        <input
                                            value={settings.translation.baidu.baseUrl}
                                            onChange={(event) => updateBaiduField("baseUrl", event.target.value)}
                                        />
                                    </label>
                                    <label>
                                        <span>{t("translationSettings.baidu.appId")}</span>
                                        <input
                                            value={settings.translation.baidu.appId}
                                            onChange={(event) => updateBaiduField("appId", event.target.value)}
                                        />
                                    </label>
                                    <label>
                                        <span>{t("translationSettings.baidu.secret")}</span>
                                        <input
                                            type="password"
                                            value={settings.translation.baidu.secret}
                                            onChange={(event) => updateBaiduField("secret", event.target.value)}
                                        />
                                    </label>
                                    <label>
                                        <span>{t("translationSettings.baidu.timeoutSeconds")}</span>
                                        <input
                                            type="number"
                                            min={1}
                                            value={settings.translation.baidu.timeoutSeconds}
                                            onChange={(event) => updateBaiduField("timeoutSeconds", parseInteger(event.target.value, 60))}
                                        />
                                    </label>
                                </div>
                            )}

                            {currentOpenAISettingsKey && (
                                <div className="config-grid">
                                    <label className="wide-field">
                                        <span>{t("translationSettings.openai.baseUrl")}</span>
                                        <input
                                            value={settings.translation[currentOpenAISettingsKey].baseUrl}
                                            onChange={(event) => updateOpenAIField(currentOpenAISettingsKey, "baseUrl", event.target.value)}
                                        />
                                    </label>
                                    <label>
                                        <span>{t("translationSettings.openai.apiKey")}</span>
                                        <input
                                            type="password"
                                            value={settings.translation[currentOpenAISettingsKey].apiKey}
                                            onChange={(event) => updateOpenAIField(currentOpenAISettingsKey, "apiKey", event.target.value)}
                                        />
                                    </label>
                                    <label>
                                        <span>{t("translationSettings.openai.model")}</span>
                                        <input
                                            value={settings.translation[currentOpenAISettingsKey].model}
                                            onChange={(event) => updateOpenAIField(currentOpenAISettingsKey, "model", event.target.value)}
                                        />
                                    </label>
                                    <label>
                                        <span>{t("translationSettings.openai.batchSize")}</span>
                                        <input
                                            type="number"
                                            min={1}
                                            value={settings.translation[currentOpenAISettingsKey].batchSize}
                                            onChange={(event) => updateOpenAIField(currentOpenAISettingsKey, "batchSize", parseInteger(event.target.value, 32))}
                                        />
                                    </label>
                                    <label>
                                        <span>{t("translationSettings.openai.concurrency")}</span>
                                        <input
                                            type="number"
                                            min={1}
                                            value={settings.translation[currentOpenAISettingsKey].concurrency}
                                            onChange={(event) => updateOpenAIField(currentOpenAISettingsKey, "concurrency", parseInteger(event.target.value, 1))}
                                        />
                                    </label>
                                    <label>
                                        <span>{t("translationSettings.openai.timeoutSeconds")}</span>
                                        <input
                                            type="number"
                                            min={1}
                                            value={settings.translation[currentOpenAISettingsKey].timeoutSeconds}
                                            onChange={(event) => updateOpenAIField(currentOpenAISettingsKey, "timeoutSeconds", parseInteger(event.target.value, 120))}
                                        />
                                    </label>
                                    <label>
                                        <span>{t("translationSettings.openai.reasoningEffort")}</span>
                                        <input
                                            value={settings.translation[currentOpenAISettingsKey].reasoningEffort}
                                            onChange={(event) => updateOpenAIField(currentOpenAISettingsKey, "reasoningEffort", event.target.value)}
                                        />
                                    </label>
                                    <label>
                                        <span>{t("translationSettings.openai.temperature")}</span>
                                        <input
                                            value={optionalNumberToInput(settings.translation[currentOpenAISettingsKey].temperature)}
                                            onChange={(event) => updateOpenAIField(currentOpenAISettingsKey, "temperature", parseOptionalNumber(event.target.value))}
                                        />
                                    </label>
                                    <label>
                                        <span>{t("translationSettings.openai.topP")}</span>
                                        <input
                                            value={optionalNumberToInput(settings.translation[currentOpenAISettingsKey].topP)}
                                            onChange={(event) => updateOpenAIField(currentOpenAISettingsKey, "topP", parseOptionalNumber(event.target.value))}
                                        />
                                    </label>
                                    <label>
                                        <span>{t("translationSettings.openai.presencePenalty")}</span>
                                        <input
                                            value={optionalNumberToInput(settings.translation[currentOpenAISettingsKey].presencePenalty)}
                                            onChange={(event) => updateOpenAIField(currentOpenAISettingsKey, "presencePenalty", parseOptionalNumber(event.target.value))}
                                        />
                                    </label>
                                    <label>
                                        <span>{t("translationSettings.openai.frequencyPenalty")}</span>
                                        <input
                                            value={optionalNumberToInput(settings.translation[currentOpenAISettingsKey].frequencyPenalty)}
                                            onChange={(event) => updateOpenAIField(currentOpenAISettingsKey, "frequencyPenalty", parseOptionalNumber(event.target.value))}
                                        />
                                    </label>
                                    <label>
                                        <span>{t("translationSettings.openai.maxOutputTokens")}</span>
                                        <input
                                            value={optionalNumberToInput(settings.translation[currentOpenAISettingsKey].maxOutputTokens)}
                                            onChange={(event) => updateOpenAIField(currentOpenAISettingsKey, "maxOutputTokens", parseOptionalNumber(event.target.value))}
                                        />
                                    </label>
                                    <label className="wide-field">
                                        <span>{t("translationSettings.openai.prompt")}</span>
                                        <textarea
                                            value={settings.translation[currentOpenAISettingsKey].prompt}
                                            onChange={(event) => updateOpenAIField(currentOpenAISettingsKey, "prompt", event.target.value)}
                                        />
                                    </label>
                                    <label className="wide-field">
                                        <span>{t("translationSettings.openai.extraJson")}</span>
                                        <textarea
                                            value={settings.translation[currentOpenAISettingsKey].extraJson}
                                            onChange={(event) => updateOpenAIField(currentOpenAISettingsKey, "extraJson", event.target.value)}
                                        />
                                    </label>
                                    <p className="help wide-field">{t("translationSettings.openai.promptHelp")}</p>
                                    <div
                                        className="path-value wide-field">{t("translationSettings.openai.placeholders")}</div>
                                    <p className="help wide-field">{t("translationSettings.openai.extraJsonHelp")}</p>
                                </div>
                            )}

                            <div className="config-section">
                                <h3 className="config-section-title">{t("translationSettings.asr.title")}</h3>
                                <div className="config-grid">
                                    <label className="wide-field">
                                        <span>{t("translationSettings.asr.baseUrl")}</span>
                                        <input
                                            value={settings.translation.asr.baseUrl}
                                            onChange={(event) => updateASRField("baseUrl", event.target.value)}
                                        />
                                    </label>
                                    <label>
                                        <span>{t("translationSettings.asr.language")}</span>
                                        <input
                                            value={settings.translation.asr.language}
                                            onChange={(event) => updateASRField("language", event.target.value)}
                                        />
                                    </label>
                                    <label>
                                        <span>{t("translationSettings.asr.batchSize")}</span>
                                        <input
                                            type="number"
                                            min={1}
                                            value={settings.translation.asr.batchSize}
                                            onChange={(event) => updateASRField("batchSize", parseInteger(event.target.value, 4))}
                                        />
                                    </label>
                                    <label>
                                        <span>{t("translationSettings.asr.concurrency")}</span>
                                        <input
                                            type="number"
                                            min={1}
                                            value={settings.translation.asr.concurrency}
                                            onChange={(event) => updateASRField("concurrency", parseInteger(event.target.value, 1))}
                                        />
                                    </label>
                                    <label>
                                        <span>{t("translationSettings.asr.timeoutSeconds")}</span>
                                        <input
                                            type="number"
                                            min={1}
                                            value={settings.translation.asr.timeoutSeconds}
                                            onChange={(event) => updateASRField("timeoutSeconds", parseInteger(event.target.value, 600))}
                                        />
                                    </label>
                                    <label className="wide-field">
                                        <span>{t("translationSettings.asr.prompt")}</span>
                                        <textarea
                                            value={settings.translation.asr.prompt}
                                            onChange={(event) => updateASRField("prompt", event.target.value)}
                                        />
                                    </label>
                                </div>
                                <p className="help">{t("translationSettings.asr.help")}</p>
                                <div className="translator-actions">
                                    <button type="button" disabled={busy || testingASR} onClick={testASR}>
                                        {testingASR
                                            ? t("translationSettings.asr.testing")
                                            : t("translationSettings.asr.test")}
                                    </button>
                                </div>
                            </div>

                            {currentTranslator === "manual" &&
                                <p className="help">{t("translationSettings.manualHelp")}</p>}
                            {isAutomaticTranslator(currentTranslator) && (
                                <div className="translator-actions">
                                    <button type="button" disabled={busy || testingTranslator} onClick={testCurrentTranslator}>
                                        {testingTranslator
                                            ? t("translationSettings.testTranslatorRunning")
                                            : t("translationSettings.testTranslator")}
                                    </button>
                                </div>
                            )}
                        </div>
                    </div>

                    <div className="side-stack">
                        <div className="panel compact-panel">
                            <div className="panel-title-row">
                                <h2>{t("importSection.title")}</h2>
                                <button
                                    className="accent"
                                    disabled={busy || currentImportPath.trim() === ""}
                                    onClick={runImport}
                                >
                                    {t("importSection.run")}
                                </button>
                            </div>
                            <label>
                                <span>{t("importSection.importer")}</span>
                                <select
                                    value={selectedImporter}
                                    onChange={(event) => setSelectedImporter(event.target.value)}
                                >
                                    {importers.map((name) => (
                                        <option key={name} value={name}>{translateImporterLabel(name, t)}</option>
                                    ))}
                                </select>
                            </label>
                            <div className="path-row no-border">
                                <div className="path-meta">
                  <span className="path-label">
                    {importerUsesFileSource(selectedImporter) ? t("importSection.sourceFile") : t("importSection.sourceDirectory")}
                  </span>
                                    <div className="path-value">
                                        {displayPath(
                                            currentImportPath,
                                            importerUsesFileSource(selectedImporter) ? t("importSection.noImportFile") : t("importSection.noImportDirectory"),
                                        )}
                                    </div>
                                </div>
                                <button disabled={busy} onClick={chooseImportSource}>
                                    {importerUsesFileSource(selectedImporter) ? t("common.chooseFile") : t("common.chooseFolder")}
                                </button>
                            </div>
                            <label className="checkline">
                                <input type="checkbox" checked={allowOverwrite}
                                       onChange={(event) => setAllowOverwrite(event.target.checked)}/>
                                <span>{t("importSection.allowOverwrite")}</span>
                            </label>
                            <p className="help">{translateImporterHelp(selectedImporter, t)}</p>
                            {importProgress && (
                                <div className="import-progress-card">
                                    <div className="import-progress-header">
                                        <span
                                            className={`phase-pill ${importProgress.phase}`}>{formatImportPhase(importProgress.phase, t)}</span>
                                        <span
                                            className="import-progress-importer">{translateImporterLabel(importProgress.importer, t)}</span>
                                    </div>
                                    <div className="import-progress-file">
                                        {displayPath(importProgress.currentFile, t("importProgress.waitingFile"))}
                                    </div>
                                    <div className="import-progress-grid">
                                        <div>
                                            <strong>{importProgress.filesProcessed}</strong>
                                            <span>{t("importProgress.files")}</span>
                                        </div>
                                        <div>
                                            <strong>{importProgress.totalLines}</strong>
                                            <span>{t("importProgress.lines")}</span>
                                        </div>
                                        <div>
                                            <strong>{importProgress.inserted}</strong>
                                            <span>{t("importProgress.inserted")}</span>
                                        </div>
                                        <div>
                                            <strong>{importProgress.updated}</strong>
                                            <span>{t("importProgress.updated")}</span>
                                        </div>
                                        <div>
                                            <strong>{importProgress.skipped}</strong>
                                            <span>{t("importProgress.skipped")}</span>
                                        </div>
                                        <div>
                                            <strong>{importProgress.unmatched}</strong>
                                            <span>{t("importProgress.unmatched")}</span>
                                        </div>
                                        <div>
                                            <strong>{importProgress.errorLines}</strong>
                                            <span>{t("importProgress.errors")}</span>
                                        </div>
                                    </div>
                                </div>
                            )}
                        </div>

                        <div className="panel compact-panel">
                            <div className="panel-title-row">
                                <h2>{t("exportSection.title")}</h2>
                                <button
                                    className="accent"
                                    disabled={busy || exportPath.trim() === ""}
                                    onClick={runExport}
                                >
                                    {t("exportSection.run")}
                                </button>
                            </div>
                            <label>
                                <span>{t("exportSection.exporter")}</span>
                                <select value={selectedExporter}
                                        onChange={(event) => {
                                            const nextExporter = event.target.value;
                                            const previousDefault = buildDefaultExportPath(settings.exportDir, selectedExporter);
                                            const nextDefault = buildDefaultExportPath(settings.exportDir, nextExporter);
                                            setSelectedExporter(nextExporter);
                                            setExportPath((current) => {
                                                if (current.trim() === "" || current === previousDefault) {
                                                    return nextDefault;
                                                }
                                                return current;
                                            });
                                        }}>
                                    {exporters.map((name) => (
                                        <option key={name} value={name}>{translateExporterLabel(name, t)}</option>
                                    ))}
                                </select>
                            </label>
                            <div className="path-row no-border">
                                <div className="path-meta">
                                    <span className="path-label">{t("exportSection.outputFile")}</span>
                                    <div
                                        className="path-value">{displayPath(exportPath, t("exportSection.noExportFile"))}</div>
                                </div>
                                <button disabled={busy} onClick={chooseExportFile}>{t("common.chooseFile")}</button>
                            </div>
                            {exporterSupportsSkipEmptyFinal(selectedExporter) && (
                                <label className="checkline">
                                    <input type="checkbox" checked={skipEmptyFinal}
                                           onChange={(event) => setSkipEmptyFinal(event.target.checked)}/>
                                    <span>{t("exportSection.skipEmptyFinal")}</span>
                                </label>
                            )}
                            <p className="help">{translateExporterHelp(selectedExporter, t)}</p>
                            {exportProgress && (
                                <div className="import-progress-card">
                                    <div className="import-progress-header">
                                        <span
                                            className={`phase-pill ${exportProgress.phase}`}>{formatExportPhase(exportProgress.phase, t)}</span>
                                        <span
                                            className="import-progress-importer">{translateExporterLabel(exportProgress.exporter, t)}</span>
                                    </div>
                                    <div className="import-progress-file">
                                        {displayPath(exportProgress.outputPath, t("exportProgress.waitingOutput"))}
                                    </div>
                                    <div className="import-progress-grid export-progress-grid">
                                        <div>
                                            <strong>{exportProgress.processedRows}</strong>
                                            <span>{t("exportProgress.rows")}</span>
                                        </div>
                                        <div>
                                            <strong>{exportProgress.exported}</strong>
                                            <span>{t("exportProgress.exported")}</span>
                                        </div>
                                        <div>
                                            <strong>{exportProgress.skipped}</strong>
                                            <span>{t("exportProgress.skipped")}</span>
                                        </div>
                                    </div>
                                </div>
                            )}
                        </div>

                        <div className="panel compact-panel">
                            <div className="panel-title-row">
                                <h2>{t("maintenanceSection.title")}</h2>
                                <button className="accent" disabled={busy} onClick={runMaintenanceCleanup}>
                                    {t("maintenanceSection.run")}
                                </button>
                            </div>
                            <p className="help">{t("maintenanceSection.help")}</p>
                        </div>
                    </div>
                </section>
            )}

            <section className="panel status-panel">
                <div className="panel-title-row">
                    <h2>{t("statusPanel.title")}</h2>
                </div>
                <pre>{message || t("common.noMessagesYet")}</pre>
            </section>
        </div>
    );
}

export default App;
