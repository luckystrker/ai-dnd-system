#!/usr/bin/env bash
# Первичная установка Go-бота на свежем VPS (Ubuntu/Debian).
# Запускать ОДИН РАЗ под root:  sudo bash deploy/setup.sh
#
# Что делает:
#   1. Ставит базовые пакеты и Go (>= 1.24) из официального тарбола.
#   2. Создаёт системного пользователя ai-dnd и каталог /opt/ai-dnd-system.
#   3. Клонирует репозиторий и собирает Go-бинарь с версией из git.
#   4. Ставит systemd-юнит ai-dnd (long-poll, без Caddy и webhook).
#
# НЕ запускает сервис — после скрипта нужно заполнить .env (см. deploy/README.md).

set -euo pipefail

# ─── Конфигурация (можно переопределить через env) ────────────────────────────
SERVICE_USER="${SERVICE_USER:-ai-dnd}"
INSTALL_DIR="${INSTALL_DIR:-/opt/ai-dnd-system}"
REPO_URL="${REPO_URL:-https://github.com/luckystrker/ai-dnd-system.git}"
SERVICE_NAME="ai-dnd"
REPO_BRANCH="${REPO_BRANCH:-main}"
GO_VERSION="${GO_VERSION:-1.24.6}"
# ───────────────────────────────────────────────────────────────────────────────

c_red()   { printf '\033[31m%s\033[0m\n' "$*"; }
c_green() { printf '\033[32m%s\033[0m\n' "$*"; }
c_yellow(){ printf '\033[33m%s\033[0m\n' "$*"; }
c_blue()  { printf '\033[34m%s\033[0m\n' "$*"; }

step() { echo; c_blue "▶ $*"; }
note() { c_yellow "  • $*"; }
ok()   { c_green "  ✓ $*"; }
die()  { c_red "✗ Ошибка: $*" >&2; exit 1; }

[[ $EUID -eq 0 ]] || die "Скрипт нужно запускать под root (sudo)."
command -v apt-get >/dev/null 2>&1 || die "Поддерживается только Debian/Ubuntu (apt-get)."

step "1/5 — Базовые пакеты"
export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get install -y curl ca-certificates git sqlite3 tar sudo
ok "Базовые пакеты установлены."

step "2/5 — Go ${GO_VERSION}"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64) GOARCH=amd64 ;;
  aarch64|arm64) GOARCH=arm64 ;;
  *) die "Неподдерживаемая архитектура: $ARCH" ;;
esac
if command -v go >/dev/null 2>&1 && go version | awk '{print $3}' | grep -Eq '^go1\.(2[4-9]|[3-9][0-9])'; then
  ok "Go $(go version | awk '{print $3}') уже установлен."
else
  note "Скачиваю go${GO_VERSION}.linux-${GOARCH}.tar.gz..."
  curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${GOARCH}.tar.gz" -o /tmp/go.tar.gz
  rm -rf /usr/local/go
  tar -C /usr/local -xzf /tmp/go.tar.gz
  ln -sfn /usr/local/go/bin/go /usr/local/bin/go
  ln -sfn /usr/local/go/bin/gofmt /usr/local/bin/gofmt
  ok "Go $(go version | awk '{print $3}') установлен."
fi

step "3/5 — Пользователь и каталог"
if id "$SERVICE_USER" &>/dev/null; then
  ok "Пользователь $SERVICE_USER существует."
else
  useradd --system --create-home --shell /usr/sbin/nologin "$SERVICE_USER"
  ok "Создан системный пользователь $SERVICE_USER."
fi
mkdir -p "$INSTALL_DIR"
chown -R "$SERVICE_USER":"$SERVICE_USER" "$INSTALL_DIR"

if [[ -d "$INSTALL_DIR/.git" ]]; then
  note "$INSTALL_DIR уже содержит git-репозиторий — пропускаю клонирование."
else
  note "Клонирую $REPO_URL (ветка $REPO_BRANCH)..."
  git clone --branch "$REPO_BRANCH" "$REPO_URL" "$INSTALL_DIR"
  chown -R "$SERVICE_USER":"$SERVICE_USER" "$INSTALL_DIR"
  ok "Репозиторий клонирован в $INSTALL_DIR."
fi

step "4/5 — Сборка Go-бинаря"
sudo -u "$SERVICE_USER" -H bash -lc "cd '$INSTALL_DIR' && \
  mkdir -p bin && \
  VERSION=\$(git rev-parse --short HEAD) && \
  go build -trimpath \
    -ldflags \"-s -w -X main.version=\$VERSION -X main.buildDate=\$(date -u +%Y-%m-%dT%H:%M:%SZ)\" \
    -o bin/ai-dnd ./cmd/bot"
ok "Готово: $INSTALL_DIR/bin/ai-dnd"

step "5/5 — systemd-юнит"
UNIT_SRC="$INSTALL_DIR/deploy/systemd/${SERVICE_NAME}.service"
UNIT_DST="/etc/systemd/system/${SERVICE_NAME}.service"
[[ -f "$UNIT_SRC" ]] || die "Не найден $UNIT_SRC — проверьте ветку."
ln -sfn "$UNIT_SRC" "$UNIT_DST"
systemctl daemon-reload
systemctl enable "${SERVICE_NAME}.service"
ok "Юнит установлен и включён в автозагрузку (сервис не запущен)."

cat <<EOF

$(c_green "✓ Установка завершена.")

$(c_yellow "Дальнейшие шаги (см. deploy/README.md):")

  1. Создайте окружение:
       sudo -u $SERVICE_USER cp $INSTALL_DIR/.env.example $INSTALL_DIR/.env
       sudo -u $SERVICE_USER nano $INSTALL_DIR/.env
     Обязательно: LLM_API_KEY (OpenCode Go), TELEGRAM_BOT_TOKEN,
     TELEGRAM_BOT_USERNAME.

  2. Запустите сервис (long-poll, публичный домен не нужен):
       sudo systemctl start $SERVICE_NAME
       journalctl -u $SERVICE_NAME -f

  3. Проверьте конфигурацию без сети:
       sudo -u $SERVICE_USER $INSTALL_DIR/bin/ai-dnd -check

  4. Ночные бэкапы (SQLite .backup, ротация 14 копий):
       sudo crontab -e
       # 0 3 * * * $INSTALL_DIR/deploy/backup.sh

EOF
