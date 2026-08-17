# Часть 09 — документация, env и тексты тулов

**Цель**: убрать все упоминания MD-хранения из документации, `.env.example`,
шаблонов ответов и `description` тулов. Можно делать параллельно с частями
01–06 (тексты править после мерджа 01–06, чтобы не конфликтовать).

## README.md

- Строка 31: дерево проекта — `campaigns/` заменить на `campaigns.db` с
  описанием «вся память кампаний: SQLite».
- Строки 41–46: переписать блок хранилища: одна база `data/campaigns.db`
  (`CAMPAIGN_DB_PATH`); убрать `CAMPAIGN_STORE=markdown` и
  `CAMPAIGN_DATA_DIR`.
- Строка 83: удалить строку `CAMPAIGN_STORE` из таблицы env-переменных.
- Проверить остальные упоминания «MD»/«markdown» по файлу.

## AGENTS.md

- Строки 35–41: переписать абзац «Storage split»: всё (кампании, участники,
  персонажи, квесты, нити, журнал, NPC, локации, фракции, world-state,
  combat) живёт в SQLite (`data/campaigns.db`, `CAMPAIGN_DB_PATH`).
  `CAMPAIGN_STORE` и MD-слой не упоминать. Команды `npm run migrate:sqlite`
  — убрать из списка после части 07 (пока оставить с пометкой «временная»).

## .env.example

- Строки 32–38: оставить `CAMPAIGN_DB_PATH`, удалить `CAMPAIGN_STORE` и
  `CAMPAIGN_DATA_DIR`, поправить комментарии.

## docs/memory-roadmap.md

- Пройти по всем упоминаниям «всегда-MD» и путям `data/campaigns/<slug>/...`
  (строки ~36–37, 81, 107–108, 127, 134): переписать секции хранения на
  SQLite-таблицы (по именам из частей 01–04). История решений («был выбран
  MD по паттерну NPC») — оставить как исторический контекст или пометить
  «устарело», но пути/статус хранения обновить на актуальный.

## agent/instructions.md

- Строки ~181–183 и любые упоминания файлов памяти: заменить описания
  («файл транскрипта» → «транскрипт дня», «world-state файл» → «состояние
  мира» и т.п.), не меняя смысл инструкций DM.

## Тексты тулов (`agent/tools/`)

Обновить `description`/тексты ответов, упоминающие файлы/пути:
- `append_key_event.ts` (~строка 10)
- `append_campaign_summary.ts` (~строки 10, 28)
- `update_day_summary.ts` («в файл дня» → «в саммари дня»)
- `append_thread.ts` (~строка 10)
- `list_world_state.ts` (строки ~10–12: `history/world-state.md` → «состояние
  мира»)
- `record_world_change.ts` (~строка 10)
- `search_memory.ts` (если часть 05 ещё не поправила)
- Проверить grep `\.md` по `agent/tools/` и `agent/subagents/` целиком.

**Правило**: LLM-видимые тексты на английском — просто убрать упоминания
файлов, смысл сохранить.

## Приёмка

- grep по `README.md`, `AGENTS.md`, `.env.example`, `docs/`,
  `agent/instructions.md`, `agent/tools/`, `agent/subagents/`:
  нет `CAMPAIGN_STORE`, `CAMPAIGN_DATA_DIR`, `campaignDataRoot`, актуальных
  путей `data/campaigns/...md` (исторические упоминания в `docs/` допустимы,
  но помечены «устарело»).
- `npm run typecheck` (если правились .ts).
