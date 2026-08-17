# Миграция памяти кампаний из MD в SQLite

Цель: убрать **все** фоллбеки на `.md`-файлы. Вся память кампаний живёт в
одной SQLite-базе `data/campaigns.db`. После миграции и прогона на реальных
данных **кода для чтения MD в репозитории не остаётся вообще** — ни в `agent/`,
ни в `scripts/`, ни в `test/`. Папка `data/campaigns/` удаляется.

## Текущее состояние

Уже в SQLite (`agent/lib/campaigns/store-sqlite.ts`): `campaigns`,
`campaign_members`, `characters`, `quests`, `open_threads`.

Ещё в MD (пишутся при любом сторе через `campaignDataRoot()`):

| Данные | Файлы | Модуль |
|---|---|---|
| Транскрипты дней + note/summary/headline | `history/days/day-NNNN.md` | `journal.ts` |
| Хроника кампании | `history/summary.md` | `journal.ts` |
| Ключевые события | `history/key-events.md` | `journal.ts` |
| Лут-журнал | `history/ledger.md` | `journal.ts` |
| Состояние мира | `history/world-state.md` | `world-state.ts` |
| NPC (профиль + память) | `npcs/<slug>.md` | `npc.ts` (`MarkdownNpcStore`) |
| Локации | `locations/<slug>.md` | `locations.ts` (`MarkdownLocationStore`) |
| Фракции | `factions/<slug>.md` | `factions.ts` (`MarkdownFactionStore`) |
| Снимок боя | `combat.md` | `combat-store.ts` |
| Keyword-поиск | обход всех `.md` | `search.ts` |

Плюс полноценный MD-стор кампаний (`MarkdownCampaignStore` в `store.ts`,
включается `CAMPAIGN_STORE=markdown`).

## Ключевые решения (обязательны для всех частей)

1. **Каждая часть владеет своими таблицами.** DDL выполняется в модуле части
   (`CREATE TABLE IF NOT EXISTS`) при первом открытии БД. Общий файл — только
   `sqlite-db.ts` (часть 00). Это позволяет параллелить части без конфликтов.
2. **Публичные сигнатуры сохраняются.** Тулы, хуки и `campaign-context.ts`
   не меняют вызовы: аргументы `campaignSlug`/`campaignIdOrSlug` остаются.
   Внутри slug→id резолвится запросом `SELECT id FROM campaigns WHERE slug = ?`
   (id нужен для FK).
3. **Формат выдачи сохраняется байт-в-байт.** Строки транскрипта
   (`- [HH:MM] **Игрок @x**: ... <!-- evt:... -->`), key-events
   (`- [важно] **День N**: ...`), ledger (`- [День N] found: ...`),
   world-state (`## Категория\n- текст (день N)`) рендерятся из таблиц так же,
   как раньше читались из файлов. В таблицах хранятся отрендеренные строки
   (line) + структурированные колонки для фильтров.
4. **Дедуп по eventId**: `UNIQUE (campaign_id, event_id)` +
   `INSERT OR IGNORE`. Маркер `evt:...` вытаскивается из хвоста строки
   `<!-- evt:... -->`.
5. **Ленивость.** Модули не открывают БД на импорте (eve-снапшот компиляции
   падает при открытии better-sqlite3). Handle открывается при первом вызове;
   синглтоны `npcStore`/`locationStore`/`factionStore` — ленивые (паттерн
   Proxy из `campaignStore` или ленивое поле).
6. **Миграционный скрипт — временный и самодостаточный** (часть 07): содержит
   собственные копии парсеров frontmatter/MD. После успешного прогона на
   реальных данных скрипт удаляется вместе с `frontmatter.ts` и `data/campaigns/`.
7. Части 01–05 **не импортируют** `campaignDataRoot` и `assertCampaignSlug`
   (они удаляются в части 06). `slugify`, `StoreError`, типы — остаются.

## Состав частей

| # | Часть | Файлы | Зависит от |
|---|---|---|---|
| 00 | Общий слой БД | `agent/lib/campaigns/sqlite-db.ts` (+ рефакторинг `store-sqlite.ts`) | — |
| 01 | Журнал (дни, транскрипт, summary, события, ledger) | `journal.ts` | 00 |
| 02 | NPC | `npc.ts` | 00 |
| 03 | Локации + фракции | `locations.ts`, `factions.ts` | 00 |
| 04 | World-state + бой | `world-state.ts`, `combat-store.ts` | 00 |
| 05 | Поиск | `search.ts` | 00 |
| 06 | Удаление MD-стора | `store.ts`, `frontmatter.ts` | 01–05 |
| 07 | Миграция данных + прогон + зачистка | `scripts/migrate-md-to-sqlite.ts` (временный) | 01–06 |
| 08 | Тесты и скрипты | `test/*`, `scripts/e2e-start.ts`, `scripts/smoke-campaigns.ts`, `deploy/backup.sh`, `deploy/README.md` | 01–06 |
| 09 | Документация и env | `README.md`, `AGENTS.md`, `.env.example`, `docs/memory-roadmap.md`, тексты тулов, `agent/instructions.md` | — |

## Порядок исполнения

1. **00** — сделать первой (общий контракт для всех).
2. **01, 02, 03, 04, 05** — параллельно, независимы друг от друга (разные файлы).
3. **06** — после мерджа 01–05.
4. **09** — в любой момент, параллельно.
5. **08** — после мерджа 01–06.
6. **07** — написать после мерджа 01–06; прогнать на реальных данных:
   бэкап → миграция → сверка счётчиков и точечная проверка данных → удаление
   `data/campaigns/` → удаление временного скрипта и `frontmatter.ts`.

## Верификация (после каждого этапа)

- `npm run typecheck`
- `npm test` (файлы с тестами переписываются в части 08; до этого части 01–05
  должны обновить свои тесты сразу — см. раздел «Приёмка» в каждой части)
- `npm run smoke`
- `npm run build` (better-sqlite3 остаётся в `build.externalDependencies`)

## Риски и границы

- **Хуки и тулы не меняются** (API сохраняется): `hooks/transcript.ts`,
  `hooks/day-digest.ts`, `hooks/combat-autosave.ts`, `lib/hydrate.ts`,
  все тулы в `agent/tools/`, включая реэкспорты chronicler-субагента.
- `campaign-context.ts` (`agent/instructions/campaign-context.ts`) — вызывающий
  код не меняется, только модули под ним.
- WAL-режим: в `deploy/backup.sh` перед копированием БД выполнять чекпоинт
  (`PRAGMA wal_checkpoint(TRUNCATE)`) или бэкапить все три файла
  (`campaigns.db`, `-wal`, `-shm`).
- `.eve/` и `scripts/dump-sessions.ts` не затрагиваются.
- `test/frontmatter.test.ts` и `test/store.test.ts` (MD-стор) удаляются.
- В финальном состоянии grep по `\.md`/`frontmatter`/`CAMPAIGN_STORE`/
  `CAMPAIGN_DATA_DIR`/`campaignDataRoot`/`Markdown*` в `agent/`, `scripts/`,
  `test/` должен давать пусто (допустимы только исторические упоминания в
  `docs/` и `plan/`).
