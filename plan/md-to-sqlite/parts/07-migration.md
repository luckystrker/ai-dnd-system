# Часть 07 — миграция данных, прогон и полная зачистка

**Цель**: перенести все реальные данные из `data/campaigns/*.md` в
`data/campaigns.db`, затем **удалить** MD-данные и весь миграционный код.
Финальное состояние: в репозитории нет ни одного файла, читающего MD.

## 1. Скрипт миграции (временный)

Переписать `scripts/migrate-md-to-sqlite.ts` так, чтобы он был
**самодостаточным**: содержал собственные копии минимального парсера
frontmatter (split/serialize — из `frontmatter.ts`) и функций
`docToCampaign/docToCharacter/docToQuest/docToThreads/docToNpc/
docToLocation/docToFaction` (из удаляемых частей `store.ts`/`npc.ts`/...).
Никаких импортов из `agent/lib/campaigns/`, кроме `openCampaignDb`/
`campaignDbPath` (`sqlite-db.ts`) — остальной код мигрирует напрямую SQL'ом
через `openCampaignDb()` (таблицы к этому моменту создаются модулями при
запуске — но скрипт выполнит DDL сам: продублировать `CREATE TABLE IF NOT
EXISTS` для таблиц частей 01–04 в скрипте, это надёжнее).

Что мигрировать:

| Источник | Назначение |
|---|---|
| `<slug>/campaign.md` | `campaigns` + `campaign_members` + `description` |
| `<slug>/characters/*.md` | `characters` (+body → `background`) |
| `<slug>/quests/*.md` | `quests` |
| `<slug>/threads.md` | `open_threads` |
| `history/days/day-NNNN.md` | `campaign_days` + `transcript_entries` (frontmatter: day/date/note/summary/headline; body-строки `- ...` как есть, event_id из `<!-- evt:... -->`) |
| `history/summary.md` | `campaign_summary` (секции `## День N`) |
| `history/key-events.md` | `key_events` (day, permanent по `[важно]`, line, event_id) |
| `history/ledger.md` | `ledger_rows` (day, type по `found|spent`, line, event_id) |
| `history/world-state.md` | `world_changes` (категории, текст без `(день N)`, day) |
| `npcs/*.md` | `npcs` (frontmatter→профиль, body→memory) |
| `locations/*.md` | `locations` (body→description) |
| `factions/*.md` | `factions` (body→description) |
| `combat.md` | `combat_snapshot` |

Требования: **идемпотентность** (`INSERT OR IGNORE`/`ON CONFLICT DO UPDATE`
по первичным ключам — можно гонять повторно), отчёт-счётчики по каждой
сущности в stdout, понятные ошибки (не молчаливый пропуск битых файлов —
предупреждение с путём). Для кампании: `INSERT ... ON CONFLICT(id) DO UPDATE`
по всем полям (аналог текущего `upsertCampaign`).

## 2. Прогон на реальных данных (после мерджа частей 01–06 и 08)

1. Бэкап: скопировать `data/campaigns/` и `data/campaigns.db` (+`-wal`,
   `-shm`) в бэкап-каталог с датой.
2. `npm run migrate:sqlite` (проверить, что package.json указывает на
   переписанный скрипт).
3. Сверка: сравнить счётчики (7 кампаний в БД сейчас; сверить количество
   дней/событий/NPC/локаций по каждой кампании с числом файлов в
   `data/campaigns/`); точечно проверить контент через
   `node scripts/dump-sessions.ts` не нужно — лучше временным SQL-запросом
   (например, `node -e` с better-sqlite3) по 2–3 сущностям каждой кампании.
4. **Удалить `data/campaigns/` полностью** (бэкап остаётся).
5. **Удалить временный код**: `scripts/migrate-md-to-sqlite.ts` (и npm-скрипт
   `migrate:sqlite` в `package.json`, если он станет не нужен),
   `agent/lib/campaigns/frontmatter.ts`, `test/frontmatter.test.ts`,
   временный миграционный тест (если писали в части 08).
6. Финальная проверка grep по `agent/`, `scripts/`, `test/`:
   `\.md`, `frontmatter`, `CAMPAIGN_STORE`, `CAMPAIGN_DATA_DIR`,
   `campaignDataRoot`, `MarkdownCampaignStore|MarkdownNpcStore|...` —
   только ложные совпадения (например, `summary.md` в комментариях истории —
   их тоже вычистить). В `package.json` убрать `migrate:sqlite`.

## Приёмка

- Миграция прогнана на реальных данных, счётчики сошлись, выборочная сверка
  контента пройдена.
- `data/campaigns/` удалена; бот работает с БД (`npm run smoke` на реальной
  БД — осторожно, он создаёт тестовые данные; лучше прогнать smoke на
  временной копии БД через `CAMPAIGN_DB_PATH`).
- `npm run typecheck`, `npm test`, `npm run build` зелёные после зачистки.
- Git-статус: ни одного упоминания MD-чтения в коде; удаления закоммичены
  отдельным коммитом «удаление MD-слоя и миграционного кода».
