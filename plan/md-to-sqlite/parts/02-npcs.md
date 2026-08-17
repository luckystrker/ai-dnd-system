# Часть 02 — NPC на SQLite (`npc.ts`)

**Цель**: переписать `agent/lib/campaigns/npc.ts` на SQLite, сохранив
публичный API класса и синглтон `npcStore`.

## Файлы

- `agent/lib/campaigns/npc.ts` — полная переработка.
- `test/npc.test.ts`, `test/access.test.ts` — переписать на SQLite-фикстуры.

## Таблица (DDL в этом модуле, при первом открытии БД)

```sql
CREATE TABLE IF NOT EXISTS npcs (
  id TEXT PRIMARY KEY,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  slug TEXT NOT NULL,
  name TEXT NOT NULL,
  role TEXT,
  status TEXT NOT NULL DEFAULT 'alive',
  location TEXT,
  relationships TEXT NOT NULL DEFAULT '{}', -- JSON: имя персонажа -> {attitude, notes?}
  first_seen_day INTEGER,
  last_seen_day INTEGER,
  memory TEXT NOT NULL DEFAULT '',         -- отрендеренные строки памяти NPC
  created_at TEXT NOT NULL,
  updated_at TEXT,
  UNIQUE (campaign_id, slug)
);
```

## Публичный API — сохранить

Класс переименовать в `SqliteNpcStore` (конструктор без аргументов), методы:
`upsertNpc` (возвращает `NpcProfile & { memory: string }`), `getNpc`,
`listNpcs`, `lastMemoryLine`. Синглтон:

```ts
export const npcStore: SqliteNpcStore = lazySingleton();
```

Плюс **upsert для миграции**:

```ts
/** Вставляет NPC с готовой памятью как есть (для миграции). Идемпотентен. */
export function importNpc(profile: NpcProfile, memory: string): void;
```

(`importNpc` — модульная функция или метод; на усмотрение, главное —
идемпотентность: `INSERT OR REPLACE` по id.)

## Правила реализации

- `findCampaign(campaignIdOrSlug)` — как сейчас, через `campaignStore.getCampaign`.
- Память: строки в формате как в файле — `- [День N] текст` (если
  `memoryAppendDay`) или `-текст`; append через конкатенацию
  `memory ? memory + "\n" + line : line` (как сейчас npc.ts:67-73).
- `lastMemoryLine`: искать последнюю строку памяти, начинающуюся с `- ` или
  `* `, обрезка до maxChars + `…` — та же логика.
- `relationships` — JSON-колонка; сериализация/десериализация как в текущих
  `npcToFrontmatter`/`docToNpc` (npc.ts:167-221).
- uniqueSlug — `SELECT 1 FROM npcs WHERE campaign_id = ? AND slug = ?`.
- **Ленивость**: БД открывается при первом вызове метода, не в конструкторе и
  не на импорте модуля (см. решения в PLAN.md).
- Не импортировать `campaignDataRoot`, `assertCampaignSlug`, `frontmatter`,
  `node:fs`/`node:path`. Импорт `openCampaignDb` из `./sqlite-db.ts`.
- Комментарии на русском.

## Приёмка

- `npm run typecheck`.
- `test/npc.test.ts`: все прежние сценарии (upsert по имени, память append,
  get по id/slug/имени, lastMemoryLine, отношения) на SQLite-фикстуре.
- `test/access.test.ts`: убрать `process.env.CAMPAIGN_STORE = "markdown"`
  (строка 10), вместо неё — temp БД через `CAMPAIGN_DB_PATH`; остальные
  сценарии доступа сохранить.
- `npm test` зелёный для этих файлов.
