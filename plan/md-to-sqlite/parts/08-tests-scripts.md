# Часть 08 — тесты и вспомогательные скрипты

**Цель**: привести тесты и скрипты к SQLite-миру; удалить тесты MD-стора.
**Выполнять после мерджа частей 01–06** (части 01–05 уже переписали свои
тесты — здесь остальное).

## test/

- `test/helpers.ts`: добавить `tempDb(prefix)` — temp-файл БД + `cleanup`
  (аналог `tempDir`); установить `process.env.CAMPAIGN_DB_PATH` до первого
  обращения к сторам **в каждом тестовом файле** (handle ленивый, поэтому
  сработает). Сбрасывать `process.env.CAMPAIGN_DB_PATH` в `after()`.
- Удалить: `test/store.test.ts` (тесты MarkdownCampaignStore),
  `test/frontmatter.test.ts` (frontmatter удалён частью 06).
- `test/access.test.ts`, `test/npc.test.ts`, `test/locations.test.ts`,
  `test/factions.test.ts`: убрать `process.env.CAMPAIGN_STORE = "markdown"`,
  заменить на temp БД (части 02–03 могли уже это сделать — проверить).
- Остальные тесты (`journal`, `ledger`, `search`, `world-state`,
  `combat-persist`, `store-sqlite`, `session`, `dnd5e*`, ...) — проверить,
  что не ссылаются на MD-папки/`campaignDataRoot`; при необходимости
  перевести фикстуры.
- Добавить `test/migration.test.ts` (временный, живёт до прогона части 07):
  MD-фикстура (сгенерированные файлы в temp-папке) → запуск логики
  миграционного скрипта → проверка таблиц БД. После реальной миграции
  тест удаляется вместе со скриптом.

## scripts/

- `scripts/e2e-start.ts` — сейчас читает `campaign.md` напрямую
  (строки 40–54). Переписать: после webhook-POST в цикле опрашивать
  `campaignStore.getCampaign("testovyy-pohod")` (или SQL-запрос через
  `openCampaignDb`) до появления `status === "active"` + `boundChat`;
  проверки на `boundChat.chatId`/`messageThreadId` остаются.
- `scripts/smoke-campaigns.ts` — убрать MD-стор (`MarkdownCampaignStore`,
  ветку `CAMPAIGN_STORE`, строки 15, 308–309) и MD-прогоны; оставить
  SQLite-сценарии + журнал/NPC поверх БД.
- `package.json` — проверить `migrate:sqlite` (удаляется в части 07),
  `smoke` оставить.

## deploy/

- `deploy/backup.sh` — убрать tar `data/campaigns/`; бэкапить только
  `campaigns.db`. Перед копированием выполнить чекпоинт WAL:
  `sqlite3 data/campaigns.db "PRAGMA wal_checkpoint(TRUNCATE);"` (или
  копировать `.db` + `-wal` + `-shm` вместе). Выбрать чекпоинт.
- `deploy/README.md` — таблица бэкапов (строка ~104): убрать
  `data/campaigns/`, оставить `data/campaigns.db`.

## Приёмка

- `npm run typecheck`, `npm test` зелёные (без MD-фикстур и
  `CAMPAIGN_STORE`).
- grep по `test/`, `scripts/`, `deploy/`: нет `campaign.md`,
  `CAMPAIGN_STORE`, `campaignDataRoot`.
- `npm run smoke` зелёный (на temp-БД).
