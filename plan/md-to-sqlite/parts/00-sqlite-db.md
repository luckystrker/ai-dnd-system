# Часть 00 — общий слой SQLite (`sqlite-db.ts`)

**Цель**: единственная точка открытия `campaigns.db` для всех новых модулей
памяти + рефакторинг существующего стора на неё.

## Файлы

- Создать `agent/lib/campaigns/sqlite-db.ts`.
- Поправить `agent/lib/campaigns/store-sqlite.ts` (только блок динамического
  require и открытие БД — логику не трогать).

## Контракт

`sqlite-db.ts` экспортирует:

```ts
/** Открывает SQLite-базу кампаний: WAL, foreign_keys=ON. Лениво. */
export function openCampaignDb(): BetterSqlite3.Database;
/** Путь к базе (копия campaignDbPath() из store.ts). */
export function campaignDbPath(): string;
```

Требования:

- Тот же приём динамического require `better-sqlite3` через `createRequire`
  (сейчас в `store-sqlite.ts:24-31`) — статический import ломает eve-сборку.
- Ленивый кэш handle: БД открывается при первом вызове, а не на импорте.
  Функции `openCampaignDb()` могут быть несколько, handle — один.
- Прагмы: `journal_mode = WAL`, `foreign_keys = ON`.
- `mkdirSync(dirname(dbPath), { recursive: true })` перед открытием.
- `campaignDbPath()`: `resolve(process.cwd(), process.env.CAMPAIGN_DB_PATH ?? "data/campaigns.db")`.
- Комментарии на русском, стиль как в `store-sqlite.ts`.

`store-sqlite.ts`: заменить `betterSqlite()`/`projectRequire`/`databaseCtor` и
конструктор на использование `openCampaignDb()` (импорт `campaignDbPath` там,
где он нужен). `campaignDbPath` остаётся экспортом `store.ts` до части 06 —
не дублировать: в 06 он переедет в `sqlite-db.ts` и `store.ts` реэкспортирует
или потребители переключатся. Пока `sqlite-db.ts` реализует его сам.

## Приёмка

- `npm run typecheck` зелёный.
- `npm test` — все существующие тесты зелёные (особенно `store-sqlite.test.ts`).
- Два подряд вызова `openCampaignDb()` возвращают один и тот же handle.

## Не делать

- Не трогать схему, миграции и методы `SqliteCampaignStore`.
- Не удалять ничего из `store.ts`.
