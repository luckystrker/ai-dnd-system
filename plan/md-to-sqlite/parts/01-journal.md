# Часть 01 — журнал на SQLite (`journal.ts`)

**Цель**: переписать `agent/lib/campaigns/journal.ts` с MD-файлов на SQLite,
сохранив все публичные сигнатуры и формат выдачи.

## Файлы

- `agent/lib/campaigns/journal.ts` — полная переработка.
- `test/journal.test.ts`, `test/ledger.test.ts` — переписать на SQLite-фикстуры
  (см. приёмку; helpers дорабатываются здесь же, если часть 08 ещё не мержена —
  добавь `tempDb()` в `test/helpers.ts`, если его ещё нет).

## Таблицы (DDL в этом модуле, при первом открытии БД)

```sql
CREATE TABLE IF NOT EXISTS campaign_days (
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  day INTEGER NOT NULL,
  date TEXT,
  note TEXT,
  summary TEXT,
  headline TEXT,
  PRIMARY KEY (campaign_id, day)
);

CREATE TABLE IF NOT EXISTS transcript_entries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  day INTEGER NOT NULL,
  line TEXT NOT NULL,          -- отрендеренная строка, как в day-NNNN.md
  event_id TEXT,               -- из маркера "<!-- evt:... -->"
  UNIQUE (campaign_id, event_id)
);

CREATE TABLE IF NOT EXISTS key_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  day INTEGER NOT NULL,
  line TEXT NOT NULL,          -- "- [важно] **День N**: ..." / "- **День N**: ..."
  permanent INTEGER NOT NULL DEFAULT 0,
  event_id TEXT,
  UNIQUE (campaign_id, event_id)
);

CREATE TABLE IF NOT EXISTS ledger_rows (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  day INTEGER NOT NULL,
  type TEXT NOT NULL,          -- "found" | "spent"
  line TEXT NOT NULL,          -- "- [День N] found: ..."
  event_id TEXT,
  UNIQUE (campaign_id, event_id)
);

CREATE TABLE IF NOT EXISTS campaign_summary (
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  day INTEGER NOT NULL,
  text TEXT NOT NULL,          -- содержимое секции "## День N" (без заголовка)
  PRIMARY KEY (campaign_id, day)
);
```

Замечание: `UNIQUE (campaign_id, event_id)` в SQLite допускает много NULL
(нужно для записей без eventId) — поведение именно то, что нужно.

## Публичный API — сохранить без изменений (сигнатуры)

Из текущего `journal.ts`:
`ensureDay`, `listDays`, `appendTranscriptEntry`, `readDay`, `readDayTail`,
`setDaySummary`, `setDayHeadline`, `listDayHeadlines` (+ `DayHeadline`),
`upsertCampaignSummaryDay`, `readCampaignSummary`, `appendKeyEvent`,
`splitKeyEvents`, `readKeyEvents`, `AUTO_DIGEST_MARK`, `buildDayDigest`
(чистая функция — не менять), `appendLedgerRow`, `readLedgerRaw`, `readLedger`.

Плюс **upsert-методы для миграции** (новые, временно используемые частью 07):

```ts
/** Вставляет день с метаданными и готовыми строками транскрипта (для миграции). */
export function importDay(campaignId: string, record: {
  day: number; date?: string; note?: string; summary?: string; headline?: string;
  entries: string[]; // отрендеренные строки "- ..." с маркерами evt
}): void;
export function importKeyEvents(campaignId: string, lines: string[]): void;
export function importLedgerRows(campaignId: string, lines: string[]): void;
export function importSummaryDay(campaignId: string, day: number, text: string): void;
```

Реализация `import*`: парсит из строки `day`/`type`/`permanent`/`event_id`
(маркер в конце строки `<!-- evt:... -->`), пишет `INSERT OR IGNORE`
(идемпотентность). `importSummaryDay` — upsert по (campaign_id, day).

## Правила реализации

- Аргументы остаются `campaignSlug`: внутри резолвится id:
  `SELECT id FROM campaigns WHERE slug = ?` (кэшировать на время вызова не
  обязательно). Если кампании нет — вести себя как раньше с отсутствием папки
  (пустые списки / no-op, **не** бросать исключения).
- Рендер строк точно как сейчас в `appendTranscriptEntry` (journal.ts:88-100),
  `appendKeyEvent` (:192-204), `appendLedgerRow` (:265-279), включая маркеры
  `<!-- evt:... -->`. Время `[HH:MM]` — из `entry.at ?? now`, слайс `(11, 16)`.
- Дедуп по eventId: `INSERT OR IGNORE` + уникальный индекс; после вставки
  проверять `changes === 0` не нужно, это и есть дедуп.
- `readDayTail`: `SELECT line FROM transcript_entries WHERE campaign_id=? AND day=?
  ORDER BY id LIMIT -maxLines` (или выбрать все и slice); вернуть `DayRecord`
  с meta из `campaign_days`.
- `listDays`: `SELECT day FROM campaign_days ... ORDER BY day`.
- `listDayHeadlines`: из `campaign_days` где `headline IS NOT NULL`.
- `readKeyEvents`/`splitKeyEvents`: строки из `key_events` по возрастанию id;
  permanent-разделение по флагу, а не по префиксу (в БД). Формат строки при
  чтении — тот же, что был в файле.
- `upsertCampaignSummaryDay`: upsert по (campaign_id, day); `readCampaignSummary`
  рендерит `## День N\n\n{text}` секциями по возрастанию day.
- `readLedger` фильтры day/type — по колонкам, не по regex.
- `readLedgerRaw`: строки по возрастанию id, join `\n`.
- Не импортировать `campaignDataRoot`, `assertCampaignSlug`, `frontmatter`,
  `node:fs`/`node:path` вообще. Импорт `openCampaignDb` из `./sqlite-db.ts`.
- Комментарии на русском; в шапке модуля описать новые таблицы вместо списка
  файлов.

## Приёмка

- `npm run typecheck`.
- `test/journal.test.ts`, `test/ledger.test.ts` переведены на SQLite:
  фикстура = temp-файл БД (`process.env.CAMPAIGN_DB_PATH` до первого вызова)
  + кампания через `SqliteCampaignStore`; вызовы журнала — со slug кампании.
  Все прежние сценарии (дедуп по eventId, рендер строк, tail-чтение, фильтры
  ledger, digest, headlines) сохранены.
- `npm test` зелёный (полностью; если падают только тесты частей 02–05 —
  они ещё не переехали, их переписывают их части; убедись, что твои тесты
  зелёные сами по себе).
