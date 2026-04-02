export namespace model {
	
	export class ASRConfig {
	    baseUrl: string;
	    language: string;
	    prompt: string;
	    batchSize: number;
	    timeoutSeconds: number;
	    concurrency: number;
	
	    static createFrom(source: any = {}) {
	        return new ASRConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.baseUrl = source["baseUrl"];
	        this.language = source["language"];
	        this.prompt = source["prompt"];
	        this.batchSize = source["batchSize"];
	        this.timeoutSeconds = source["timeoutSeconds"];
	        this.concurrency = source["concurrency"];
	    }
	}
	export class ASRTestResult {
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
	
	    static createFrom(source: any = {}) {
	        return new ASRTestResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.endpoint = source["endpoint"];
	        this.finalUrl = source["finalUrl"];
	        this.proxyMode = source["proxyMode"];
	        this.resolvedProxy = source["resolvedProxy"];
	        this.statusCode = source["statusCode"];
	        this.status = source["status"];
	        this.responseTime = source["responseTime"];
	        this.responseLanguage = source["responseLanguage"];
	        this.responseText = source["responseText"];
	        this.responseBody = source["responseBody"];
	    }
	}
	export class ArcFile {
	    id: number;
	    filename: string;
	    path: string;
	    status: string;
	    lastError: string;
	    discoveredAt: string;
	    parsedAt: string;
	    lastScannedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new ArcFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.filename = source["filename"];
	        this.path = source["path"];
	        this.status = source["status"];
	        this.lastError = source["lastError"];
	        this.discoveredAt = source["discoveredAt"];
	        this.parsedAt = source["parsedAt"];
	        this.lastScannedAt = source["lastScannedAt"];
	    }
	}
	export class BaiduTranslateConfig {
	    baseUrl: string;
	    appId: string;
	    secret: string;
	    timeoutSeconds: number;
	
	    static createFrom(source: any = {}) {
	        return new BaiduTranslateConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.baseUrl = source["baseUrl"];
	        this.appId = source["appId"];
	        this.secret = source["secret"];
	        this.timeoutSeconds = source["timeoutSeconds"];
	    }
	}
	export class BatchDeleteResult {
	    deleted: number;
	
	    static createFrom(source: any = {}) {
	        return new BatchDeleteResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.deleted = source["deleted"];
	    }
	}
	export class BatchUpdateInput {
	    ids: number[];
	    translatedText: string;
	    polishedText: string;
	    translatorStatus: string;
	
	    static createFrom(source: any = {}) {
	        return new BatchUpdateInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ids = source["ids"];
	        this.translatedText = source["translatedText"];
	        this.polishedText = source["polishedText"];
	        this.translatorStatus = source["translatorStatus"];
	    }
	}
	export class BatchUpdateResult {
	    updated: number;
	
	    static createFrom(source: any = {}) {
	        return new BatchUpdateResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.updated = source["updated"];
	    }
	}
	export class Entry {
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
	
	    static createFrom(source: any = {}) {
	        return new Entry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.type = source["type"];
	        this.voiceId = source["voiceId"];
	        this.role = source["role"];
	        this.sourceArc = source["sourceArc"];
	        this.sourceFile = source["sourceFile"];
	        this.sourceText = source["sourceText"];
	        this.translatedText = source["translatedText"];
	        this.polishedText = source["polishedText"];
	        this.translatorStatus = source["translatorStatus"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class EntryList {
	    items: Entry[];
	    total: number;
	
	    static createFrom(source: any = {}) {
	        return new EntryList(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], Entry);
	        this.total = source["total"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class EntryQuery {
	    search: string;
	    sourceArc: string;
	    sourceFile: string;
	    type: string;
	    status: string;
	    untranslatedOnly: boolean;
	    limit: number;
	    offset: number;
	
	    static createFrom(source: any = {}) {
	        return new EntryQuery(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.search = source["search"];
	        this.sourceArc = source["sourceArc"];
	        this.sourceFile = source["sourceFile"];
	        this.type = source["type"];
	        this.status = source["status"];
	        this.untranslatedOnly = source["untranslatedOnly"];
	        this.limit = source["limit"];
	        this.offset = source["offset"];
	    }
	}
	export class ExportRequest {
	    exporter: string;
	    outputPath: string;
	    search: string;
	    sourceArc: string;
	    sourceFile: string;
	    type: string;
	    status: string;
	    untranslatedOnly: boolean;
	    skipEmptyFinal: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ExportRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.exporter = source["exporter"];
	        this.outputPath = source["outputPath"];
	        this.search = source["search"];
	        this.sourceArc = source["sourceArc"];
	        this.sourceFile = source["sourceFile"];
	        this.type = source["type"];
	        this.status = source["status"];
	        this.untranslatedOnly = source["untranslatedOnly"];
	        this.skipEmptyFinal = source["skipEmptyFinal"];
	    }
	}
	export class ExportResult {
	    exporter: string;
	    outputPath: string;
	    exported: number;
	    skipped: number;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new ExportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.exporter = source["exporter"];
	        this.outputPath = source["outputPath"];
	        this.exported = source["exported"];
	        this.skipped = source["skipped"];
	        this.message = source["message"];
	    }
	}
	export class FilterBatchStatusInput {
	    query: EntryQuery;
	    translatorStatus: string;
	
	    static createFrom(source: any = {}) {
	        return new FilterBatchStatusInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.query = this.convertValues(source["query"], EntryQuery);
	        this.translatorStatus = source["translatorStatus"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class FilterOptions {
	    arcs: string[];
	    files: string[];
	    types: string[];
	    statuses: string[];
	
	    static createFrom(source: any = {}) {
	        return new FilterOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.arcs = source["arcs"];
	        this.files = source["files"];
	        this.types = source["types"];
	        this.statuses = source["statuses"];
	    }
	}
	export class GoogleTranslateConfig {
	    baseUrl: string;
	    apiKey: string;
	    format: string;
	    model: string;
	    batchSize: number;
	    timeoutSeconds: number;
	
	    static createFrom(source: any = {}) {
	        return new GoogleTranslateConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.baseUrl = source["baseUrl"];
	        this.apiKey = source["apiKey"];
	        this.format = source["format"];
	        this.model = source["model"];
	        this.batchSize = source["batchSize"];
	        this.timeoutSeconds = source["timeoutSeconds"];
	    }
	}
	export class ImportRequest {
	    importer: string;
	    rootDir: string;
	    allowOverwrite: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ImportRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.importer = source["importer"];
	        this.rootDir = source["rootDir"];
	        this.allowOverwrite = source["allowOverwrite"];
	    }
	}
	export class ImportResult {
	    importer: string;
	    filesProcessed: number;
	    totalLines: number;
	    inserted: number;
	    updated: number;
	    skipped: number;
	    unmatched: number;
	    errorLines: number;
	    messages: string[];
	
	    static createFrom(source: any = {}) {
	        return new ImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.importer = source["importer"];
	        this.filesProcessed = source["filesProcessed"];
	        this.totalLines = source["totalLines"];
	        this.inserted = source["inserted"];
	        this.updated = source["updated"];
	        this.skipped = source["skipped"];
	        this.unmatched = source["unmatched"];
	        this.errorLines = source["errorLines"];
	        this.messages = source["messages"];
	    }
	}
	export class MaintenanceResult {
	    deletedInvisibleBlankEntries: number;
	
	    static createFrom(source: any = {}) {
	        return new MaintenanceResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.deletedInvisibleBlankEntries = source["deletedInvisibleBlankEntries"];
	    }
	}
	export class OpenAIProviderConfig {
	    baseUrl: string;
	    apiKey: string;
	    model: string;
	    prompt: string;
	    batchSize: number;
	    concurrency: number;
	    timeoutSeconds: number;
	    temperature?: number;
	    topP?: number;
	    presencePenalty?: number;
	    frequencyPenalty?: number;
	    maxOutputTokens?: number;
	    reasoningEffort: string;
	    extraJson: string;
	
	    static createFrom(source: any = {}) {
	        return new OpenAIProviderConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.baseUrl = source["baseUrl"];
	        this.apiKey = source["apiKey"];
	        this.model = source["model"];
	        this.prompt = source["prompt"];
	        this.batchSize = source["batchSize"];
	        this.concurrency = source["concurrency"];
	        this.timeoutSeconds = source["timeoutSeconds"];
	        this.temperature = source["temperature"];
	        this.topP = source["topP"];
	        this.presencePenalty = source["presencePenalty"];
	        this.frequencyPenalty = source["frequencyPenalty"];
	        this.maxOutputTokens = source["maxOutputTokens"];
	        this.reasoningEffort = source["reasoningEffort"];
	        this.extraJson = source["extraJson"];
	    }
	}
	export class ParseResult {
	    arcFilename: string;
	    entryCount: number;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new ParseResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.arcFilename = source["arcFilename"];
	        this.entryCount = source["entryCount"];
	        this.message = source["message"];
	    }
	}
	export class ProxyConfig {
	    mode: string;
	    url: string;
	
	    static createFrom(source: any = {}) {
	        return new ProxyConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.url = source["url"];
	    }
	}
	export class ProxyTestResult {
	    targetUrl: string;
	    finalUrl: string;
	    proxyMode: string;
	    resolvedProxy: string;
	    statusCode: number;
	    status: string;
	    bytesRead: number;
	
	    static createFrom(source: any = {}) {
	        return new ProxyTestResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.targetUrl = source["targetUrl"];
	        this.finalUrl = source["finalUrl"];
	        this.proxyMode = source["proxyMode"];
	        this.resolvedProxy = source["resolvedProxy"];
	        this.statusCode = source["statusCode"];
	        this.status = source["status"];
	        this.bytesRead = source["bytesRead"];
	    }
	}
	export class ReparseAllResult {
	    totalArcs: number;
	    reparsedCount: number;
	    failedCount: number;
	    skippedCount: number;
	    messages: string[];
	
	    static createFrom(source: any = {}) {
	        return new ReparseAllResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.totalArcs = source["totalArcs"];
	        this.reparsedCount = source["reparsedCount"];
	        this.failedCount = source["failedCount"];
	        this.skippedCount = source["skippedCount"];
	        this.messages = source["messages"];
	    }
	}
	export class ReparseFailedResult {
	    totalFailed: number;
	    reparsedCount: number;
	    failedCount: number;
	    messages: string[];
	
	    static createFrom(source: any = {}) {
	        return new ReparseFailedResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.totalFailed = source["totalFailed"];
	        this.reparsedCount = source["reparsedCount"];
	        this.failedCount = source["failedCount"];
	        this.messages = source["messages"];
	    }
	}
	export class ScanResult {
	    scanned: number;
	    newArcCount: number;
	    parsedCount: number;
	    failedCount: number;
	    messages: string[];
	
	    static createFrom(source: any = {}) {
	        return new ScanResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.scanned = source["scanned"];
	        this.newArcCount = source["newArcCount"];
	        this.parsedCount = source["parsedCount"];
	        this.failedCount = source["failedCount"];
	        this.messages = source["messages"];
	    }
	}
	export class TranslationSettings {
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
	
	    static createFrom(source: any = {}) {
	        return new TranslationSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.activeTranslator = source["activeTranslator"];
	        this.sourceLanguage = source["sourceLanguage"];
	        this.targetLanguage = source["targetLanguage"];
	        this.glossary = source["glossary"];
	        this.proxy = this.convertValues(source["proxy"], ProxyConfig);
	        this.google = this.convertValues(source["google"], GoogleTranslateConfig);
	        this.baidu = this.convertValues(source["baidu"], BaiduTranslateConfig);
	        this.asr = this.convertValues(source["asr"], ASRConfig);
	        this.openAIChat = this.convertValues(source["openAIChat"], OpenAIProviderConfig);
	        this.openAIResponses = this.convertValues(source["openAIResponses"], OpenAIProviderConfig);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Settings {
	    arcScanDir: string;
	    workDir: string;
	    importDir: string;
	    exportDir: string;
	    translation: TranslationSettings;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.arcScanDir = source["arcScanDir"];
	        this.workDir = source["workDir"];
	        this.importDir = source["importDir"];
	        this.exportDir = source["exportDir"];
	        this.translation = this.convertValues(source["translation"], TranslationSettings);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SourceRecognitionRequest {
	    search: string;
	    sourceArc: string;
	    sourceFile: string;
	    type: string;
	    status: string;
	    untranslatedOnly: boolean;
	    allowOverwrite: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SourceRecognitionRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.search = source["search"];
	        this.sourceArc = source["sourceArc"];
	        this.sourceFile = source["sourceFile"];
	        this.type = source["type"];
	        this.status = source["status"];
	        this.untranslatedOnly = source["untranslatedOnly"];
	        this.allowOverwrite = source["allowOverwrite"];
	    }
	}
	export class SourceRecognitionResult {
	    provider: string;
	    total: number;
	    processed: number;
	    updated: number;
	    skipped: number;
	    failed: number;
	    messages: string[];
	
	    static createFrom(source: any = {}) {
	        return new SourceRecognitionResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider = source["provider"];
	        this.total = source["total"];
	        this.processed = source["processed"];
	        this.updated = source["updated"];
	        this.skipped = source["skipped"];
	        this.failed = source["failed"];
	        this.messages = source["messages"];
	    }
	}
	export class TranslateRequest {
	    translator: string;
	    search: string;
	    sourceArc: string;
	    sourceFile: string;
	    type: string;
	    status: string;
	    untranslatedOnly: boolean;
	    allowOverwrite: boolean;
	    targetField: string;
	
	    static createFrom(source: any = {}) {
	        return new TranslateRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.translator = source["translator"];
	        this.search = source["search"];
	        this.sourceArc = source["sourceArc"];
	        this.sourceFile = source["sourceFile"];
	        this.type = source["type"];
	        this.status = source["status"];
	        this.untranslatedOnly = source["untranslatedOnly"];
	        this.allowOverwrite = source["allowOverwrite"];
	        this.targetField = source["targetField"];
	    }
	}
	export class TranslateResult {
	    translator: string;
	    targetField: string;
	    total: number;
	    processed: number;
	    updated: number;
	    skipped: number;
	    failed: number;
	    messages: string[];
	
	    static createFrom(source: any = {}) {
	        return new TranslateResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.translator = source["translator"];
	        this.targetField = source["targetField"];
	        this.total = source["total"];
	        this.processed = source["processed"];
	        this.updated = source["updated"];
	        this.skipped = source["skipped"];
	        this.failed = source["failed"];
	        this.messages = source["messages"];
	    }
	}
	
	export class TranslatorTestRequest {
	    translator: string;
	    targetField: string;
	    settings: TranslationSettings;
	
	    static createFrom(source: any = {}) {
	        return new TranslatorTestRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.translator = source["translator"];
	        this.targetField = source["targetField"];
	        this.settings = this.convertValues(source["settings"], TranslationSettings);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TranslatorTestResult {
	    translator: string;
	    targetField: string;
	    sourceText: string;
	    outputText: string;
	    responseTime: number;
	
	    static createFrom(source: any = {}) {
	        return new TranslatorTestResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.translator = source["translator"];
	        this.targetField = source["targetField"];
	        this.sourceText = source["sourceText"];
	        this.outputText = source["outputText"];
	        this.responseTime = source["responseTime"];
	    }
	}
	export class UpdateEntryInput {
	    id: number;
	    translatedText: string;
	    polishedText: string;
	    translatorStatus: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateEntryInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.translatedText = source["translatedText"];
	        this.polishedText = source["polishedText"];
	        this.translatorStatus = source["translatorStatus"];
	    }
	}

}

