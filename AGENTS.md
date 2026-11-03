# TTRPG Telegram bot

Бот ведёт D&D 5e-подобную игру в Telegram (соло или группа до 6). Реализация —
**Go + ago**, единственная боевая версия. До миграции бот был на eve
(TypeScript); слайс 8 удалил TS из репозитория, старый код доступен только в
истории git (`git log` до коммита «удалить TS и eve», ветка `ago-port`).

Реплики бота, комментарии в коде, `README.md` и коммиты — **по-русски**.
Строки для LLM (описания тулов, `outputSchema`) — по-английски.

## Команды

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

CI (`.github/workflows/ci.yml`, единственный job `go`): `gofmt -l`, `go vet`,
`go test`, `go test -race`, `go build`, сборка бинаря, офлайн-eval,
`cmd/eval -check-only`, `cmd/eval -telegram`. Живой eval в CI не запускается.

Гейт качества абсолютный (`internal/evalgame/gate.go`), а не «Go ≥ baseline на
eve»: TS-бот локально не собирался, baseline снять было нельзя (PLAN 30).

## Архитектура

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

## Соглашения и gotchas

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
  автора сообщения. Баг TS-версии «в группе HP/лут не сохраняются» не
  переносить.
- В группе склейка 2.5 с, follow-up 10 мин, лимиты 20/10 в минуту. Калибровка:
  `go run ./cmd/eval -telegram`; значения не менять без решения владельца.
- Durable-история чата ограничена 600 событиями; память кампании — в SQLite,
  не в истории. `Resume` не используется: оборванный ход срезается до
  последнего чистого `ModelResponse`.
- SQLite в WAL: бэкап только `sqlite3 .backup` / `VACUUM INTO`, не копией
  файла. `data/` — единственный writable путь systemd-юнита.
- `internal/store/testdata/schema_ts.sql` — эталон схемы TS-версии для
  golden-теста (`schema_test.go`): доказывает, что Go открывает прод-базу без
  миграции. Файл нужен Go-тестам, хотя и назван по историческому источнику.
- `plan/` и `docs/` — история проекта (в том числе TS-эпохи). Актуальные
  документы — `README.md`, `AGENTS.md`, `deploy/README.md`, `eval/README.md`,
  `plan/ago-migration/PLAN.md`.
