# deploy/ — ops-референс и инфраструктура VPS

Стек: **bare metal + systemd, long-poll**. Один Go-бинарь, без Caddy, webhook и
Node. Старый TS-путь (eve + Nitro + Caddy) сохранён до слайса 8 — см. раздел
«Откат на TS».

## Что где лежит

| Файл | Назначение |
|------|------------|
| `setup.sh` | Первичная установка на свежем VPS (run-once): Go, пользователь, клон, сборка `bin/ai-dnd`, systemd-юнит. |
| `backup.sh` | Бэкап `data/campaigns.db` через `sqlite3 .backup` (целостный снимок на живой WAL-базе), проверка `integrity_check`, ротация 14 копий. Готов к cron. |
| `../deploy.sh` | Обновление: `git pull → go build → restart + проверка`. `--ts` — старый npm-путь. |
| `systemd/ai-dnd.service` | Боевой Go-юнит: `EnvironmentFile=.env`, `Restart=on-failure`, graceful shutdown 15 с + запас, харденинг. |
| `systemd/ai-dnd-ts.service` | **Устаревает.** Старый TS/eve-юнит для отката до слайса 8. |
| `start-polling.sh` | Ручной запуск Go-бинаря (long-poll встроен): замена двум tmux-сессиям. |
| `start-polling-ts.sh` | **Устаревает.** TS long-poll через tmux (до слайса 8). |
| `caddy/Caddyfile` | **Устаревает.** Reverse proxy + авто-TLS для webhook. Go-боту не нужен; удаление — слайс 8. |

## Топология (Go)

```
Telegram ──long-poll getUpdates──▶ ai-dnd (Go) [systemd: ai-dnd.service]
                                     │
                                     ├── /opt/ai-dnd-system/bin/ai-dnd  (бинарь)
                                     ├── /opt/ai-dnd-system/data/       (SQLite — ПЕРСИСТЕНТНО)
                                     │     ├── campaigns.db (+ -wal, -shm)
                                     │     └── .skills/                 (материализуются при старте)
                                     └── /opt/ai-dnd-system/.env        (секреты, читает systemd)
```

Один процесс, SQLite и in-memory rate-limiter. Горизонтальное масштабирование
не предусмотрено: один бот на одном VPS.

## Типовые операции

### Обновить бота

```bash
cd /opt/ai-dnd-system
./deploy.sh                 # с преддеплойным бэкапом
./deploy.sh --skip-backup   # быстро, без бэкапа
```

Скрипт пересобирает `bin/ai-dnd`, перезапускает юнит и ждёт, пока процесс
стабильно живёт после рестарта. У Go-бота нет HTTP-порта: health-check — это
`systemctl is-active` + отсутствие немедленного падения. Логи — journald.

### Откатить версию

```bash
cd /opt/ai-dnd-system
sudo -u ai-dnd git log --oneline -10
sudo -u ai-dnd git checkout <commit-sha>
./deploy.sh --skip-backup
```

### Восстановить из бэкапа

```bash
sudo systemctl stop ai-dnd
BACKUP=/opt/ai-dnd-system/data/backups/20260101T030000Z

# SQLite (вся память кампаний — журнал, NPC, локации, world-state и т.п.)
sqlite3 "$BACKUP/campaigns.db" "PRAGMA integrity_check;"
cp "$BACKUP/campaigns.db" /opt/ai-dnd-system/data/campaigns.db
rm -f /opt/ai-dnd-system/data/campaigns.db-wal /opt/ai-dnd-system/data/campaigns.db-shm

sudo -u ai-dnd chown -R ai-dnd:ai-dnd /opt/ai-dnd-system/data
sudo systemctl start ai-dnd
```

### Откат на TS (до слайса 8)

```bash
cd /opt/ai-dnd-system
sudo -u ai-dnd git checkout <ts-commit>
sudo -u ai-dnd npm ci && sudo -u ai-dnd npm run build

# Старый юнит (webhook + Caddy) и Caddyfile
sudo ln -sfn /opt/ai-dnd-system/deploy/systemd/ai-dnd-ts.service /etc/systemd/system/ai-dnd-ts.service
sudo systemctl daemon-reload
sudo systemctl disable --now ai-dnd
sudo systemctl enable --now ai-dnd-ts
# и вернуть Caddy: cp deploy/caddy/Caddyfile /etc/caddy/Caddyfile && restart caddy
```

Домен/Caddy нужны только TS-пути. После слайса 8 файлы `ai-dnd-ts.service`,
`start-polling-ts.sh` и `caddy/` удаляются.

## Типовые проблемы

| Симптом | Что проверить |
|---------|---------------|
| Бот не отвечает на сообщения | `journalctl -u ai-dnd -n 100`. Частые причины: неверный `TELEGRAM_BOT_TOKEN`, `LLM_API_KEY`, 401/400 от провайдера. |
| `HTTP 400 MissingSessionID` от OpenCode Go | Бот должен слать `x-opencode-session`; в Go это делает `internal/llm` (реегрессия — тест `TestOpenCodeGoSessionHeader`). |
| Сервис не стартует | `journalctl -u ai-dnd -n 50`. `bin/ai-dnd -check` проверяет env без сети. |
| `SQLITE_BUSY` | Один процесс-писатель. Проверь, не запущен ли второй экземпляр или старый TS-бот на той же базе: `ps aux \| grep ai-dnd`. |
| Нет сообщений в group-чате | Включён privacy mode. BotFather → `/setprivacy` → Disable, либо сделать бота админом группы. |
| Старые дни без саммари | Очередь летописца закрывает последние `DAYCLOSE_BACKFILL_DAYS` дней; более старые — детерминированный дайджест. Смотри `dayclose:` в логе. |
| Бэкап не создаётся | `sqlite3` в PATH и права `ai-dnd` на `data/`. Скрипт падает, если снимок не прошёл `integrity_check`. |

## Что персистентно, что эфемерно

| Путь | Статус | Содержит |
|------|--------|----------|
| `data/campaigns.db` (+`-wal`, `-shm`) | **БЭКАПИТЬ** | кампании, участники, персонажи, квесты, треды, транскрипты дней, саммари, NPC/локации/фракции, лут-леджер, world-state, снимок боя, durable-inbox и `event_streams` |
| `data/.skills` | эфемерно | встроенные скиллы, перезаписываются при старте |
| `.env` | **ХРАНИТЬ** (вне репо) | секреты |
| `bin/ai-dnd` | пересобирается | Go-бинарь с версией из git |
| `data/backups/` | ротация | сами бэкапы (14 последних) |
| `.eve/`, `.output/` | эфемерно | остатки TS-версии; удаление — слайс 8 |

## Важно

- **`EnvironmentFile=.env`**: Go-бот не читает `.env` сам в systemd-режиме —
  переменные инжектирует systemd. Для ручного запуска `start-polling.sh`
  подгружает `.env`.
- **Graceful shutdown**: `KillSignal=SIGTERM`, `TimeoutStopSec=25`. Ходы
  дожидаются до 15 с, затем отменяются; очередь летописца и база закрываются.
  Не уменьшайте таймаут: SIGKILL посреди записи в SQLite оставит WAL-хвост
  (база восстановится, но может потерять последний ход).
- **Харденинг**: `ProtectSystem=strict` + `ReadWritePaths=data`. Если перенести
  базу из `data/`, добавьте её каталог в `ReadWritePaths`, иначе Get/Set
  упадут с `read-only file system`.
- **rate-limiter in-memory** — один процесс. Для нескольких инстансов нужен
  общий стор (Redis и т.п.), для одиночного бота неактуально.
