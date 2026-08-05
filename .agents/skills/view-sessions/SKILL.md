---
name: view-sessions
description: Dump and inspect user conversations with the TTRPG Telegram bot from local eve traces. Use when asked to show what a user/friend wrote to the bot, read bot replies, debug a conversation, or find who talked to the bot — e.g. "что писал dmkamensky", "покажи переписку", "посмотреть сессии". Triggers on scripts/dump-sessions.ts and .eve/traces.
---

# view-sessions

Просмотр переписок пользователей с ботом по локальным OTLP-трейсам eve
(`agent-service/.eve/traces/v1`). Каждая сессия (чат) — отдельный трейс с
полной историей промптов, включая `username`, `user_id` и `chat_id`.

Скрипт: `agent-service/scripts/dump-sessions.ts` (запускать из
`agent-service/`).

## Запуск

```bash
node scripts/dump-sessions.ts                 # все личные чаты (DM)
node scripts/dump-sessions.ts <фильтр>        # username / user_id / chat_id
node scripts/dump-sessions.ts --all           # все чаты, включая группы
```

## Как отвечать пользователю

1. Выполните скрипт без аргументов или с фильтром, который назвал пользователь.
2. Покажите результат компактно: для каждой сессии заголовок
   `==== <username> | chat_id <id> | private ====`, затем переписку.
3. Если сообщений нет или чат не найден — скрипт сам подскажет типы найденных
   чатов; попробуйте `--all` или уточните username у пользователя.

## Примечания

- Видны только сессии, записанные локально при `eve dev`. Если переписка
  шла, когда бот работал на деплое, локально её нет.
- Трейсы держатся ограниченно (~7 дней или `EVE_TRACES_MAX_AGE_MS`), старые
  сессии могут быть вычищены.
- В одном чате может быть несколько блоков — это разные сессии (окна трейсов)
  одного диалога; у них одинаковый chat_id, разные session id.
- Если кириллица в консоли кракозябрами: `chcp 65001` перед запуском.
- При фильтре по `--all` блоки групп помечены `supergroup` в заголовке.
