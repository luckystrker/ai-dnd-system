# deploy/ — ops-референс и инфраструктура VPS

Стек: **bare metal + systemd + Caddy**. Ручной деплой, ночные бэкапы по cron.
Подробное пошаговое руководство — в корневом `README.md` (раздел «Деплой на VPS»).
Здесь — справочник для эксплуатации и диагностики.

## Что где лежит

| Файл | Назначение |
|------|------------|
| `setup.sh` | Первичная установка на свежем VPS (run-once). Ставит Node 24 + Caddy, клонирует репо, собирает билд, ставит systemd-юнит и Caddyfile. |
| `backup.sh` | Бэкап `data/campaigns.db` (SQLite `.backup` + WAL-чекпоинт), ротация 14 копий. Готов к cron. |
| `../deploy.sh` | Обновление бота (в корне репо): `git pull → npm ci → build → restart + health-check`. |
| `caddy/Caddyfile` | Reverse proxy + авто-TLS (Let's Encrypt). Проксирует на `127.0.0.1:3000`. |
| `systemd/ai-dnd.service` | Supervisor-юнит. `User=ai-dnd`, `EnvironmentFile=.env`, `Restart=on-failure`. |

## Топология

```
Telegram ──HTTPS──▶ Caddy (:80, :443, авто-TLS)
                      │  reverse_proxy
                      ▼
                 eve-сервер (Nitro) ──▶ 127.0.0.1:3000  [systemd: ai-dnd.service]
                      │
                      ├── /opt/ai-dnd-system/.output/   (билд)
                      ├── /opt/ai-dnd-system/data/      (SQLite — ПЕРСИСТЕНТНО)
                      └── /opt/ai-dnd-system/.env       (секреты, читает systemd)
```

Один процесс, горизонтальное масштабирование не предусмотрено (SQLite + in-memory
rate-limiter). Этого достаточно для одного бота на одном VPS.

## Типовые операции

### Обновить бота

```bash
cd /opt/ai-dnd-system
./deploy.sh                 # с преддеплойным бэкапом
./deploy.sh --skip-backup   # быстро, без бэкапа
```

Скрипт делает health-check (`/eve/v1/health`) после рестарта. Если сервис не
поднялся за 60с — деплой считается провалившимся, в stderr выводятся последние
40 строк лога.

### Откатить версию

```bash
cd /opt/ai-dnd-system
sudo -u ai-dnd git log --oneline -10          # найти предыдущий коммит
sudo -u ai-dnd git checkout <commit-sha>
./deploy.sh --skip-backup
```

### Восстановить из бэкапа

```bash
sudo systemctl stop ai-dnd
# Бэкап лежит в /opt/ai-dnd-system/data/backups/<timestamp>/
BACKUP=/opt/ai-dnd-system/data/backups/20260101T030000Z

# 1. SQLite (вся память кампаний — журнал, NPC, локации, world-state и т.п.)
cp "$BACKUP/campaigns.db" /opt/ai-dnd-system/data/campaigns.db

sudo -u ai-dnd chown -R ai-dnd:ai-dnd /opt/ai-dnd-system/data
sudo systemctl start ai-dnd
curl -s http://127.0.0.1:3000/eve/v1/health
```

### Изменить секрет webhook'а или токен

```bash
sudo -u ai-dnd nano /opt/ai-dnd-system/.env   # правим TELEGRAM_*
sudo systemctl restart ai-dnd
# Перерегистрируем webhook с новым секретом:
curl -X POST "https://api.telegram.org/bot$TELEGRAM_BOT_TOKEN/setWebhook" \
  -H "Content-Type: application/json" \
  -d '{"url":"https://ВАШ-ДОМЕН/eve/v1/telegram",
       "secret_token":"'"$TELEGRAM_WEBHOOK_SECRET_TOKEN"'",
       "allowed_updates":["message","callback_query"]}'
```

## Типовые проблемы

| Симптом | Что проверить |
|---------|---------------|
| Бот не отвечает на сообщения | `curl "https://api.telegram.org/bot$TOKEN/getWebhookInfo"` — поле `last_error_message`. Частые причины: неверный `secret_token`, протухший TLS, 5xx от сервера. |
| Сервис не стартует | `journalctl -u ai-dnd -n 50 --no-pager`. Проверь `.env` (особенно `LLM_API_KEY`, `TELEGRAM_BOT_TOKEN`), что `npm run build` отработал, что Node 24. |
| Caddy не получает сертификат | `journalctl -u caddy -n 50`. Убедись, что A-запись домена указывает на IP сервера, порты 80/443 открыты в файрволе, домен в Caddyfile указан верно. |
| 502 Bad Gateway | eve-сервер не поднялся или слушает не тот порт. `systemctl status ai-dnd`, `curl 127.0.0.1:3000/eve/v1/health`. |
| База занята / `SQLITE_BUSY` | Один процесс писателя; при 502+ очередях проверь, что не запущен второй экземпляр (`ps aux \| grep node`). |
| `better-sqlite3` не грузится | Сборка проходила не под тем пользователем/arch. Пересобери: `sudo -u ai-dnd npm ci` из `/opt/ai-dnd-system`. |
| В group-чате бот видит не все сообщения | Включён privacy mode. BotFather → `/setprivacy` → Disable, либо сделать бота админом группы. |

## Что персистентно, что эфемерно

| Путь | Статус | Содержит |
|------|--------|----------|
| `data/campaigns.db` (+`-wal`, `-shm`) | **БЭКАПИТЬ** | кампании, участники, персонажи, квесты, треды, транскрипты дней, саммари, NPC/локации/фракции, лут-леджер, world-state, снимок боя |
| `.env` | **ХРАНИТЬ** (вне репо) | секреты |
| `.eve/` | эфемерно | трейсы, локи, билды, sandbox-кэш — пересоздаётся |
| `.output/` | эфемерно | билд-артефакт — пересобирается `deploy.sh` |
| `data/backups/` | ротация | сами бэкапы (14 последних) |

## Важно

- **`eve start` не читает `.env`** — переменные инжектирует systemd через
  `EnvironmentFile`. Поэтому `.env` обязателен в `/opt/ai-dnd-system/`.
- **better-sqlite3 — native addon**, держится external в билде. На типичном
  Linux x64/arm64 берётся prebuilt-бинарник, компилятор не нужен. При смене arch
  (например, x86→arm) — пересобери `npm ci` под целевую платформу.
- **rate-limiter in-memory** — один процесс. Для нескольких инстансов нужен
  общий стор (Redis и т.п.), но для одиночного бота это неактуально.
