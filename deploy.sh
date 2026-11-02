#!/usr/bin/env bash
# Развернуть обновление на VPS: pull → build → restart + проверка.
# Идемпотентный — безопасно запускать повторно.
#
# Запуск (по ssh на сервере, от пользователя с sudo):
#   ./deploy.sh                 # Go-бинарь (по умолчанию)
#   ./deploy.sh --skip-backup   # пропустить преддеплойный бэкап
#   ./deploy.sh --ts            # старый TS/eve-путь до слайса 8 (откат)
#
# Требует: systemd-юнит ai-dnd.service (Go) или ai-dnd-ts.service (TS),
# установленный через deploy/setup.sh.

set -euo pipefail

# ─── Конфигурация ─────────────────────────────────────────────────────────────
INSTALL_DIR="${INSTALL_DIR:-/opt/ai-dnd-system}"
SERVICE_NAME="${SERVICE_NAME:-ai-dnd}"
SERVICE_USER="${SERVICE_USER:-ai-dnd}"
HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-60}"   # секунд — сколько ждать подъёма сервиса
HEALTH_URL="${HEALTH_URL:-http://127.0.0.1:3000/eve/v1/health}"
# ───────────────────────────────────────────────────────────────────────────────

c_red()    { printf '\033[31m%s\033[0m\n' "$*"; }
c_green()  { printf '\033[32m%s\033[0m\n' "$*"; }
c_yellow() { printf '\033[33m%s\033[0m\n' "$*"; }
c_blue()   { printf '\033[34m%s\033[0m\n' "$*"; }

step() { echo; c_blue "▶ $*"; }
note() { echo "  • $*"; }
ok()   { c_green "  ✓ $*"; }
die()  { c_red "✗ $*" >&2; exit 1; }

SKIP_BACKUP=0
TS_MODE=0
for arg in "$@"; do
  case "$arg" in
    --skip-backup) SKIP_BACKUP=1 ;;
    --ts)          TS_MODE=1; SERVICE_NAME="${TS_SERVICE_NAME:-ai-dnd-ts}" ;;
    *) die "Неизвестный флаг: $arg" ;;
  esac
done

cd "$INSTALL_DIR"

# ─── Проверки предусловий ─────────────────────────────────────────────────────
[[ -d .git ]] || die "Не в git-репозитории: $INSTALL_DIR (выполните deploy/setup.sh)."
command -v systemctl >/dev/null 2>&1 || die "systemd не найден."
[[ -f "/etc/systemd/system/${SERVICE_NAME}.service" ]] \
  || die "Systemd-юнит ${SERVICE_NAME} не установлен. Сначала выполните sudo bash deploy/setup.sh."

if [[ $TS_MODE -eq 0 ]]; then
  command -v go >/dev/null 2>&1 || die "Требуется Go >= 1.24 (запустите deploy/setup.sh)."
else
  node_v="$(node -v 2>/dev/null || true)"
  [[ "$node_v" =~ ^v24 ]] || die "TS-режим требует Node 24 (найдено: ${node_v:-не установлен})."
fi

# ─── Преддеплойный бэкап (чтобы можно было откатиться при падении миграции) ────
if [[ $SKIP_BACKUP -eq 0 ]]; then
  step "Преддеплойный бэкап data/"
  if [[ -f deploy/backup.sh ]]; then
    sudo -u "$SERVICE_USER" bash deploy/backup.sh || die "Бэкап не удался. Используйте --skip-backup для пропуска."
    ok "Бэкап готов."
  else
    note "deploy/backup.sh не найден — пропускаю."
  fi
else
  note "Бэкап пропущен (--skip-backup)."
fi

# ─── 1. Обновление кода ───────────────────────────────────────────────────────
step "1/3 — Обновление кода (git pull)"
sudo -u "$SERVICE_USER" -H git fetch --all --prune
sudo -u "$SERVICE_USER" -H git reset --hard "@{u}" 2>/dev/null \
  || sudo -u "$SERVICE_USER" -H git pull --ff-only
ok "Код обновлён на $(sudo -u "$SERVICE_USER" -H git rev-parse --short HEAD)."

# ─── 2. Сборка ────────────────────────────────────────────────────────────────
if [[ $TS_MODE -eq 1 ]]; then
  step "2/3 — TS-сборка (npm ci → npm run build)"
  sudo -u "$SERVICE_USER" -H bash -lc "cd '$INSTALL_DIR' && npm ci"
  sudo -u "$SERVICE_USER" -H bash -lc "cd '$INSTALL_DIR' && npm run build"
  ok "Сборка завершена, артефакт в .output/."
else
  step "2/3 — Сборка Go-бинаря"
  sudo -u "$SERVICE_USER" -H bash -lc "cd '$INSTALL_DIR' && mkdir -p bin && \
    VERSION=\$(git rev-parse --short HEAD) && \
    go build -trimpath \
      -ldflags \"-s -w -X main.version=\$VERSION -X main.buildDate=\$(date -u +%Y-%m-%dT%H:%M:%SZ)\" \
      -o bin/ai-dnd.new ./cmd/bot && mv bin/ai-dnd.new bin/ai-dnd"
  ok "Готово: bin/ai-dnd ($(sudo -u "$SERVICE_USER" -H git rev-parse --short HEAD))."
fi

# ─── 3. Перезапуск + проверка ────────────────────────────────────────────────
step "3/3 — Перезапуск сервиса и проверка"
note "systemctl restart $SERVICE_NAME..."
sudo systemctl restart "${SERVICE_NAME}.service"

deadline=$(( $(date +%s) + HEALTH_TIMEOUT ))
# Go-боту нужен короткий прогрев: сразу после restart юнит уже active, а
# падение на инициализации видно через секунды. Дальше крутим цикл до deadline.
if [[ $TS_MODE -eq 0 ]]; then
  sleep 3
fi
healthy=0
while [[ $(date +%s) -lt $deadline ]]; do
  if systemctl is-active --quiet "${SERVICE_NAME}.service"; then
    if [[ $TS_MODE -eq 1 ]]; then
      if curl -sf --max-time 3 "$HEALTH_URL" >/dev/null 2>&1; then healthy=1; break; fi
    else
      healthy=1
      break
    fi
  fi
  sleep 2
done

if [[ $healthy -eq 1 ]]; then
  ok "Сервис активен."
else
  c_red "✗ Сервис не поднялся за ${HEALTH_TIMEOUT}s. Логи:"
  journalctl -u "$SERVICE_NAME" -n 40 --no-pager >&2 || true
  die "Деплой не прошёл проверку. Откатитесь: git checkout <предыдущий коммит> && ./deploy.sh --skip-backup"
fi

echo
c_green "════════════════════════════════════════════"
c_green "  Деплой завершён успешно."
echo    "  Версия: $(sudo -u "$SERVICE_USER" -H git rev-parse --short HEAD) ($(sudo -u "$SERVICE_USER" -H git log -1 --format=%ci))"
echo    "  Логи:   journalctl -u $SERVICE_NAME -f"
c_green "════════════════════════════════════════════"
