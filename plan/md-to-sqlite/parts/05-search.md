# Часть 05 — поиск по памяти на SQL (`search.ts`)

**Цель**: переписать `agent/lib/campaigns/search.ts`: вместо обхода `.md` —
SQL-поиск по таблицам памяти.

## Файлы

- `agent/lib/campaigns/search.ts` — полная переработка.
- `test/search.test.ts` — переписать.
- `agent/tools/search_memory.ts` — обновить текст `description`/ответов,
  если они упоминают файлы (проверить).

## Контракт

```ts
export interface MemoryHit {
  /** Человекочитаемый источник: "транскрипт, день 3", "NPC Марта", ... */
  source: string;
  /** Номер игрового дня, если известен. */
  day?: number;
  snippet: string;
}
export function searchCampaignMemory(slug: string, query: string, options?: SearchOptions): MemoryHit[];
export function queryTerms(query: string): string[];
```

- `SearchOptions { limit?, context?, perFile? }` — `perFile` переименовать в
  `perSource`, старое имя удалить (проверить потребителей: только
  `search_memory.ts`, он передаёт `limit`).
- `queryTerms`, `snippetAround`, `normalize`, `DEFAULT_LIMIT`,
  `DEFAULT_CONTEXT` — оставить (без FS).

## Что искать (по `campaign_id`)

| Таблица | Колонки | `source` | `day` |
|---|---|---|---|
| `transcript_entries` | `line` | `"транскрипт, день N"` | day |
| `key_events` | `line` | `"ключевое событие, день N"` | day |
| `ledger_rows` | `line` | `"журнал лута, день N"` | day |
| `world_changes` | `text`, `category` | `"состояние мира"` | day? |
| `npcs` | `name`, `role`, `memory` | `"NPC <name>"` | last_seen_day |
| `locations` | `name`, `description` | `"локация <name>"` | discovered_day |
| `factions` | `name`, `description` | `"фракция <name>"` | — |
| `characters` | `name`, `background`, `motivation` | `"персонаж <name>"` | — |
| `quests` | `title`, `objective` | `"квест <title>"` | created_day |
| `campaigns` | `title`, `description`, `setting` | `"кампания"` | — |

## Правила реализации

- slug→id: `SELECT id FROM campaigns WHERE slug = ?`; нет кампании → `[]`.
- Поиск: `SELECT ... WHERE lower(col) LIKE '%'||?||'%'` по каждому терму
  (AND термов). Сниппеты строятся тем же `snippetAround` по найденной строке.
- Порядок результата: как раньше — сначала больше `day`, затем по source.
  Общий лимит `limit` (срез после сортировки).
- Таблицы из частей 01–04 существуют к моменту использования (их DDL
  выполняется их модулями); этот модуль своих таблиц не создаёт, но вызывать
  DDL других модулей нельзя — просто предполагаем, что БД открыта единым
  handle'ом (`openCampaignDb`) и таблицы уже созданы при первом обращении к
  соответствующим модулям. Если таблицы нет — `SELECT` упадёт: обернуть каждый
  источник в try/catch (пропуск источника) — безопасно.
- Не импортировать `campaignDataRoot`, `assertCampaignSlug`, `node:fs`,
  `node:path`. Импорт `openCampaignDb` из `./sqlite-db.ts`.
- Комментарии на русском.

## Приёмка

- `npm run typecheck`.
- `test/search.test.ts`: фикстура в temp БД — кампания + несколько строк
  транскрипта/событий/NPC; проверить: совпадение по термам, AND-логику,
  сортировку по дню, сниппеты, пустой результат, лимит.
- `agent/tools/search_memory.ts`: description не должен упоминать файлы;
  ответы используют `hit.source`.
- `npm test` зелёный для этих файлов.
