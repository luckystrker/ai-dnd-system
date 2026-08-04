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

## Память кампании

Долговременная память живёт в папке кампании (MD-файлы за интерфейсами сторов):

- `history/days/day-NNNN.md` — транскрипт игрового дня: пишет хук
  `agent/hooks/transcript.ts` (message.received / message.completed /
  action.result), дедупликация по `meta.id` события.
- `history/summary.md`, `history/key-events.md`, саммари дня — пишет субагент
  `chronicler` (`agent/subagents/chronicler/`) на границах игровых дней и
  сюжетных вехах; хранилище — `agent/lib/campaigns/journal.ts`.
- `npcs/<slug>.md` — профиль, отношения и память NPC (`agent/lib/campaigns/npc.ts`).
- `characters/<slug>.md` — листы персонажей с динамическим состоянием
  (HP, инвентарь, золото, XP, локация).
- Подгрузка в контекст: `agent/instructions/campaign-context.ts` (defineDynamic
  на turn.started) + sliding window через встроенный compaction eve
  (окно контекста зависит от длины кампании: short/medium/long).

`lib/memory.ts` хранит только per-session состояние игры (`gameState`).

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
2. **M2 (готово)**: персонажи игроков (создание, статистики) в состоянии партии,
   кампании, участники, привязка к чату.
3. **M3 (готово)**: долговременная память кампании на файлах: транскрипт по
   игровым дням, саммари и ключевые события (субагент-летописец), sliding
   window через compaction, динамическая подгрузка памяти в контекст.
4. **M4 (готово)**: NPC-профили с собственной памятью и отношениями; динамическое
   состояние персонажей.
5. **Далее**: адаптер хранилища для деплоя (Vercel: эфемерная ФС — нужен
   S3/KV/Postgres за интерфейсами сторов); расширение правил по мере необходимости.
