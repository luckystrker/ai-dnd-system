# TTRPG Telegram bot (Go / ago)

Бот ведёт настольную ролевую игру в Telegram: соло-партия в личном чате или
группа до 6 игроков. Переписан с eve (TypeScript) на Go поверх
[ago](https://github.com/luckystrker/ago); схема SQLite та же, миграция данных
не нужна.

- **Long-poll**: отдельный webhook, Caddy и Node не нужны. Один Go-бинарь под
  systemd.
- **Модель**: OpenAI-совместимый Chat Completions; провайдер по умолчанию —
  OpenCode Go (`deepseek-v4.1-flash`).
- **Память**: одна SQLite-база `data/campaigns.db`. В группе сообщения
  склеиваются (2.5 с) и проходят дешёвый гейт «отвечать ли».

## Структура

```
cmd/
├── bot/            # точка входа: long-poll, repl, -check
├── eval/           # живой eval-прогон, калибровка судьи/Telegram, гейт
├── e2e/            # Telegram-апдейт через канал против fake Bot API
└── dump-sessions/  # переписка из event_streams и транскрипта
internal/
├── config/         # env → Config (дефолты и валидация)
├── llm/            # провайдеры, base URL, x-opencode-session
├── engine/         # d20, проверки, бой, награды (без LLM и SQLite)
├── store/          # SQLite: кампании, память, inbox, event_streams, FTS5
├── tools/          # 42 тула DM + 25 тулов летописца, авторизация
├── dm/             # промпт, память в хвосте, компакция, лимиты хода
├── chronicler/     # субагент закрытия дня
├── dayclose/       # детерминированная очередь закрытия дня
├── telegram/       # long-poll, durable-inbox, очередь, гейт, отправка
├── evalgame/       # кейсы, скореры, судья, гейт, отчёт
├── evalrun/        # исполнение кейса настоящим DM на временной SQLite
└── calibrate/      # офлайн-калибровка гейта и склейки Telegram
eval/
├── cases/*.jsonl   # 43 синтетических сценария
└── judge/          # калибровка судьи: хорошие/плохие ответы
deploy/             # systemd, backup, setup, ops-референс
```

## Быстрый старт

Нужен **Go 1.24+** (модуль объявляет `go 1.24`, образец — Go 1.27).

```bash
cp .env.example .env       # впишите LLM_API_KEY и TELEGRAM_BOT_TOKEN
go run ./cmd/bot           # auto: telegram при токене, иначе repl
go run ./cmd/bot -mode repl
go run ./cmd/bot -check    # проверить env/конфиг без сети
```

Проверки:

```bash
go vet ./...
go test ./...              # офлайн, ключи не нужны
go test -race ./...        # в CI (локально нужен cgo, здесь его нет)
go build ./...
go build -trimpath -ldflags "-X main.version=$(git rev-parse --short HEAD)" -o bin/ai-dnd ./cmd/bot
```

## Провайдеры

Все три — OpenAI-совместимый Chat Completions; `LLM_BASE_URL` и `LLM_MODEL`
переопределяют значения провайдера.

| `LLM_PROVIDER` | Base URL по умолчанию | Модель | Ключ |
|---|---|---|---|
| `opencode-go` (по умолчанию) | `https://opencode.ai/zen/go/v1` | `deepseek-v4.1-flash` | `LLM_API_KEY` / `OPENCODE_API_KEY` |
| `openrouter` | `https://openrouter.ai/api/v1` | `deepseek/deepseek-v4.1-flash` | `LLM_API_KEY` / `OPENROUTER_API_KEY` |
| `deepseek` | `https://api.deepseek.com` | `deepseek-flash` | `LLM_API_KEY` / `DEEPSEEK_API_KEY` |

OpenCode Go **требует** заголовок `x-opencode-session`: без него любой запрос
падает с `HTTP 400 MissingSessionID`. `internal/llm` ставит его сам — у каждого
Telegram-чата, летописца и судьи eval своя сессия (хеш chat id, не сам id).

Eval-судья по умолчанию — `muse-spark-1.3-contributor` через Responses API
того же провайдера; переопределяется `EVAL_JUDGE_PROVIDER` / `EVAL_JUDGE_MODEL`.

## Переменные окружения

Всё читает `internal/config`. Значения по умолчанию — из кода; шаблон —
`.env.example`.

| Переменная | По умолчанию | Назначение |
|---|---|---|
| `LLM_PROVIDER` | `opencode-go` | `openrouter`, `opencode-go`, `deepseek` |
| `LLM_API_KEY` | — | ключ провайдера (иначе переменная провайдера) |
| `LLM_BASE_URL` | значение провайдера | корень эндпоинта **без** `/chat/completions` |
| `LLM_MODEL` | значение провайдера | id модели |
| `MAX_TOOL_ROUNDS` | `12` | лимит раундов «модель → тулы» в одном ходу |
| `CAMPAIGN_DB_PATH` | `data/campaigns.db` | SQLite-база всей памяти и inbox |
| `CONTEXT_WINDOW_TOKENS` | `96000` (16000..512000) | фиксированное окно; компакция на 75% |
| `TURN_TIMEOUT_SECONDS` | `180` (1..3600) | дедлайн одного хода DM |
| `TOKEN_BUDGET` | `200000` | бюджет токенов одного хода |
| `DAYCLOSE_BACKFILL_MAX` | `2` | сколько пропущенных дней закрывать летописцем на старте; `-1` — не догонять |
| `DAYCLOSE_BACKFILL_DAYS` | `2` (1..30) | окно последних дней для летописца; старше — дайджест |
| `TELEGRAM_BOT_TOKEN` | — | без него запускается repl |
| `TELEGRAM_BOT_USERNAME` | — | username без `@`, для команд и упоминаний |
| `TELEGRAM_API_BASE_URL` | `https://api.telegram.org` | корень Bot API |
| `TELEGRAM_COALESCE_MS` | `2500` (1..60000) | окно склейки сообщений чата |
| `TELEGRAM_FOLLOW_UP_MINUTES` | `10` (1..1440) | окно «ответ на реплику DM» |
| `TELEGRAM_RATE_LIMIT_CHAT_PER_MIN` | `20` | сообщений чата в минуту |
| `TELEGRAM_RATE_LIMIT_USER_PER_MIN` | `10` | сообщений игрока в минуту |
| `TELEGRAM_MAX_UPDATE_BYTES` | `1048576` | предел входящего апдейта |
| `TELEGRAM_POLL_TIMEOUT_SECONDS` | `50` (1..300) | long-poll `getUpdates` |
| `TELEGRAM_MAX_ATTACHMENT_BYTES` | `26214400` | предел скачиваемого вложения |
| `RUNWARE_API_KEY` | — | без ключа `illustrate_scene` просит продолжать текстом |
| `RUNWARE_MODEL` | `runware:101@1` | модель генерации |
| `RUNWARE_WIDTH` / `RUNWARE_HEIGHT` | `1024` | размер картинки |
| `EVAL_JUDGE_PROVIDER` | `opencode-go` | провайдер судьи eval |
| `EVAL_JUDGE_MODEL` | `muse-spark-1.3-contributor` | модель судьи eval |

Устаревшие переменные TS/eve (`TELEGRAM_WEBHOOK_SECRET_TOKEN`, `PORT`, `HOST`,
`NITRO_*`) Go-боту не нужны и игнорируются до слайса 8.

## Команды бота

`/join` и `/kick` обрабатываются кодом без LLM; остальные — DM-агентом по
инструкциям.

| Команда | Кто | Что делает |
|---|---|---|
| `/newcampaign` | любой | опросник создания кампании и `save_campaign` |
| `/mycampaigns` | любой | список кампаний пользователя |
| `/startcampaign` | DM кампании | привязка кампании к чату/топику, запуск |
| `/join` | игрок | явное вступление в кампанию этого чата (код) |
| `/kick` | DM | убрать игрока (реплай или `@username`); персонаж и история остаются (код) |
| `/newchar` | игрок | создание персонажа |
| `/mychar` (`/sheet`, `/stats`, `/character`) | игрок | карточка персонажа |
| `/invite` | DM | пригласить игрока, пока в партии есть место |

Дополнительно:

- любой не-бот, который пишет в привязанном чате/топике активной кампании,
  автоматически регистрируется игроком (до 6) — без LLM;
- в группе LLM будится только на команду, `@упоминание`, реплай на бота,
  обращение «ДМ/мастер/ведущий», свой ход в бою или первый ответ на реплику
  DM в пределах `TELEGRAM_FOLLOW_UP_MINUTES`; остальные сообщения пишутся в
  транскрипт и видны модели в блоке памяти, но хода не запускают;
- в личке бот отвечает на любое сообщение.

Для групп выключите privacy mode в BotFather (`/setprivacy → Disable`) или
сделайте бота админом, иначе он не увидит переписку игроков.

## Eval и гейт качества

Набор — `eval/cases/*.jsonl` (43 кейса, `eval/README.md`). Офлайн (CI, без
ключей):

```bash
go test ./internal/evalgame/ ./internal/evalrun/   # скриптованные траектории
go run ./cmd/eval -check-only                       # состав набора
go run ./cmd/eval -telegram                         # калибровка гейта/склейки
```

Живой прогон (нужен `LLM_API_KEY`; стоит денег — 43 кейса, судья и летописец):

```bash
# один прогон набора
go run ./cmd/eval -n 1 -out eval-results/last.json
# повторы только проблемных кейсов (Pass@k / Pass^k)
go run ./cmd/eval -case rolls-05-narrate-per-tool-output -n 5 -out eval-results/rolls.json
# регрессионный гейт относительно прошлого прогона
go run ./cmd/eval -baseline eval-results/baseline.json -gate
```

Что делает раннер:

- собирает временную SQLite-кампанию: DM, партия (Дэн/Лиса), персонажи,
  привязка к чату, сессия говорящего с его ролью (`player` / `dm`), начало боя;
- прогоняет кейс через настоящего DM-агента (`internal/dm`) и настоящие тулы;
- исполняет `postconditions` по БД (например, `day_summary` закрытого дня
  вместе с очередью летописца);
- исполняет `fault`: результат тула подменяется ошибкой на N-м вызове;
- использует детерминированный rand; `rolls-05` идёт дважды — с натуральной
  20 и натуральной 1;
- считает Pass@k / Pass^k по повторам, пишет JSON и сводку по категориям,
  отдаёт совместимый `eval.RunResult` для `eval.Diff`;
- калибрует судью на 4 хороших и 4 плохих ответах (`eval/judge/calibration.jsonl`):
  good должен пройти, bad — провалиться.

Абсолютный гейт (`-gate`) — флаг, а не дефолт: пороги по категориям и
обязательные кейсы задаются `-min-pass`, `-category`, `-required`. Предложенные
значения — в `evalgame.ProposedGate()`; они ждут решения владельца.

## Деплой на VPS

Стек: bare metal + systemd, long-poll. Подробности — `deploy/README.md`.

```bash
sudo bash deploy/setup.sh          # Go, пользователь, клон, bin/ai-dnd, unit
sudo -u ai-dnd cp .env.example /opt/ai-dnd-system/.env
sudo -u ai-dnd nano /opt/ai-dnd-system/.env
sudo systemctl start ai-dnd
journalctl -u ai-dnd -f
```

Обновление и бэкапы:

```bash
cd /opt/ai-dnd-system && ./deploy.sh     # pull → go build → restart
sudo crontab -e                          # 0 3 * * * /opt/ai-dnd-system/deploy/backup.sh
```

`deploy/backup.sh` снимает SQLite через `sqlite3 .backup` (не копированием
файла: в WAL данные лежат в `-wal`), проверяет `integrity_check` и хранит 14
копий. Юнит: `KillSignal=SIGTERM`, `TimeoutStopSec=25` (graceful shutdown 15 с
+ запас), `ProtectSystem=strict` и запись только в `data/`.

Caddy/webhook и `ai-dnd-ts.service` — устаревший TS-путь, сохранён для отката
до слайса 8.

## Диагностика

```bash
go run ./cmd/dump-sessions                  # все чаты из event_streams + транскрипт
go run ./cmd/dump-sessions den              # по username/имени/user id
go run ./cmd/dump-sessions -7770001         # по chat id
go run ./cmd/e2e                            # канал против fake Bot API (офлайн)
go run ./cmd/e2e -live                      # то же с живой моделью
```

Логи — `journalctl -u ai-dnd -f`. `HTTP 400 MissingSessionID` означает, что
заголовок OpenCode Go потерялся (см. `internal/llm`). `SQLITE_BUSY` — запущен
второй процесс на той же базе.
