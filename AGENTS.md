# TTRPG Telegram bot

Бот ведёт D&D 5e-подобную игру в Telegram (соло или группа до 6). Миграция с
eve (TypeScript) на **Go + ago** идёт веткой `ago-port`: слайсы 0–7 — Go,
слайс 8 — удаление TS и мерж в `main`. Ниже явно помечено, что относится к
Go-версии (боевая), а что — к уходящему TS.

Реплики бота, комментарии в коде, `README.md` и коммиты — **по-русски**.
Строки для LLM (описания тулов, `outputSchema`) — по-английски.

## Команды

### Go (боевая версия)

```bash
export PATH=$HOME/.local/go/bin:$PATH   # в этой песочнице Go 1.27

go run ./cmd/bot -mode auto        # telegram при TELEGRAM_BOT_TOKEN, иначе repl
go run ./cmd/bot -mode repl        # локальный чат без сети
go run ./cmd/bot -check            # проверить env/конфиг без сети

go vet ./...
go test ./...                      # офлайн, ключи не нужны
go test -race ./...                # только в CI (локально нет cgo)
go build ./...
go build -trimpath -ldflags "-X main.version=$(git rev-parse --short HEAD)" -o bin/ai-dnd ./cmd/bot

# eval
go test ./internal/evalgame/ ./internal/evalrun/ ./internal/calibrate/
go run ./cmd/eval -check-only      # состав набора, без модели
go run ./cmd/eval -telegram        # калибровка гейта и склейки (офлайн)
go run ./cmd/eval -n 1 -out eval-results/last.json            # живой, нужен ключ
go run ./cmd/eval -case <name> -n 5                            # повторы кейса
go run ./cmd/eval -n 3 -case <required>,... -out eval-results/required.json
go run ./cmd/eval -from <json> -required <repeats.json> -gate   # гейт (PLAN 46)
go run ./cmd/eval -baseline <json> -gate                       # регрессионный гейт

# e2e и диагностика
go run ./cmd/e2e                   # апдейт через канал против fake Bot API
go run ./cmd/e2e -live             # то же с живой моделью
go run ./cmd/dump-sessions [username|chat_id]
```

CI (`.github/workflows/ci.yml`, job `go`): `gofmt -l`, `go vet`, `go test`,
`go test -race`, `go build`, сборка бинаря, офлайн-eval, `cmd/eval -check-only`,
`cmd/eval -telegram`. Живой eval в CI не запускается.

### TS (уходящая версия, до слайса 8)

`npm run dev -- --no-ui`, `npm run poll`, `npm run typecheck`, `npm test`,
`npm run build`, `npm run smoke`. Локально TS-бот не собирается
(`better-sqlite3`, нет `make`); baseline на eve снять нельзя — поэтому гейт
качества абсолютный (`internal/evalgame/gate.go`), а не «Go ≥ baseline».

## Архитектура Go

- `cmd/bot` — точка входа: `config.Load`, провайдер, `store.Open`, очередь
  закрытия дня, режимы `telegram` / `repl`. По SIGTERM канал перестаёт
  опрашивать Telegram, ждёт идущие ходы до 15 с, закрывает очередь и базу.
- `cmd/eval` — живой прогон набора + гейт + калибровка судьи/Telegram.
- `cmd/e2e` — Telegram-апдейт через настоящий канал против fake Bot API.
- `cmd/dump-sessions` — переписка из `event_streams` и транскрипта.
- `internal/config` — все env, дефолты и валидация; единственное место, где
  читается окружение (кроме `RUNWARE_*` в `internal/tools`).
- `internal/llm` — таблица провайдеров, base URL, id моделей, ключи и
  обязательный для OpenCode Go заголовок `x-opencode-session` (хеш chat id).
- `internal/engine` — чистая механика (d20, проверки, бой, награды); только
  здесь живут правила бросков.
- `internal/store` — SQLite (`modernc.org/sqlite`, pure Go): кампании,
  персонажи, квесты, нити, транскрипт, саммари, NPC/локации/фракции,
  world-state, лут, бой, durable-inbox Telegram и `event_streams`, FTS5-поиск.
  Один источник истины; `gameState` удалён.
- `internal/tools` — 43 тула DM и 25 летописца из одной реализации; кампания
  и говорящий приходят через DI (`aginject`), не из аргументов модели.
- `internal/dm` — DM-агент: `instructions.md` через `embed`, блок памяти в
  хвосте каждого LLM-вызова, компакция, лимит раундов, бюджет токенов,
  транскрипт.
- `internal/chronicler` / `internal/dayclose` — субагент закрытия дня и
  детерминированная очередь: `advance_day` только ставит задачу, fallback
  пишет дайджест, `ScanGaps` догоняет пропуски после рестарта.
- `internal/telegram` — long-poll, durable-inbox с дедупом по `update_id`,
  очередь чатов со склейкой, гейт «отвечать ли», разметка/разбивка, Runware.
- `internal/evalgame` — кейсы, механические скореры, судья, калибровка, гейт
  и отчёт. `internal/evalrun` исполняет кейс настоящим DM на временной SQLite.

## Соглашения и gotchas (Go)

- `LLM_BASE_URL` — корень эндпоинта (`https://host/v1`), **не**
  `/chat/completions`: ago дописывает путь сам и явно отклоняет такой URL.
- OpenCode Go: `https://opencode.ai/zen/go/v1` + заголовок
  `x-opencode-session`. Без заголовка — `HTTP 400 MissingSessionID`. Каталог
  моделей на `/inference/go/openai/v1` отдаёт 404, хотя chat completions там
  работает; дефолт — `zen/go/v1`.
- `MAX_TOOL_ROUNDS=12`, окно 96k, компакция на 75% — стартовые значения;
  у летописца свой `CHRONICLER_MAX_TOOL_ROUNDS=24` (PLAN 44);
  `ErrMaxToolRounds` даёт игроку нейтральный ответ, диагностика — в лог,
  записи тулов не откатываются.
- `advance_day` доступен любому участнику (операция памяти); роль `dm` только
  у `start_campaign` / `finish_campaign` / `invite_member`.
- Вехи внутри дня не запускают летописца: DM пишет их `append_key_event`.
- Граница сессии — по паузе в чате (`SESSION_GAP_HOURS`, по умолчанию 20 ч),
  время последнего сообщения — в `chat_activity` SQLite, не в памяти процесса.
  Маркер «Начало новой сессии» в блоке памяти включает recap; без маркера
  recap нет (PLAN 39).
- `rollQuoteMW` дописывает недостающие компоненты цитаты броска в бою и в
  живом eval (продуктовая гарантия, PLAN 40); счётчик срабатываний — в логе
  `dm: roll quote appended` и в отчёте `cmd/eval`.
- `ask_player` — структурный сигнал «ход ждёт решения другого PC»; по нему
  код дописывает «<имя>, что делаешь?», если ответ не кончился вопросом.
  Счётчик — `prompt_appends` / `prompt_characters` в отчёте `cmd/eval`
  (PLAN 41). Без вызова тула код не вмешивается.
- Право на запись в память даёт привязка сессии к чату кампании, а не роль
  автора сообщения. Баг TS «в группе HP/лут не сохраняются» не переносить.
- В группе склейка 2.5 с, follow-up 10 мин, лимиты 20/10 в минуту. Калибровка:
  `go run ./cmd/eval -telegram`; значения не менять без решения владельца.
- Durable-история чата ограничена 600 событиями; память кампании — в SQLite,
  не в истории. `Resume` не используется: оборванный ход срезается до
  последнего чистого `ModelResponse`.
- SQLite в WAL: бэкап только `sqlite3 .backup` / `VACUUM INTO`, не копией
  файла. `data/` — единственный writable путь systemd-юнита.
- `.eve/`, `.output/`, `node_modules/` — остатки TS, не трогать до слайса 8.

## TS (уходящее, до слайса 8)

- `agent/agent.ts` — eve-агент, `agent/instructions.md` — промпт,
  `agent/tools/` — 42 тула, `agent/subagents/chronicler/` — летописец.
- `agent/lib/engine/dnd5e.ts` — правила; `agent/lib/campaigns/` — SQLite.
- `scripts/e2e-*.ts`, `scripts/dump-sessions.ts`, `scripts/smoke-campaigns.ts` —
  заменены Go-командами (`cmd/e2e`, `cmd/dump-sessions`) и `go test`.
- `deploy/systemd/ai-dnd-ts.service`, `deploy/start-polling-ts.sh`,
  `deploy/caddy/` — старый путь деплоя, помечен устаревшим. Удаление — слайс 8.
- TS-код в слайсе 7 не меняется.

## eve framework (TS, историческое)

Если всё же правится TS: читать `node_modules/eve/docs/`; интеграции —
`eve registry search/list/view/add`. Для Go-версии это неактуально.
