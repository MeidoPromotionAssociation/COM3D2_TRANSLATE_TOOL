# COM3D2 Translate Tool

[English](#english) | [简体中文](#简体中文)

Desktop translation workflow manager for **CUSTOM ORDER MAID 3D2 / COM3D2**.

This project is a **Wails + Go + React** desktop application focused on managing a large COM3D2 translation database
instead of patching the game at runtime. It integrates ARC scanning, ARC extraction, KAG parsing, translation storage,
manual editing, batch translation, glossary management, `playvoice_notext` handling, optional ASR source-text backfill,
and import/export pipelines into a single tool.

The application is designed for large datasets. It uses **SQLite3**, manual pagination, autosave, filtered batch
operations, translation progress reporting, and LLM request deduplication so that multi-million-entry databases remain
manageable.

## English

### Disclaimer

- This is an unofficial tool. It is not affiliated with KISS / COM3D2 or any translation group.
- This project manages translation data and workflows. It is **not** a runtime plugin for injecting text into the game.
- Machine translation and LLM translation can produce incorrect or inconsistent results. Human review is still required.
- You are responsible for your own game files, backups, API usage, and any third-party service costs.
- This project is 99% written by GPT-5.4.

### What This Tool Is For

COM3D2 stores a large amount of scenario text inside `.arc` archives and `.ks` KAG scripts. After game updates, new ARC
files are added and existing translation workflows often become fragmented across external unpackers, CSV tools,
handwritten text files, and ad-hoc scripts.

This tool centralizes that workflow:

- Scan a configured ARC directory for newly discovered archives
- Open ARC files with **MeidoSerialization v1.6.1**
- Extract only the required `.ks` files automatically
- Parse KAG script content into a structured SQLite database
- Store and manage the mapping:
    - `type`
    - `voice_id`
    - `role`
    - `source_arc`
    - `source_file`
    - `source_text`
    - `translated_text`
    - `polished_text`
- Edit translations manually with autosave
- Translate in batches with Google, Baidu, or OpenAI-compatible APIs
- Backfill missing source text for `playvoice_notext` rows with an ASR server
- Import existing translation assets from several legacy formats
- Export both simple text format and round-trip-safe full snapshots

In practice, it replaces the manual workflow of:

- unpacking `.arc` files outside the app
- running `ks_extract` and `ks_split` manually
- maintaining loose text files with limited identity metadata


<img width="1280" height="720" alt="image" src="https://github.com/user-attachments/assets/055349f7-f3cf-4f3a-95f3-5dfab02bb672" /> <img width="1280" height="720" alt="image" src="https://github.com/user-attachments/assets/1bd3f948-25e7-4260-92d0-809213aa83af" />

<img width="1280" height="720" alt="image" src="https://github.com/user-attachments/assets/1b8e8911-a235-4143-9f47-a93fa6b05b0d" /> <img width="1280" height="720" alt="image" src="https://github.com/user-attachments/assets/c15c86ee-629a-42d3-af90-58d74fcfc24d" />

<img width="1280" height="720" alt="image" src="https://github.com/user-attachments/assets/6fc56561-202d-4ed1-b3ab-96e4612676be" />

### Core Features

- **Integrated ARC workflow**
    - ARC unpacking is built in via MeidoSerialization.
    - Users do not need to manually unpack `.arc` files before parsing.
- **Integrated KAG parsing**
    - The app extracts and parses `.ks` files directly.
    - It functionally absorbs the workflow that previously required separate `ks_extract` / `ks_split` style tooling.
- **SQLite3 translation database**
    - Unique identity key:
        - `type + voice_id + role + source_arc + source_file + source_text`
    - Translation and polish data are stored per entry.
- **Large-dataset oriented UI**
    - Manual pagination on the entry page
    - Debounced autosave for entry edits
    - Debounced autosave for settings and glossary
    - Filter-based batch operations
- **Multiple translation backends**
    - Manual editing
    - Google Translate
    - Baidu Translate
    - OpenAI Chat Completions compatible APIs
    - OpenAI Responses compatible APIs
- **Optional source recognition for voice-only lines**
    - `playvoice_notext` rows preserve `voice_id` even when no source text is available during KAG parsing.
    - The app can later extract matching voice audio from the original ARC and send it to an ASR server to write
      recognized text back into `source_text`.
- **Extensible importer/exporter interfaces**
    - Importers and exporters are implemented as interfaces so more formats can be added later.
- **Status and progress reporting**
    - ARC scan results
    - Import progress
    - Export progress
    - Batch translation progress
    - Source recognition progress
    - Live LLM / ASR request / response / error logs
- **Glossary tooling**
    - Built-in glossary editor
    - JSON import / export for glossary files
    - Context-aware glossary filtering before sending LLM requests

### Data Model

The translation database centers on the following fields:

- `type`
- `voice_id`
- `role`
- `source_arc`
- `source_file`
- `source_text`
- `translated_text`
- `polished_text`
- `translator_status`

`translated_text` is the main translation field.

`polished_text` is an optional post-edited field for workflows such as:

- source text -> machine / LLM translation -> human or LLM polish

The UI also tracks review state with statuses such as:

- `new`
- `translated`
- `polished`
- `reviewed`

### ARC Scan And Parse Workflow

#### Regular scan

When you press **Scan New Arc**, the application:

1. Recursively walks the configured **Arc Scan Directory**
2. Finds files ending in `.arc`
3. Registers each archive in the database by filename and path
4. Automatically parses only ARC filenames that were **not seen before**

This is intentional:

- regular scan is optimized for game updates that add new archives
- already known ARC files are left untouched to avoid unnecessary reparsing
- existing manual translation work is preserved unless you explicitly reparse

If you need to rebuild existing ARC data, use:

- **Reparse** for a single ARC
- **Reparse Failed** for all failed ARC files
- **Reparse All** for every ARC already known to the database

`Reparse All` is the correct action when parser behavior changes and you want to rebuild existing stored rows, for
example:

- after adding support for new KAG patterns such as `playvoice_notext`
- after changing deduplication or preservation logic
- after importing an older database that was parsed with earlier tool versions

#### What happens during ARC parsing

For each ARC selected for parsing:

1. The ARC status becomes `parsing`
2. The app opens the archive lazily with **MeidoSerialization**
3. It enumerates files inside the archive
4. It extracts only `.ks` files into a temporary directory under the configured **Work Directory**
5. It parses the extracted KAG scripts
6. It converts parsed results into structured database entries
7. It deduplicates repeated entries inside the same ARC
8. It replaces stored entries for that ARC in the database
9. ARC status becomes `parsed` or `failed`

#### Important preservation behavior

When an ARC is reparsed, the app does **not** blindly discard all manual work.

If an entry keeps the same unique identity key:

- existing `translated_text`
- existing `polished_text`
- existing `translator_status`
- existing `created_at`

are preserved while the source-side entry list is refreshed.

This allows reparsing updated ARC content without unnecessarily losing translation progress for unchanged rows.

#### Failure handling

If parsing fails:

- the ARC record is marked as `failed`
- the error is stored
- the failed ARC can be reparsed later
- all failed ARC files can be reparsed in one action

### KAG Parsing Details

The built-in parser reads `.ks` files directly and extracts structured text entries from several common KAG patterns.

Recognized entry categories include:

- `talk`
- `narration`
- `subtitle`
- `choice`
- `calldialog`
- `playvoice`
- `playvoice_notext`

The parser also captures contextual metadata when available:

- speaker / role
- `voice_id`
- source ARC
- source `.ks` filename

#### `playvoice_notext` behavior

`playvoice_notext` is used for KAG voice playback entries where a `voice_id` exists but no directly extractable comment
text is present in the script.

When this happens, the parser stores a row with:

- `type = playvoice_notext`
- preserved `voice_id`
- normal `source_arc`
- normal `source_file`
- empty `source_text`

This is intentional. It allows the database to preserve the identity of a voice-only line first, and then optionally
fill the missing original text later through ASR.

When the same ARC is reparsed later, the tool preserves ASR-filled `source_text` for matching `playvoice_notext` rows
instead of wiping it back to empty, as long as the fallback identity still matches:

- `type`
- `voice_id`
- `role`
- `source_arc`
- `source_file`

Encoding handling is built in. The parser can handle:

- UTF-8 with BOM
- UTF-16 LE / BE
- Shift-JIS fallback

### Source Recognition / ASR Backfill

Some COM3D2 workflows, including JAT-related text modules, need to reason about voice lines that do not expose source
text in the KAG script itself. This tool handles that in two stages:

1. parse and store the `playvoice_notext` row immediately
2. optionally recover `source_text` later by running source recognition

The source-recognition workflow is:

1. filter database rows to `playvoice_notext`
2. keep only rows with non-empty `voice_id`
3. locate the source ARC by `source_arc`
4. find the audio file inside that ARC by filename match against `voice_id`
5. extract the matched audio file to a temporary work directory
6. send the extracted file to an ASR server
7. normalize the returned transcription
8. write the recognized text back into `source_text`

Important matching rule:

- audio lookup is based on filename / basename match against `voice_id`
- the current implementation assumes there are no duplicate audio filenames inside a single ARC

This makes it possible to preserve voice-only lines in the database immediately, and then gradually backfill missing
originals later without reparsing the game scripts again.

#### Supported ASR server shape

The built-in source-recognition client currently targets an **OpenAI-style audio transcription API**.

Supported endpoints are:

- single-file:
    - `POST /v1/audio/transcriptions`
- batch:
    - `POST /v1/audio/transcriptions/batch`

The current recommended server is:

- [MeidoPromotionAssociation/Qwen3-ASR-Custom-Server](https://github.com/MeidoPromotionAssociation/Qwen3-ASR-Custom-Server)

The app expects the same basic form fields described by that server:

- single mode:
    - multipart `file`
    - optional `language`
    - optional `prompt`
- batch mode:
    - multipart `files`
    - optional repeated or shared `language`
    - optional repeated or shared `prompt`

#### ASR request behavior

ASR settings currently include:

- base URL
- language
- prompt
- batch size
- concurrency
- timeout
- proxy

Behavior rules:

- the configured base URL should normally point to the single-file endpoint
- if `batch size > 1`, the app automatically sends batch requests to the `/batch` endpoint
- if a batch request fails, that batch automatically falls back to per-file single requests
- if batch mode appears unavailable, the app disables batch mode for the rest of that run to avoid repeated failed calls
- source recognition is manual; it does not run automatically during ARC parsing

This separation is deliberate:

- ARC parsing stays fast and deterministic
- ASR remains opt-in because it depends on an external model server and is much heavier than pure script parsing

### Source Text Normalization And Blank Cleanup

Source text is normalized aggressively during parsing and importing to prevent invisible-garbage rows from polluting the
database.

Normalization includes:

- CRLF / CR normalization to LF
- trimming regular whitespace
- trimming full-width spaces
- removing BOM and zero-width style characters such as:
    - `U+180E`
    - `U+200B`
    - `U+200C`
    - `U+200D`
    - `U+2060`
    - `U+FEFF`

If the resulting source text becomes empty:

- new parsed/imported rows are skipped

For historical databases, the tool also includes a **manual maintenance action** that deletes legacy rows whose
`source_text` becomes empty after the same invisible-whitespace normalization.

This cleanup is **manual on purpose**. It no longer runs automatically at startup, because scanning millions of rows
during launch can freeze the app on very large databases.

### Translation Workflow

The tool supports two target fields:

- `translated`
- `polished`

#### Manual translation

Manual editing happens directly in the entry table:

- edits autosave after a short debounce
- batch operations apply to the currently filtered set
- filtered translation text can be cleared in bulk
- filtered statuses can be updated in bulk

#### Machine translation

Current non-LLM translators:

- **Google Translate**
    - configurable base URL
    - API key
    - format
    - model
    - batch size
    - timeout
- **Baidu Translate**
    - configurable base URL
    - AppID
    - secret
    - timeout

#### LLM translation

Current LLM translators:

- **OpenAI Chat**
- **OpenAI Responses**

Both are implemented against **OpenAI-compatible** HTTP APIs, with configurable:

- base URL
- API key
- model
- custom prompt
- batch size
- concurrency
- timeout
- temperature
- top-p
- presence penalty
- frequency penalty
- max output tokens
- reasoning effort
- extra JSON parameters

### LLM Batch Construction

LLM translation is not done as a naive one-line-per-request loop.

The pipeline first loads all matching entries from SQLite, then builds batches.

For LLM translators, batches are grouped by:

- `source_arc`
- `source_file`

This means lines from the same `.ks` file stay together whenever possible. If a file is larger than the configured batch
size, the file is split into multiple batches, but batching still respects file boundaries.

Each LLM item can include:

- `id`
- `type`
- `speaker` (stored internally from `role`)
- `voice_id`
- `source_arc`
- `source_file`
- `source_text`
- `previous_source_text`
- `next_source_text`
- `existing_translated`
- `existing_polished`

This is especially important for:

- speaker-dependent tone
- repeated short lines
- menu / choice entries
- in-file context disambiguation
- polish mode

### LLM Prompt Behavior

If no custom prompt is provided, the app builds an internal prompt from:

- mode instruction
- context instruction
- glossary instruction
- JSON response contract instruction

For custom prompts, the following placeholders are supported:

- `{{source_language}}`
- `{{target_language}}`
- `{{target_field}}`
- `{{mode_instruction}}`
- `{{context_instruction}}`
- `{{glossary}}`
- `{{response_format}}`

If a custom prompt omits some of these sections, the app automatically appends the missing
mode/context/glossary/response-format instructions so the request still remains valid.

#### Translate vs polish

For normal translation:

- the model is asked to write `translated_text`

For polish mode:

- the model is asked to use:
    - `source_text`
    - `existing_translated`
- and produce:
    - `polished_text`

This means polish mode is explicitly a **source + existing translation -> polished result** workflow, not just a blind
retranslation.

### LLM Glossary Details

The glossary is shared across all LLM translators.

The UI currently edits simple rows with:

- source
- preferred translation
- note

The glossary is stored as JSON in settings and can also be imported/exported as JSON files.

At request time, the app does **not** dump the entire glossary into every prompt. Instead, it filters glossary entries
against the current batch context using:

- current source text
- previous source text
- next source text
- speaker / role
- `voice_id`
- `type`
- `source_arc`
- `source_file`

Only glossary entries relevant to the current batch are injected into the prompt.

Behavior rules:

- if `preferred` is present, the model is asked to use it consistently
- if `preferred` is empty, the entry acts as a note or disambiguation hint

The parser is tolerant:

- the glossary editor exports normalized JSON
- the backend can also understand legacy line-based glossary text and richer JSON matchers if needed

### LLM Response Parsing And Compatibility

This project intentionally avoids forcing official OpenAI JSON-schema body constraints into requests, because many
OpenAI-compatible backends do not implement them correctly.

Instead:

- the app asks the model for JSON through the prompt
- response parsing is defensive and tolerant

Accepted response styles include:

- plain text for single-item batches
- JSON array of strings
- JSON array of objects with `id` + text
- wrapped objects such as:
    - `{"translations":[...]}`
    - `{"items":[...]}`
    - `{"results":[...]}`
- JSON object mapping `id -> text`
- fenced code blocks
- extra commentary before or after the JSON, as long as a valid JSON object/array can still be extracted

Returned IDs are validated before writing results back into the database.

Common refusal phrases in English and Chinese are treated as translation failures rather than successful output.

### Duplicate Avoidance And Reuse

Large translation runs can waste a huge number of requests if repeated source text is sent blindly.

This tool avoids that in two layers.

#### 1. Reuse existing database translations before sending requests

Before new translator calls are made, the app searches existing database rows.

Reuse rules are conservative:

- For `translated` target:
    - reuse only when a given `source_text` has exactly **one unique** non-empty `translated_text` across the database
- For `polished` target:
    - reuse only when a given pair of
        - `source_text`
        - `translated_text`
          has exactly **one unique** non-empty `polished_text` across the database

If multiple conflicting candidates exist, nothing is reused automatically for that key.

#### 2. Deduplicate identical requests inside the current run

After the DB reuse step, identical remaining requests inside the current translation run are collapsed:

- one representative item is sent to the translator
- duplicate items are filled from the cached result after the first one returns

This reduces wasted tokens and redundant HTTP requests for repeated lines.

### Proxy Support

Proxy settings apply to:

- Google Translate
- Baidu Translate
- OpenAI Chat
- OpenAI Responses
- ASR source recognition

Modes:

- `system`
- `direct`
- `custom`

On Windows, system proxy resolution tries:

1. Windows Internet Settings
2. environment proxy variables as fallback

The UI also includes a proxy test button that checks connectivity against:

- `https://www.google.com`

### Import Formats

Importers are interface-based and currently include:

#### `arc-ks-folder-text`

Directory structure:

```text
arc_folder_name\
  some_scene.ks.txt
```

Each line is typically:

```text
source<TAB>translation
```

Multiple adjacent tab separators are tolerated.

#### `arc-source-text-file`

Imports one `.txt` file or a directory of `.txt` files named after ARC files, for example:

- `script.txt`
- `script.arc.txt`

Each line can be:

- source only
- source + translation

#### `ks-extract-csv`

Imports `ks_extract` CSV output.

Matching primarily uses:

- `source_arc`
- `source_file`
- `source_text`

When present, these soft hint columns are also used:

- `type`
- `voice_id`
- `role`

Those hint columns are allowed to be empty. If they do not match anything, the importer falls back to the required
identity columns.

#### `translated-csv`

Imports `*_translated.csv` files.

The importer:

- infers the `.ks` filename from the CSV filename
- looks up matching `source_arc` values from the database
- inserts entries with empty `source_arc` if no existing ARC can be inferred

#### `entry-jsonl`

Round-trip-safe full snapshot import.

This format preserves:

- `type`
- `voiceId`
- `role`
- `sourceArc`
- `sourceFile`
- `sourceText`
- `translatedText`
- `polishedText`
- `translatorStatus`
- timestamps

### Export Formats

Exporters are also interface-based and currently include:

#### `tab-text`

Format:

```text
source<TAB>final_text
```

`final_text` means:

- `polished_text` if present
- otherwise `translated_text`

JAT compatibility details:

- the exporter writes UTF-8 text using JAT-compatible escaping for `\n`, `\t`, `\\`, and related special characters
- if a source text starts with `;` or `$`, the exporter skips that row instead of writing an ambiguous line that JAT
  would parse as a comment or regex rule
- for `playvoice` and `playvoice_notext` rows, the exporter uses `voice_id` as the source-side key instead of
  `source_text`
- this makes source lines containing real newlines, tabs, and backslashes loadable by JAT without manual
  post-processing, while reserved-leading rows are reported as skipped

Important limitation:

- this format cannot preserve source identity such as ARC / file / role / voice
- therefore only **one row is exported for each unique `source_text`**

If multiple candidate rows share the same source text, the exporter picks the best candidate by priority:

1. rows with `polished_text`
2. then higher review state
3. then newer `updated_at`
4. then newer `id`

Use this format only when you want a simple text module and can accept identity loss.

#### `voice-subtitle-text`

Format:

```text
voice_id<TAB>final_text
```

This exporter is meant for JAT-style custom subtitle loading where the lookup key is the voice file identifier instead
of the original script text.

`final_text` means:

- `polished_text` if present
- otherwise `translated_text`

Export rules:

- only rows with non-empty `voice_id` and non-empty `final_text` participate
- rows are deduplicated by `voice_id`
- if multiple rows share the same `voice_id`, the exporter prefers:
    1. rows with `polished_text`
    2. then higher review state
    3. then newer `updated_at`
    4. then newer `id`
- output escaping rules are the same as `tab-text`

This is useful when you want to drive subtitles directly from voice playback keys, including `playvoice` and
`playvoice_notext` workflows.

#### `entry-jsonl`

Line-delimited JSON full snapshot.

Use this when you need:

- round-trip import back into the app
- no loss of source identity
- no loss of review / polish metadata

### UI Behavior For Very Large Databases

The tool is intentionally tuned for large datasets.

Current behavior includes:

- manual pagination instead of loading all rows into one virtual grid
- filtered batch operations instead of page-only actions
- autosave instead of manual save buttons
- long-running task cancellation
- progress reporting for import/export/translation
- status panel logging for LLM requests and responses

The application data is stored under the user config directory. On Windows this is typically:

```text
%AppData%\COM3D2TranslateTool
```

By default the app keeps:

- SQLite database under `data`
- extracted temporary work files under `work`
- default import directory under `imports`
- default export directory under `exports`

### Build And Development

#### Requirements

- Go **1.26**
- Node.js
- **pnpm**
- Wails CLI v2
- Windows is the main target environment for COM3D2 usage

#### Install Wails

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

#### Install frontend dependencies

```bash
cd frontend
pnpm install
cd ..
```

#### Run in development

```bash
wails dev
```

#### Build

```bash
wails build
```

The packaged Windows binary is generated at:

```text
build/bin/COM3D2TranslateTool.exe
```

### Credits

- **MeidoSerialization** for ARC access
- **Wails**
- **React**
- **SQLite**

### License

This repository is licensed under the **BSD 3-Clause License**.

---

## 简体中文

### 免责声明

- 本项目是非官方工具，与 KISS / COM3D2 或任何翻译组均无隶属关系。
- 本项目用于管理翻译数据与工作流，**不是**游戏运行时注入文本的插件。
- 机器翻译与 LLM 翻译都可能出错或前后不一致，仍然需要人工校对。
- 游戏文件、备份、API 使用与第三方服务费用均由使用者自行负责。
- 本项目 99% 由 GPT-5.4 编写

### 这个工具是做什么的

COM3D2 的大量剧情文本存放在 `.arc` 压缩包与 `.ks` KAG 脚本中。游戏更新后，常见流程往往会分散在外部解包器、CSV
工具、零散文本文件和临时脚本之间，维护成本很高。

本工具把这些步骤集中到一个桌面应用中：

- 扫描配置好的 ARC 目录，找到新增档案
- 通过 **MeidoSerialization v1.6.1** 直接读取 ARC
- 自动提取其中需要的 `.ks` 文件
- 解析 KAG 脚本并写入 SQLite 数据库
- 管理如下映射关系：
    - `type`
    - `voice_id`
    - `role`
    - `source_arc`
    - `source_file`
    - `source_text`
    - `translated_text`
    - `polished_text`
- 手动编辑并自动保存
- 批量调用 Google / 百度 / OpenAI 兼容接口翻译
- 用 ASR 服务为 `playvoice_notext` 条目补回缺失原文
- 导入现有翻译资产
- 导出简化文本格式或可完整回导的快照格式

实际效果上，它把过去需要手动做的这些步骤整合进了应用：

- 在软件外手动解包 `.arc`
- 手动运行 `ks_extract`
- 手动运行 `ks_split`
- 维护缺少来源字段的散装文本文件


<img width="1280" height="720" alt="image" src="https://github.com/user-attachments/assets/055349f7-f3cf-4f3a-95f3-5dfab02bb672" /> <img width="1280" height="720" alt="image" src="https://github.com/user-attachments/assets/1bd3f948-25e7-4260-92d0-809213aa83af" />

<img width="1280" height="720" alt="image" src="https://github.com/user-attachments/assets/1b8e8911-a235-4143-9f47-a93fa6b05b0d" /> <img width="1280" height="720" alt="image" src="https://github.com/user-attachments/assets/c15c86ee-629a-42d3-af90-58d74fcfc24d" />

<img width="1280" height="720" alt="image" src="https://github.com/user-attachments/assets/6fc56561-202d-4ed1-b3ab-96e4612676be" />


### 核心特性

- **内建 ARC 工作流**
    - 通过 MeidoSerialization 直接读取 / 解包 ARC
    - 不需要用户先手动处理 `.arc`
- **内建 KAG 解析**
    - 直接提取并解析 `.ks`
    - 功能上吸收了过去依赖 `ks_extract` / `ks_split` 的流程
- **SQLite3 翻译数据库**
    - 唯一键为：
        - `type + voice_id + role + source_arc + source_file + source_text`
    - 每条记录都可保存译文与润色文本
- **面向大数据量的界面**
    - 条目页手动翻页
    - 条目编辑防抖自动保存
    - 设置与术语表防抖自动保存
    - 基于筛选条件的批量操作
- **多种翻译后端**
    - 手动编辑
    - Google Translate
    - Baidu Translate
    - OpenAI Chat 兼容接口
    - OpenAI Responses 兼容接口
- **可选的语音原文补录**
    - 对于 KAG 里没有直接原文的 `playvoice_notext`，程序会先保留 `voice_id` 落库。
    - 之后可按原始 ARC 提取对应语音，并调用 ASR 服务把识别结果写回 `source_text`。
- **导入 / 导出接口可扩展**
    - 导入器和导出器都做成了接口，后续可继续扩展格式
- **状态与进度可见**
    - ARC 扫描状态
    - 导入进度
    - 导出进度
    - 批量翻译进度
    - 原文补录进度
    - LLM / ASR 请求 / 返回 / 报错实时日志
- **术语表能力**
    - 内建术语表编辑器
    - 术语表 JSON 导入 / 导出
    - 发送给 LLM 前会先做上下文筛选

### 数据模型

数据库中的核心字段为：

- `type`
- `voice_id`
- `role`
- `source_arc`
- `source_file`
- `source_text`
- `translated_text`
- `polished_text`
- `translator_status`

其中：

- `translated_text` 是主译文字段
- `polished_text` 是可选的润色字段

适合这样的流程：

- 原文 -> 机翻 / LLM 翻译 -> 人工或 LLM 润色

界面中还会记录状态，例如：

- `new`
- `translated`
- `polished`
- `reviewed`

### ARC 扫描与解析流程

#### 常规扫描

点击 **扫描新 Arc** 后，应用会：

1. 递归遍历设置中的 **Arc 扫描目录**
2. 找到所有 `.arc` 文件
3. 按文件名与路径写入数据库记录
4. 只对**此前未见过的 ARC 文件名**自动执行解析

这样设计是有意为之：

- 常规扫描主要针对游戏更新后新增的 ARC
- 已存在的 ARC 默认不重复解析，避免无意义重跑
- 这样也能避免覆盖已经积累的翻译工作

如果你确实需要重建旧 ARC 的条目，可以使用：

- 单个 ARC 的 **重新解析**
- 所有失败 ARC 的 **一键重新解析失败项**
- 所有已登记 ARC 的 **一键重新解析全部**

当解析器行为发生变化时，应该使用 **一键重新解析全部**，典型场景包括：

- 新增了 `playvoice_notext` 这类过去没入库的 KAG 类型
- 去重或保留逻辑发生变化
- 你导入了旧版本工具生成的数据库，希望按当前解析器重建条目

#### ARC 解析时具体做了什么

对每个待解析 ARC：

1. 先把状态标为 `parsing`
2. 用 **MeidoSerialization** 懒加载打开 ARC
3. 枚举包内文件
4. 只提取其中的 `.ks` 文件到 **工作目录**下的临时目录
5. 解析这些 KAG 脚本
6. 转成结构化数据库条目
7. 对同一 ARC 内重复条目做去重
8. 替换数据库中该 ARC 对应的条目集合
9. 最终状态写成 `parsed` 或 `failed`

#### 重要的保留逻辑

重新解析 ARC 时，程序不是简单粗暴地把旧翻译全丢掉。

只要某条记录的唯一键没有变化，就会保留原来的：

- `translated_text`
- `polished_text`
- `translator_status`
- `created_at`

这样即使重新解析了 ARC，也不会因为源数据重建而无谓丢失已完成的翻译成果。

#### 失败处理

如果解析失败：

- ARC 记录会被标记为 `failed`
- 错误信息会被保存
- 后续可以单独重试
- 也可以一键批量重试所有失败 ARC

### KAG 解析细节

内建解析器直接读取 `.ks` 文件，并从常见 KAG 结构中提取文本。

目前会识别的条目类型包括：

- `talk`
- `narration`
- `subtitle`
- `choice`
- `calldialog`
- `playvoice`
- `playvoice_notext`

同时也会尽可能保留上下文元数据：

- speaker / role
- `voice_id`
- 来源 ARC
- 来源 `.ks` 文件名

#### `playvoice_notext` 的处理方式

`playvoice_notext` 用来表示这类 KAG 语音播放项：

- 脚本里能解析出 `voice_id`
- 但脚本本身没有可直接提取的注释文本 / 原文

遇到这种情况时，解析器会写入一条记录，其中：

- `type = playvoice_notext`
- `voice_id` 会保留
- `source_arc` / `source_file` 正常保留
- `source_text` 先留空

这是刻意设计的。目的不是“跳过这条”，而是先把这条语音行的身份信息落库，之后再通过 ASR 补回原文。

如果后续已经用 ASR 给这条 `playvoice_notext` 填回了 `source_text`，再次重新解析同一个 ARC
时，程序也不会简单把它冲回空串。只要下面这组回退身份仍然一致，就会保留 ASR 识别出的原文：

- `type`
- `voice_id`
- `role`
- `source_arc`
- `source_file`

编码处理也是内建的，可识别：

- UTF-8 BOM
- UTF-16 LE / BE
- Shift-JIS 回退

### `playvoice_notext` 与 ASR 原文补录

有些 COM3D2 工作流，尤其是与 JAT 通用文本模块相关的流程，需要处理这类“有语音但 KAG 里没有原文”的条目。本工具对它分两步处理：

1. 先把 `playvoice_notext` 条目入库
2. 之后按需运行 ASR，把缺失的 `source_text` 补回来

原文补录的整体流程如下：

1. 先从数据库筛出 `playvoice_notext` 记录
2. 只处理 `voice_id` 非空的条目
3. 根据 `source_arc` 找到来源 ARC
4. 在该 ARC 内按文件名匹配 `voice_id`
5. 把命中的音频提取到工作目录下的临时文件
6. 把音频发送给 ASR 服务
7. 对返回文本做规范化
8. 将识别结果写回 `source_text`

当前的关键匹配规则是：

- 音频查找按文件名 / 基名与 `voice_id` 匹配
- 当前实现假定同一个 ARC 内不会出现重名音频文件

这样做的好处是：

- KAG 解析阶段就能先把“只有语音 ID 的行”保留下来
- 之后可以逐步补原文，而不必重新跑一遍脚本解析

#### 支持的 ASR 服务接口形态

当前内建的原文补录客户端面向 **OpenAI 风格的音频转写接口**。

支持的接口形态为：

- 单文件：
    - `POST /v1/audio/transcriptions`
- 批量：
    - `POST /v1/audio/transcriptions/batch`

当前推荐配合使用的服务是：

- [MeidoPromotionAssociation/Qwen3-ASR-Custom-Server](https://github.com/MeidoPromotionAssociation/Qwen3-ASR-Custom-Server)

程序默认假定它的表单字段与该服务说明一致：

- 单文件模式：
    - multipart `file`
    - 可选 `language`
    - 可选 `prompt`
- 批量模式：
    - multipart `files`
    - 可选重复或共享的 `language`
    - 可选重复或共享的 `prompt`

#### ASR 请求行为

当前 ASR 配置项包括：

- base URL
- language
- prompt
- batch size
- concurrency
- timeout
- proxy

行为规则如下：

- 通常把 base URL 配成单文件接口即可
- 当 `batch size > 1` 时，程序会自动把批请求发送到 `/batch` 接口
- 如果某个批量请求失败，该批会自动回退成逐条单文件请求
- 如果判断本轮运行里的 batch 接口不可用，后续会自动关闭 batch 模式，避免重复撞失败请求
- 原文补录是手动触发的，不会在 ARC 解析时自动执行

这样拆开是有意为之：

- ARC 解析保持快速、可重复、可预测
- ASR 属于外部模型服务，成本和耗时都明显高于纯脚本解析，所以改成按需执行

### 原文规范化与空白清理

为了避免各种不可见字符把数据库污染成“看起来空白但实际上不是空”的脏数据，原文在解析和导入时都会先做规范化。

规范化包括：

- 把 CRLF / CR 统一成 LF
- 去除普通空白
- 去除全角空格
- 去除 BOM 与零宽字符，例如：
    - `U+180E`
    - `U+200B`
    - `U+200C`
    - `U+200D`
    - `U+2060`
    - `U+FEFF`

如果规范化后原文变成空串：

- 新解析 / 新导入的这条记录会直接跳过

对于历史数据库，还提供了一个**手动维护动作**，用于删除那些在相同规范化规则下会变成空串的旧记录。

这个维护动作现在是**手动触发**，不会在启动时自动运行。原因很简单：

- 数据库来到几百万行以后，启动阶段扫描整表会明显卡死界面

### 翻译工作流

工具支持两个目标字段：

- `translated`
- `polished`

#### 手动翻译

手动编辑直接在条目表里完成：

- 输入后防抖自动保存
- 支持对当前筛选结果做批量操作
- 可批量清空已筛选条目的译文 / 润色文本
- 可批量修改已筛选条目的状态

#### 普通机器翻译

当前非 LLM 翻译器包括：

- **Google Translate**
    - 可配置 base URL
    - API key
    - format
    - model
    - batch size
    - timeout
- **Baidu Translate**
    - 可配置 base URL
    - AppID
    - secret
    - timeout

#### LLM 翻译

当前 LLM 翻译器包括：

- **OpenAI Chat**
- **OpenAI Responses**

两者都按 **OpenAI 兼容接口**实现，并支持自定义：

- base URL
- API key
- model
- 自定义提示词
- batch size
- concurrency
- timeout
- temperature
- top-p
- presence penalty
- frequency penalty
- max output tokens
- reasoning effort
- extra JSON parameters

### LLM 分批细节

LLM 翻译不是简单地“一条一请求”。

程序会先从 SQLite 中分页取出所有符合条件的条目，再进行分批。

对于 LLM 翻译器，分批会优先按以下维度保持同组：

- `source_arc`
- `source_file`

也就是说，来自同一个 `.ks` 文件的文本会尽量放在同一个批次内。如果单个文件过大，就再按你设置的 batch size 继续切分，但仍然不会跨文件混批。

每个 LLM 请求项可包含：

- `id`
- `type`
- `speaker`（内部来自 `role`）
- `voice_id`
- `source_arc`
- `source_file`
- `source_text`
- `previous_source_text`
- `next_source_text`
- `existing_translated`
- `existing_polished`

这对下面这些场景尤其重要：

- 依赖说话人口吻
- 重复短句
- 菜单 / 选项文本
- 需要前后文消歧的句子
- 润色模式

### LLM 提示词行为

如果用户没有自定义提示词，程序会自动拼出内置提示词，内容由这些部分组成：

- 任务模式说明
- 上下文约束说明
- 术语表说明
- JSON 返回格式说明

如果使用自定义提示词，可以使用这些占位符：

- `{{source_language}}`
- `{{target_language}}`
- `{{target_field}}`
- `{{mode_instruction}}`
- `{{context_instruction}}`
- `{{glossary}}`
- `{{response_format}}`

即使自定义提示词里没有写全这些部分，程序也会把缺失的 mode / context / glossary / response-format 说明自动补回去，避免提示词失去必要约束。

#### 翻译模式与润色模式

普通翻译模式下：

- 模型被要求生成 `translated_text`

润色模式下：

- 模型会拿到：
    - `source_text`
    - `existing_translated`
- 并生成：
    - `polished_text`

所以润色模式本质上是：

- 原文 + 现有译文 -> 润色文本

而不是把原文重新翻译一遍。

### LLM 术语表细节

术语表由所有 LLM 翻译器共享。

当前 UI 中术语表的编辑列为：

- 原文
- 推荐译法
- 备注

术语表会保存为 JSON，并支持 JSON 文件导入 / 导出。

真正发送请求时，程序不会把全部术语一股脑塞进每个 prompt，而是先根据当前批次上下文做筛选，匹配维度包括：

- 当前原文
- 前一条原文
- 后一条原文
- speaker / role
- `voice_id`
- `type`
- `source_arc`
- `source_file`

只有与当前批次相关的术语才会进 prompt。

规则如下：

- 如果 `preferred` 非空，就作为优先译法使用
- 如果 `preferred` 为空，就把该条当作备注 / 消歧提示

另外，后端解析器本身比较宽容：

- 前端默认导出标准化 JSON
- 后端也能兼容一些旧式行文本术语表与更丰富的 JSON matcher 结构

### LLM 返回解析与兼容性

本项目**刻意不强行向接口 body 注入官方 OpenAI JSON Schema 约束**，因为很多 OpenAI 兼容后端并不能正确支持它们。

当前策略是：

- 在 prompt 中要求模型返回 JSON
- 在客户端侧做容错解析

可接受的返回形式包括：

- 单条任务时直接返回纯文本
- JSON 字符串数组
- 带 `id` + 文本字段的对象数组
- 外层包装对象，例如：
    - `{"translations":[...]}`
    - `{"items":[...]}`
    - `{"results":[...]}`
- `id -> text` 的 JSON 对象
- Markdown 代码块包裹
- JSON 前后有额外说明文字，只要还能提取出合法 JSON 对象 / 数组

在真正写回数据库之前，程序还会验证返回的 ID 是否和请求项一致。

常见的中英文拒答语句会被识别成失败，而不是当作有效翻译。

### 防重复发送与复用策略

如果在大批量翻译时把重复文本全都原样发给模型，会浪费大量 token 和请求时间。

本工具分两层减少浪费。

#### 1. 在发送请求前复用数据库中已有译文

在真正调用翻译器之前，程序会先查询数据库中已有记录。

复用规则是保守的：

- 目标字段为 `translated` 时：
    - 只有当同一个 `source_text` 在数据库里对应**唯一一个**非空 `translated_text` 时，才会自动复用
- 目标字段为 `polished` 时：
    - 只有当同一个：
        - `source_text`
        - `translated_text`
          对应**唯一一个**非空 `polished_text` 时，才会自动复用

如果存在多个冲突候选，就不会自动复用。

#### 2. 在本次运行内部再做重复请求折叠

在数据库复用之后，剩下仍需发送的条目还会再做一次去重：

- 相同请求只发送一个代表项
- 其他重复项在首条返回后直接用缓存结果回填

这样能显著减少重复文本造成的 token 浪费。

### 代理支持

代理设置会应用到：

- Google Translate
- Baidu Translate
- OpenAI Chat
- OpenAI Responses
- ASR 原文补录

代理模式有：

- `system`
- `direct`
- `custom`

在 Windows 上，系统代理解析顺序为：

1. Windows Internet Settings
2. 环境变量代理作为回退

界面中还提供了代理测试按钮，测试目标为：

- `https://www.google.com`

### 导入格式

导入器都是接口实现，目前包括：

#### `arc-ks-folder-text`

目录结构类似：

```text
arc_folder_name\
  some_scene.ks.txt
```

每行通常是：

```text
source<TAB>translation
```

允许连续多个 TAB 分隔。

#### `arc-source-text-file`

导入单个 `.txt` 文件或一个 `.txt` 目录，文件名按 ARC 命名，例如：

- `script.txt`
- `script.arc.txt`

每行可以是：

- 仅原文
- 原文 + 译文

#### `ks-extract-csv`

导入 `ks_extract` 的 CSV。

主要匹配字段为：

- `source_arc`
- `source_file`
- `source_text`

如果存在以下列，也会作为软提示参与匹配：

- `type`
- `voice_id`
- `role`

这些软提示列允许为空。如果带软提示匹配不到，程序会自动回退到只用必需字段匹配。

#### `translated-csv`

导入 `*_translated.csv` 文件。

处理逻辑为：

- 根据 CSV 文件名反推出 `.ks` 文件名
- 再从数据库中查找对应的 `source_arc`
- 如果数据库里没有已知 ARC，对应插入记录时允许 `source_arc` 为空

#### `entry-jsonl`

可回导的完整快照导入格式。

会保留：

- `type`
- `voiceId`
- `role`
- `sourceArc`
- `sourceFile`
- `sourceText`
- `translatedText`
- `polishedText`
- `translatorStatus`
- 时间戳

### 导出格式

导出器同样做成了接口，目前包括：

#### `tab-text`

格式为：

```text
source<TAB>final_text
```

其中 `final_text` 的选择逻辑是：

- 有 `polished_text` 就用 `polished_text`
- 否则用 `translated_text`

与 JAT 兼容的细节：

- 导出时会按 JAT 的规则转义 `\n`、`\t`、`\\` 等特殊字符
- 如果原文以 `;` 或 `$` 开头，导出器会直接跳过这一行，避免生成会被 JAT 误判为注释或普通正则定义的歧义内容
- 对 `playvoice` 和 `playvoice_notext` 条目，导出器会使用 `voice_id` 作为左侧键，而不是 `source_text`
- 因此即使原文或译文里包含真实换行、Tab、反斜杠，也不需要手工再修格式；只有这类保留前缀行会被计入 skipped

这个格式有一个天然限制：

- 它无法保留 ARC / 文件 / 角色 / 语音等来源身份信息
- 因此每个唯一 `source_text` 最终只会导出 **一行**

如果同一原文在数据库中有多个候选结果，程序会按以下优先级挑出“最佳候选”：

1. 有 `polished_text` 的行
2. 状态更高的行
3. `updated_at` 更新更晚的行
4. `id` 更大的行

如果你只是想导出简化文本模块，这个格式适合；如果你需要完整保留来源信息，请不要用它。

#### `voice-subtitle-text`

格式为：

```text
voice_id<TAB>final_text
```

这个导出器面向按语音键加载字幕的 JAT 风格自定义字幕模块，左侧查找键不是脚本原文，而是 `voice_id`。

其中 `final_text` 的选择逻辑是：

- 有 `polished_text` 就用 `polished_text`
- 否则用 `translated_text`

导出规则如下：

- 只有 `voice_id` 非空且 `final_text` 非空的条目会参与导出
- 程序会按 `voice_id` 去重
- 如果同一个 `voice_id` 对应多条记录，会按以下优先级挑选最佳候选：
    1. 有 `polished_text` 的行
    2. 状态更高的行
    3. `updated_at` 更新更晚的行
    4. `id` 更大的行
- 转义规则与 `tab-text` 完全相同

这个格式适合直接按语音播放键驱动字幕的场景，也适合 `playvoice` / `playvoice_notext` 相关工作流。

#### `entry-jsonl`

逐行 JSON 的完整快照格式。

适合这些需求：

- 之后还要重新导回本工具
- 不能丢失来源身份信息
- 不能丢失审核 / 润色元数据

### 面向超大数据库的界面行为

这个工具是按大数据量来设计的。

当前行为包括：

- 手动翻页，而不是一次加载全部条目
- 批量操作基于筛选条件，而不是只作用当前页
- 自动保存，而不是大量手动点保存
- 长任务支持停止
- 导入 / 导出 / 翻译都有进度
- 状态栏实时输出 LLM 请求与返回日志

应用数据存放在用户配置目录下。在 Windows 上通常是：

```text
%AppData%\COM3D2TranslateTool
```

默认会包含：

- `data` 里的 SQLite 数据库
- `work` 里的临时提取工作目录
- `imports` 默认导入目录
- `exports` 默认导出目录

### 构建与开发

#### 依赖要求

- Go **1.26**
- Node.js
- **pnpm**
- Wails CLI v2
- 实际使用环境以 Windows 为主

#### 安装 Wails

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

#### 安装前端依赖

```bash
cd frontend
pnpm install
cd ..
```

#### 开发模式

```bash
wails dev
```

#### 构建

```bash
wails build
```

Windows 打包产物位于：

```text
build/bin/COM3D2TranslateTool.exe
```

### 致谢

- **MeidoSerialization** 提供 ARC 读取能力
- **Wails**
- **React**
- **SQLite**

### 许可证

本仓库采用 **BSD 3-Clause License**。
