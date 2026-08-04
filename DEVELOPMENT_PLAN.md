# Development Plan: TTRPG Telegram Bot

## Цель

Telegram-бот — Dungeon Master для настольной ролевой игры (упрощённые правила
D&D 5e). Форматы:

- **Соло**: личный чат с ботом, один игрок.
- **Группа**: групповой чат, до 6 игроков.

## Архитектура

Максимально простая: один eve-агент + набор скиллов. Никакого веб-фронтенда,
отдельного бэкенда или БД на текущем этапе.

```
agent-service/
├── agent/
│   ├── agent.ts              # каркас на eve: defineAgent + LLM (OpenRouter)
│   ├── instructions.md       # промпт DM: соло / группа до 6
│   ├── channels/telegram.ts  # Telegram webhook-канал
│   ├── tools/                # игровые скиллы
│   │   ├── roll_dice.ts      # броски костей
│   │   ├── skill_check.ts    # проверки характеристик (d20)
│   │   ├── combat.ts         # пошаговый бой: HP/AC врагов
│   │   └── initiative.ts     # очередь ходов
│   └── lib/
│       ├── engine/dnd5e.ts   # правила бросков и проверок
│       └── memory.ts         # заготовка памяти
```

## Состояние игры

- Вся текущая партия (сцена, состав партии, враги, журнал) живёт в
  per-session состоянии eve (`defineState`) — переживает перезапуски, не требует БД.
- Партия ограничена 6 игроками (`MAX_PARTY` в `lib/memory.ts`).
- Личный чат = соло-сессия; групповая сессия общая для всех участников чата.

## Память (заготовка)

`lib/memory.ts` содержит:

- `gameState` — durable-состояние текущей игры через `defineState`.
- `MemoryStore` — интерфейс долговременной памяти (`remember` / `recall`) с
  заглушками. TODO: персистентное хранилище (например Postgres + pgvector),
  чтобы кампания переживала сессии и recall работал по истории.

## Запуск

```bash
cd agent-service
npm install
npm run dev -- --no-ui
```

См. `agent-service/README.md` — регистрация webhook, переменные окружения.

## Удалено при упрощении

- `frontend/` — React-UI (цель — Telegram-бот)
- `backend/` — FastAPI/LangGraph-бэкенд и его pytest-тесты
- `docs/superpowers/` — планы под старую веб-архитектуру
- `docker-compose.yml`, Postgres, миграции SQL, room/NDJSON-каналы
- vitest-тесты в `agent-service`

## Roadmap

1. **M1 (готово)**: каркас eve, Telegram-канал, скиллы бросков/проверок,
   пошаговый бой, инициатива, заготовка памяти.
2. **M2**: персонажи игроков (создание, статистики) в состоянии партии.
3. **M3**: долговременная память (Postgres + pgvector), recall по истории.
4. **M4**: NPC-профили и сцены; расширение правил по мере необходимости.
