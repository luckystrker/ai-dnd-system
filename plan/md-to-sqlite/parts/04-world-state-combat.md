# Часть 04 — состояние мира и снимок боя на SQLite

**Цель**: переписать `agent/lib/campaigns/world-state.ts` и
`agent/lib/campaigns/combat-store.ts` на SQLite, сохранив публичные функции.

## Файлы

- `agent/lib/campaigns/world-state.ts` — полная переработка.
- `agent/lib/campaigns/combat-store.ts` — полная переработка.
- `test/world-state.test.ts`, `test/combat-persist.test.ts` — переписать.

## Таблицы (DDL в своих модулях, при первом открытии БД)

```sql
CREATE TABLE IF NOT EXISTS world_changes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  category TEXT NOT NULL,
  text TEXT NOT NULL,            -- чистый текст без "- " и без "(день N)"
  day INTEGER,                   -- NULL = без пометки дня
  text_norm TEXT NOT NULL,       -- lower(text)
  UNIQUE (campaign_id, category, text_norm)
);

CREATE TABLE IF NOT EXISTS combat_snapshot (
  campaign_id TEXT PRIMARY KEY REFERENCES campaigns(id) ON DELETE CASCADE,
  combat TEXT NOT NULL,          -- JSON serializeCombatOrder
  enemies TEXT NOT NULL,         -- JSON SerializedEnemy[]
  saved_at TEXT NOT NULL
);
```

## Публичный API — сохранить

- `world-state.ts`: `upsertWorldChange(campaignSlug, change)`,
  `readWorldState(campaignSlug)` → `Map<string, string[]>`,
  `renderWorldState(campaignSlug)` → строка.
  - `readWorldState` возвращает **отрендеренные** строки `- text` или
    `- text (день N)` (формат как раньше в файле) — это важно для
    `campaign-context.ts`.
- `combat-store.ts`: `saveCombatState`, `loadCombatState`, `clearCombatState`,
  типы `SerializedEnemy`, `CombatSnapshot` — без изменений.
- **Upsert для миграции** (идемпотентно):
  `importWorldChanges(campaignId, rows: { category, text, day? }[])` и
  `importCombatSnapshot(campaignId, snapshot: CombatSnapshot)`.

## Правила реализации

- `upsertWorldChange`: сравнение по `text_norm` (lower + trim), как сейчас в
  world-state.ts:38-46 (без `- ` и без `(день N)`); существующая запись
  обновляет `day`, иначе insert. Category по умолчанию `"Изменения"`.
- `renderWorldState`/`renderCategories`: тот же вывод
  (`## Категория\n- строка`), пусто → `""`.
- `loadCombatState`: читать JSON из таблицы; невалидный/пустой бой → `null`,
  без выбросов (логика валидации как сейчас combat-store.ts:58-82).
- `clearCombatState`: `DELETE`, не падает при отсутствии записи.
- slug→id резолвинг через `campaigns` таблицу; нет кампании → пустой Map/null,
  без исключений.
- Не импортировать `campaignDataRoot`, `assertCampaignSlug`, `frontmatter`,
  `node:fs`/`node:path` (в combat-store сейчас `rmSync` — уйдёт с DELETE).
  Импорт `openCampaignDb` из `./sqlite-db.ts`. `serializeCombatOrder`/
  `deserializeCombatOrder` из `engine/combat.ts` остаются.
- Комментарии на русском.

## Приёмка

- `npm run typecheck`.
- `test/world-state.test.ts`: прежние сценарии (upsert по тексту, дедуп,
  обновление дня, рендер категорий, дефолтная категория).
- `test/combat-persist.test.ts`: save→load round-trip, clear, невалидные
  данные → null.
- `npm test` зелёный для этих файлов.
