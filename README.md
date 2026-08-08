# TTRPG Telegram bot (eve agent)

Eve-агент, который ведёт настольную ролевую игру в Telegram: соло-партия в
личном чате или группа до 6 игроков.

## Структура

```
agent/
├── agent.ts              # defineAgent: модель LLM (OpenRouter по умолчанию)
├── instructions.md       # системный промпт DM (соло / группа до 6)
├── channels/telegram.ts  # Telegram-канал (webhook POST /eve/v1/telegram)
├── tools/
│   ├── roll_dice.ts      # скилл: броски костей
│   ├── skill_check.ts    # скилл: проверки характеристик
│   ├── combat.ts         # скилл: пошаговый бой (HP/AC врагов)
│   ├── initiative.ts     # скилл: очередь ходов
│   ├── save_campaign.ts  # сохраняет новую кампанию после опросника
│   ├── list_campaigns.ts # кампании пользователя (owner/member)
│   ├── start_campaign.ts # запуск кампании: привязка к чату/топику
│   ├── get_game_context.ts # кампания + персонажи привязанного чата в контекст
│   ├── save_character.ts # персонаж внутри кампании
│   └── invite_member.ts  # приглашение участника (только DM)
├── skills/
│   ├── create-campaign/  # процедура опросника создания кампании
│   └── create-character/ # процедура создания персонажа
└── lib/
    ├── engine/dnd5e.ts   # правила: броски и проверки
    ├── memory.ts         # состояние партии (defineState)
    ├── rate-limit.ts     # sliding-window лимитер webhook (чат/пользователь)
    └── campaigns/        # хранилище кампаний: SQLite (основное) и MD-файлы (откат)
```

## Кампании и персонажи

Команды бота: `/newcampaign` (опросник создания кампании), `/mycampaigns`
(список), `/startcampaign` (запуск и привязка к текущему чату/топику),
`/join` (вступление игрока в кампанию чата), `/newchar` (создание персонажа),
`/invite` (приглашение игрока, только DM).

- Кампании, участники и персонажи хранятся в SQLite (`data/campaigns.db`,
  переопределяется `CAMPAIGN_DB_PATH`). `CAMPAIGN_STORE=markdown` включает
  откат на MD-файлы в `data/campaigns/` (переопределяется `CAMPAIGN_DATA_DIR`).
  Перенос существующих MD-кампаний в базу: `npm run migrate:sqlite`
  (идемпотентна). Транскрипты, саммари и NPC остаются MD-файлами в папках
  кампаний при любом сторе.
- Кампания создаётся где угодно, но после `/startcampaign` привязывается к
  чату (или топику форум-группы); один чат — одна активная кампания.
- Роли: `dm` (администратор, создатель кампании) и `player`. Приглашает
  участников DM, назначать новых DM может только владелец. Персонаж существует
  только внутри кампании и принадлежит создавшему его игроку: проверки и бой
  за чужого персонажа разрешены только DM.
- Вступление в кампанию только явное: игрок пишет `/join` в привязанном
  чате/топике, либо DM приглашает через `/invite` (лимит 6 игроков).
  Сообщения игроков не в адрес бота записываются в транскрипт дня — DM видит
  диалоги игроков между собой.
- Зарегистрируйте команды в BotFather (`/setcommands`):

  ```
  newcampaign - Создать кампанию
  mycampaigns - Мои кампании
  startcampaign - Запустить кампанию в этом чате
  join - Вступить в кампанию этого чата
  newchar - Создать персонажа
  invite - Пригласить игрока в кампанию
  ```

## Локальный запуск

```bash
npm install
npm run dev -- --no-ui
```

Задайте в окружении `LLM_BASE_URL`, `LLM_API_KEY`, `LLM_MODEL`,
`TELEGRAM_BOT_TOKEN`, `TELEGRAM_BOT_USERNAME`, `TELEGRAM_WEBHOOK_SECRET_TOKEN`
(шаблон в `.env.example`).

Дополнительные настройки защиты и хранилища:

| Переменная | По умолчанию | Назначение |
|------------|--------------|------------|
| `CAMPAIGN_STORE` | `sqlite` | `markdown` — откат на MD-стор |
| `CAMPAIGN_DB_PATH` | `data/campaigns.db` | путь к SQLite-базе |
| `CAMPAIGN_DATA_DIR` | `data/campaigns` | корень MD-данных (транскрипты, NPC и MD-стор) |
| `TELEGRAM_RATE_LIMIT_CHAT_PER_MIN` | `20` | лимит сообщений на чат/топик в минуту |
| `TELEGRAM_RATE_LIMIT_USER_PER_MIN` | `10` | лимит сообщений на пользователя в минуту |
| `TELEGRAM_MAX_UPDATE_BYTES` | `1048576` | максимальный размер входящего update (413 при превышении) |

### Получение сообщений без публичного URL (long polling)

Телеграм-канал eve принимает только webhook на `POST /eve/v1/telegram`, поэтому
в локальной разработке достаточно проксировать webhook-обновления из long
polling во второй терминал:

```bash
npm run poll
```

Скрипт опрашивает Telegram (`getUpdates`) и пересылает каждое обновление в
локальный eve на `http://localhost:2000/eve/v1/telegram` (путь можно поменять
через `EVE_URL`). Никакого туннеля не нужно. В групповых чатах бот реагирует
на `/`-команды, `@упоминания` и ответы на свои сообщения (плюс молча
записывает остальные сообщения в привязанных к кампании чатах); в личных —
на любой текст. При старте локального режима скрипт сам сбрасывает
зарегистрированный webhook (webhook и polling в Telegram несовместимы).

### Альтернатива: webhook на деплое

Зарегистрируйте публичный URL (деплой или туннель вроде cloudflared):

```bash
curl -X POST "https://api.telegram.org/bot$TELEGRAM_BOT_TOKEN/setWebhook" \
  -H "Content-Type: application/json" \
  -d '{"url":"https://your-app.example.com/eve/v1/telegram",
       "secret_token":"'"$TELEGRAM_WEBHOOK_SECRET_TOKEN"'",
       "allowed_updates":["message","callback_query"]}'
```

В групповых чатах бот реагирует на `/`-команды, `@упоминания` и ответы на свои
сообщения; в личных — на любой текст.

### Privacy mode в группах

Чтобы бот видел ВСЕ сообщения группы (диалоги игроков между собой), у бота
должен быть выключен privacy mode: в BotFather выполните `/setprivacy →
Disable`, либо добавьте бота администратором группы. При включённом privacy
mode Telegram доставляет боту только команды, @упоминания и реплаи на его
сообщения — запись диалогов работать не будет.

## Деплой на VPS

Стек: bare metal + systemd (supervisor) + Caddy (reverse proxy с авто-TLS).
Скрипты и конфиги лежат в `deploy/`, подробный ops-референс — в
[`deploy/README.md`](deploy/README.md).

### Пререквизиты

- VPS на Ubuntu/Debian (x86_64/arm64), порты 80 и 443 открыты.
- Домен с A-записью, указывающей на IP сервера (нужен для TLS-сертификата).
- Токен бота Telegram, LLM API-ключ (`LLM_*`), секрет webhook'а.

### 1. Первичная установка

```bash
ssh root@your-vps
git clone https://github.com/luckystrker/ai-dnd-system.git /tmp/repo
cp -r /tmp/repo/deploy /tmp/deploy-bundle
# либо, если репо уже на сервере — просто:
cd /opt/ai-dnd-system   # ещё не существует — setup.sh создаст
sudo bash deploy/setup.sh
```

`setup.sh` ставит Node 24 и Caddy, создаёт пользователя `ai-dnd`, клонирует
репо в `/opt/ai-dnd-system`, собирает билд и включает сервисы в автозагрузку.

### 2. Конфигурация

```bash
sudo -u ai-dnd cp /opt/ai-dnd-system/.env.example /opt/ai-dnd-system/.env
sudo -u ai-dnd nano /opt/ai-dnd-system/.env
# впишите: LLM_API_KEY, TELEGRAM_BOT_TOKEN, TELEGRAM_BOT_USERNAME,
#          TELEGRAM_WEBHOOK_SECRET_TOKEN

sudo nano /etc/caddy/Caddyfile   # замените bot.example.com на ваш домен
```

### 3. Запуск и регистрация webhook

```bash
sudo systemctl start caddy      # получает TLS-сертификат автоматически
sudo systemctl start ai-dnd
curl -s http://127.0.0.1:3000/eve/v1/health    # должно вернуть {"ok":true,...}

# Регистрируем публичный webhook (подставьте домен и секрет из .env):
curl -X POST "https://api.telegram.org/bot$TELEGRAM_BOT_TOKEN/setWebhook" \
  -H "Content-Type: application/json" \
  -d '{"url":"https://ВАШ-ДОМЕН/eve/v1/telegram",
       "secret_token":"'"$TELEGRAM_WEBHOOK_SECRET_TOKEN"'",
       "allowed_updates":["message","callback_query"]}'
```

### 4. Обновления и бэкапы

Обновить бота одной командой (на сервере, из репо):

```bash
cd /opt/ai-dnd-system && ./deploy.sh
#                    # git pull → npm ci → build → restart + health-check
./deploy.sh --skip-backup    # пропустить преддеплойный бэкап
```

Ночные бэкапы `data/` (SQLite `.backup` + архив кампаний, ротация 14 копий):

```bash
sudo crontab -e
# добавить: 0 3 * * * /opt/ai-dnd-system/deploy/backup.sh
```

### Эксплуатация

```bash
journalctl -u ai-dnd -f                  # логи в реальном времени
systemctl status ai-dnd                  # статус процесса
curl -s http://127.0.0.1:3000/eve/v1/health   # проверка живости
curl -s "https://api.telegram.org/bot$TELEGRAM_BOT_TOKEN/getWebhookInfo"  # диагностика Telegram
```

## Проверка

```bash
npm run typecheck
npm run build
npm run smoke          # смоук-тест хранилища кампаний (MD- и SQLite-стор)
npm run migrate:sqlite # перенос MD-кампаний в SQLite (идемпотентно)
```
