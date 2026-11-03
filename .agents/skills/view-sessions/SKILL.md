---
name: view-sessions
description: Dump and inspect player conversations with the TTRPG Telegram bot from the local SQLite database (event_streams + campaign transcripts). Use when asked to show what a user/friend wrote to the bot, read bot replies, debug a conversation, or find who talked to the bot — e.g. "что писал dmkamensky", "покажи переписку", "посмотреть сессии". Triggers on cmd/dump-sessions.
---

# view-sessions

Переписки пользователей с ботом читаются из SQLite-базы бота: durable-потоки
`event_streams` (полные ходы агента, включая модель и тулы) плюс транскрипт
кампаний (`transcript_entries`). Трейсов прежнего TS/eve-рантайма больше нет —
этот же способ работает и для истории, записанной до миграции, потому что база
одна и та же (`data/campaigns.db`).

Команда: `cmd/dump-sessions` (Go), запускать из корня репозитория.

## Запуск

```bash
export PATH=$HOME/.local/go/bin:$PATH

go run ./cmd/dump-sessions                    # все чаты
go run ./cmd/dump-sessions <фильтр>           # username / имя / user id / chat id
go run ./cmd/dump-sessions --all              # совместимость: и так все чаты
go run ./cmd/dump-sessions -db /opt/ai-dnd-system/data/campaigns.db   # другая база
```

Путь к базе берётся из `-db`, иначе из `CAMPAIGN_DB_PATH`, иначе
`data/campaigns.db`.

## Как отвечать пользователю

1. Выполните команду без аргументов или с фильтром, который назвал
   пользователь.
2. Покажите результат компактно: заголовок
   `==== кампания «Название» (slug) | chat_id <id> | <status> ====`, затем
   участники и переписку.
3. Если ничего не найдено — команда сама подскажет типы фильтров; попробуйте
   `--all` или уточните username у пользователя. Потоки без кампании (чат до
   создания кампании) выводятся отдельным блоком.

## Примечания

- Видны только чаты, которые бот реально видел: личка, группы и топики, где
  приходили сообщения. В группе с включённым privacy mode сообщения игроков
  боту не доходят и в историю не попадают.
- Durable-история чата ограничена ~600 событиями на поток (компакция), поэтому
  очень старые ходы могли быть свёрнуты; транскрипт кампании при этом остаётся.
- База в WAL: читать можно на работающем боте, но для точного снимка лучше
  `sqlite3 .backup` / `VACUUM INTO`, а не копирование файла.
- Если кириллица в консоли кракозябрами: `chcp 65001` (Windows) или
  `LANG=ru_RU.UTF-8`.
