# Часть 06 — удаление MD-стора из `store.ts`

**Цель**: убрать `MarkdownCampaignStore`, выбор стора по `CAMPAIGN_STORE` и
всю MD-инфраструктуру из `agent/lib/campaigns/store.ts` и `frontmatter.ts`.
После этой части чтение MD в `agent/` отсутствует полностью.

**Выполнять после мерджа частей 01–05** (они больше не импортируют
`campaignDataRoot`/`assertCampaignSlug`).

## Файлы

- `agent/lib/campaigns/store.ts` — удалить: класс `MarkdownCampaignStore`
  (строки 139–659), все `*ToFrontmatter`/`docTo*` функции (661–914),
  `campaignDataRoot()` (917–919), ветку markdown в `createCampaignStore`
  (930–935). Оставить: интерфейсы (`CampaignStore`, `NewCampaignInput`, ...),
  `slugify`, `campaignDbPath` (как реэкспорт из `sqlite-db.ts` или переезд —
  согласовать с частью 00), ленивый Proxy `campaignStore`
  (`createCampaignStore` теперь всегда возвращает `SqliteCampaignStore`).
- `agent/lib/campaigns/frontmatter.ts` — **удалить файл** (его копия живёт
  только во временном миграционном скрипте части 07).
- `agent/lib/campaigns/store-sqlite.ts` — поправить шапку-комментарий
  (строки 1–10): убрать упоминания MD-отката и `CAMPAIGN_STORE`.

## Правила

- `assertCampaignSlug` — проверить grep по репозиторию: если после частей
  01–05 не осталось потребителей — удалить. Если остался потребитель
  (например, валидация ввода) — оставить только валидацию, убрать упоминание
  «путей ФС» в комментарии.
- `import { SqliteCampaignStore }` в `store.ts` остаётся; циклическая
  зависимость (`store.ts` ↔ `store-sqlite.ts` через `slugify`) не меняется.
- Проверить grep: `campaignDataRoot`, `CAMPAIGN_STORE`, `MarkdownCampaignStore`,
  `frontmatter`, `existsSync|readFileSync|writeFileSync|appendFileSync` внутри
  `agent/lib/campaigns/` — не должно остаться (кроме `mkdirSync` в
  `sqlite-db.ts`/`store-sqlite.ts` для создания папки БД).
- В шапке `store.ts` обновить документацию: единое хранилище — SQLite.

## Приёмка

- `npm run typecheck`.
- `npm test` зелёный (тесты MD-стора к этому моменту переписаны частями
  01–05/08; `test/store.test.ts` и `test/frontmatter.test.ts` удаляются в
  части 08 — если они ещё есть и падают, удалить их здесь).
- `npm run build` успешен.
