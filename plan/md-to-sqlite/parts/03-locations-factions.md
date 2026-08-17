# Часть 03 — локации и фракции на SQLite

**Цель**: переписать `agent/lib/campaigns/locations.ts` и
`agent/lib/campaigns/factions.ts` на SQLite, сохранив публичные API и
синглтоны `locationStore`/`factionStore`.

## Файлы

- `agent/lib/campaigns/locations.ts` — полная переработка.
- `agent/lib/campaigns/factions.ts` — полная переработка.
- `test/locations.test.ts`, `test/factions.test.ts` — переписать на SQLite.

## Таблицы (DDL в своих модулях, при первом открытии БД)

```sql
CREATE TABLE IF NOT EXISTS locations (
  id TEXT PRIMARY KEY,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  slug TEXT NOT NULL,
  name TEXT NOT NULL,
  description TEXT,
  connections TEXT NOT NULL DEFAULT '[]', -- JSON LocationConnection[]
  discovered_day INTEGER,
  visited_days TEXT NOT NULL DEFAULT '[]', -- JSON number[]
  current INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT,
  UNIQUE (campaign_id, slug)
);

CREATE TABLE IF NOT EXISTS factions (
  id TEXT PRIMARY KEY,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  slug TEXT NOT NULL,
  name TEXT NOT NULL,
  description TEXT,
  standing INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT,
  UNIQUE (campaign_id, slug)
);
```

## Публичный API — сохранить

- `locations.ts`: класс → `SqliteLocationStore` (конструктор без аргументов),
  методы `upsertLocation`, `setCurrent`, `markVisited`, `getLocation`,
  `currentLocation`, `listLocations`; синглтон `locationStore` (ленивый).
- `factions.ts`: класс → `SqliteFactionStore`, методы `upsertFaction`,
  `adjustStanding`, `getFaction`, `listFactions`; синглтон `factionStore`
  (ленивый). `clampStanding` сохранить.
- **Upsert для миграции** (идемпотентно, `INSERT OR REPLACE` по id):
  `importLocation(location: Location): void` и
  `importFaction(faction: Faction): void`.

## Правила реализации

- `findCampaign` — как сейчас, через `campaignStore.getCampaign`.
- `setCurrent` в SQLite: один `UPDATE locations SET current = 0 WHERE
  campaign_id = ?` + `UPDATE ... SET current = 1 WHERE id = ?` (в транзакции
  не обязательно, но оба стейтмента подряд).
- `markVisited`: добавить день в `visited_days` JSON без дубликатов, сортировка.
- `description` — колонка (не тело файла): `docToLocation` брал body — теперь
  колонку.
- uniqueSlug — `SELECT 1` по (campaign_id, slug).
- **Ленивость** как в части 02.
- Не импортировать `campaignDataRoot`, `assertCampaignSlug`, `frontmatter`,
  `node:fs`/`node:path`. Импорт `openCampaignDb` из `./sqlite-db.ts`.
- Комментарии на русском.

## Приёмка

- `npm run typecheck`.
- `test/locations.test.ts`, `test/factions.test.ts`: прежние сценарии
  (upsert по имени, current-флаг один на кампанию, markVisited без дублей,
  connections, adjustStanding с клампингом -5..+5) на SQLite-фикстурах.
  Убрать `process.env.CAMPAIGN_STORE = "markdown"` (строки 13 в обоих файлах).
- `npm test` зелёный для этих файлов.
