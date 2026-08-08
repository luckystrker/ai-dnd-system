#!/usr/bin/env bash
# Первичная установка бота на свежем VPS (Ubuntu/Debian).
# Запускать ОДИН РАЗ под root:  sudo bash deploy/setup.sh
#
# Что делает:
#   1. Ставит Node 24 (NodeSource) и Caddy.
#   2. Создаёт системного пользователя ai-dnd и каталог /opt/ai-dnd-system.
#   3. Клонирует репозиторий, ставит зависимости, собирает билд.
#   4. Устанавливает systemd-юнит и Caddyfile (символическими ссылками).
#   5. Включает сервисы в автозагрузку.
#
# НЕ запускает сервисы — после скрипта нужно заполнить .env и задать домен
# (см. deploy/README.md), затем systemctl start.

set -euo pipefail

# ─── Конфигурация (можно переопределить через env) ────────────────────────────
SERVICE_USER="${SERVICE_USER:-ai-dnd}"
INSTALL_DIR="${INSTALL_DIR:-/opt/ai-dnd-system}"
REPO_URL="${REPO_URL:-https://github.com/luckystrker/ai-dnd-system.git}"
SERVICE_NAME="ai-dnd"
# Главная ветка по умолчанию (см. AGENTS.md).
REPO_BRANCH="${REPO_BRANCH:-main}"
# ───────────────────────────────────────────────────────────────────────────────

c_red()   { printf '\033[31m%s\033[0m\n' "$*"; }
c_green() { printf '\033[32m%s\033[0m\n' "$*"; }
c_yellow(){ printf '\033[33m%s\033[0m\n' "$*"; }
c_blue()  { printf '\033[34m%s\033[0m\n' "$*"; }

step() { echo; c_blue "▶ $*"; }
note() { c_yellow "  • $*"; }
ok()   { c_green "  ✓ $*"; }
die()  { c_red "✗ Ошибка: $*" >&2; exit 1; }

# ─── Проверки предусловий ─────────────────────────────────────────────────────
[[ $EUID -eq 0 ]] || die "Скрипт нужно запускать под root (sudo)."

# Определяем пакетный менеджер: apt (Debian/Ubuntu).
if command -v apt-get >/dev/null 2>&1; then
  PM=apt-get
else
  die "Поддерживается только Debian/Ubuntu (apt-get). Для других ОС — установите Node 24 и Caddy вручную."
fi

step "1/6 — Подготовка системы"
export DEBIAN_FRONTEND=noninteractive
$PM update -y
$PM install -y curl ca-certificates gnupg git sqlite3 tar sudo
ok "Базовые пакеты установлены."

step "2/6 — Установка Node.js 24"
if command -v node >/dev/null 2>&1 && node -v | grep -q '^v24'; then
  ok "Node.js $(node -v) уже установлен."
else
  note "Устанавливаю NodeSource-репозиторий для Node 24..."
  curl -fsSL https://deb.nodesource.com/setup_24.x | bash -
  $PM install -y nodejs
  ok "Node.js $(node -v), npm $(npm -v) установлены."
fi

step "3/6 — Установка Caddy (reverse proxy + авто-TLS)"
if command -v caddy >/dev/null 2>&1; then
  ok "Caddy $(caddy version | head -1) уже установлен."
else
  note "Подключаю официальный репозиторий Caddy..."
  $PM install -y debian-keyring debian-archive-keyring apt-transport-https
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' \
    | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' \
    | tee /etc/apt/sources.list.d/caddy-stable.list >/dev/null
  $PM update -y
  $PM install -y caddy
  ok "Caddy установлен."
fi

step "4/6 — Создание пользователя и каталога"
if id "$SERVICE_USER" &>/dev/null; then
  ok "Пользователь $SERVICE_USER существует."
else
  useradd --system --create-home --shell /usr/sbin/nologin "$SERVICE_USER"
  ok "Создан системный пользователь $SERVICE_USER."
fi
# Caddy должен иметь право читать каталог приложения (отдаёт статику /
# проксирует), а сервис —读写ать data/. Даём ownership.
mkdir -p "$INSTALL_DIR"
chown -R "$SERVICE_USER":"$SERVICE_USER" "$INSTALL_DIR"

if [[ -d "$INSTALL_DIR/.git" ]]; then
  note "$INSTALL_DIR уже содержит git-репозиторий — пропускаю клонирование."
else
  note "Клонирую $REPO_URL (ветка $REPO_BRANCH)..."
  # Клонируем от root, затем передаём ownership.
  git clone --branch "$REPO_BRANCH" "$REPO_URL" "$INSTALL_DIR"
  chown -R "$SERVICE_USER":"$SERVICE_USER" "$INSTALL_DIR"
  ok "Репозиторий клонирован в $INSTALL_DIR."
fi

step "5/6 — Установка зависимостей и сборка"
# better-sqlite3 берёт prebuilt-бинарник под linux-x64 — компилятор не нужен.
note "npm ci (от пользователя $SERVICE_USER)..."
sudo -u "$SERVICE_USER" -H bash -lc "cd '$INSTALL_DIR' && npm ci"
note "npm run build..."
sudo -u "$SERVICE_USER" -H bash -lc "cd '$INSTALL_DIR' && npm run build"
ok "Сборка завершена, артефакт в $INSTALL_DIR/.output/."

step "6/6 — Установка systemd-юнита и Caddyfile"
UNIT_SRC="$INSTALL_DIR/deploy/systemd/${SERVICE_NAME}.service"
UNIT_DST="/etc/systemd/system/${SERVICE_NAME}.service"
CADDY_SRC="$INSTALL_DIR/deploy/caddy/Caddyfile"
CADDY_DST="/etc/caddy/Caddyfile"

[[ -f "$UNIT_SRC" ]] || die "Не найден $UNIT_SRC — проверьте, что клонирована ветка с deploy/."
[[ -f "$CADDY_SRC" ]] || die "Не найден $CADDY_SRC."

ln -sfn "$UNIT_SRC" "$UNIT_DST"
systemctl daemon-reload
systemctl enable "${SERVICE_NAME}.service"
ok "Systemd-юнит установлен и включён в автозагрузку."

# Бэкапим дефолтный Caddyfile один раз.
if [[ ! -f /etc/caddy/Caddyfile.orig ]]; then
  cp "$CADDY_DST" /etc/caddy/Caddyfile.orig 2>/dev/null || true
fi
cp "$CADDY_SRC" "$CADDY_DST"
systemctl enable caddy
# НЕ перезапускаем Caddy сейчас — домен ещё не задан, TLS упадёт.
ok "Caddyfile установлен, Caddy включён в автозагрузку."

# ─── Что делать дальше ─────────────────────────────────────────────────────────
cat <<EOF

$(c_green "✓ Установка завершена.")

$(c_yellow "Дальнейшие шаги (см. deploy/README.md):")

  1. Создайте файл окружения:
       sudo -u $SERVICE_USER cp $INSTALL_DIR/.env.example $INSTALL_DIR/.env
       sudo -u $SERVICE_USER nano $INSTALL_DIR/.env
     Впишите реальные LLM_API_KEY, TELEGRAM_BOT_TOKEN,
     TELEGRAM_BOT_USERNAME, TELEGRAM_WEBHOOK_SECRET_TOKEN.

  2. Укажите свой домен в Caddyfile:
       sudo nano /etc/caddy/Caddyfile
     Замените bot.example.com на ваш домен.

  3. Убедитесь, что A-запись домена указывает на IP этого сервера.

  4. Запустите сервисы:
       sudo systemctl start caddy
       sudo systemctl start $SERVICE_NAME

  5. Проверьте здоровье:
       curl -s http://127.0.0.1:3000/eve/v1/health

  6. Зарегистрируйте webhook в Telegram (подставьте домен и секрет):
       curl -X POST "https://api.telegram.org/bot\$TELEGRAM_BOT_TOKEN/setWebhook" \\
         -H "Content-Type: application/json" \\
         -d '{"url":"https://ВАШ-ДОМЕН/eve/v1/telegram",
              "secret_token":"\$TELEGRAM_WEBHOOK_SECRET_TOKEN",
              "allowed_updates":["message","callback_query"]}'

  7. Настройте ночные бэкапы (опционально):
       sudo crontab -e
       # добавить строку:
       # 0 3 * * * $INSTALL_DIR/deploy/backup.sh

EOF
