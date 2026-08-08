#!/usr/bin/env bash
# Развернуть обновление на VPS: pull → deps → build → restart + health-check.
# Идемпотентный — безопасно запускать повторно.
#
# Запуск (по ssh на сервере, от пользователя с sudo):
#   ./deploy.sh
#   ./deploy.sh --skip-backup    # пропустить преддеплойный бэкап
#
# Требует: systemd-юнит ai-dnd.service, установленный через deploy/setup.sh.

set -euo pipefail

# ─── Конфигурация ─────────────────────────────────────────────────────────────
INSTALL_DIR="${INSTALL_DIR:-/opt/ai-dnd-system}"
SERVICE_NAME="${SERVICE_NAME:-ai-dnd}"
SERVICE_USER="${SERVICE_USER:-ai-dnd}"
HEALTH_URL="${HEALTH_URL:-http://127.0.0.1:3000/eve/v1/health}"
HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-60}"   # секунд — сколько ждать подъёма сервиса
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
[[ "${1:-}" == "--skip-backup" ]] && SKIP_BACKUP=1

cd "$INSTALL_DIR"

# ─── Проверки предусловий ─────────────────────────────────────────────────────
[[ -d .git ]] || die "Не в git-репозитории: $INSTALL_DIR (выполните deploy/setup.sh)."
command -v systemctl >/dev/null 2>&1 || die "systemd не найден."
[[ -f "/etc/systemd/system/${SERVICE_NAME}.service" ]] \
  || die "Systemd-юнит не установлен. Сначала выполните sudo bash deploy/setup.sh."

node_v="$(node -v 2>/dev/null || true)"
[[ "$node_v" =~ ^v24 ]] || die "Требуется Node 24 (найдено: ${node_v:-не установлен})."

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
step "1/4 — Обновление кода (git pull)"
sudo -u "$SERVICE_USER" -H git fetch --all --prune
sudo -u "$SERVICE_USER" -H git reset --hard "@{u}" 2>/dev/null \
  || sudo -u "$SERVICE_USER" -H git pull --ff-only
ok "Код обновлён на $(sudo -u "$SERVICE_USER" -H git rev-parse --short HEAD)."

# ─── 2. Зависимости ───────────────────────────────────────────────────────────
step "2/4 — Установка зависимостей (npm ci)"
# Если package-lock изменился — пересоберём, иначе npm ci быстрая и идемпотентная.
sudo -u "$SERVICE_USER" -H bash -lc "cd '$INSTALL_DIR' && npm ci"
ok "Зависимости актуальны (включая prebuilt better-sqlite3)."

# ─── 3. Сборка ────────────────────────────────────────────────────────────────
step "3/4 — Сборка (npm run build)"
sudo -u "$SERVICE_USER" -H bash -lc "cd '$INSTALL_DIR' && npm run build"
ok "Сборка завершена, артефакт в .output/."

# ─── 4. Перезапуск + health-check ─────────────────────────────────────────────
step "4/4 — Перезапуск сервиса и проверка здоровья"
note "systemctl restart $SERVICE_NAME..."
sudo systemctl restart "${SERVICE_NAME}.service"
ok "Сервис перезапущен."

note "Ожидание /eve/v1/health (до ${HEALTH_TIMEOUT}s)..."
deadline=$(( $(date +%s) + HEALTH_TIMEOUT ))
healthy=0
while [[ $(date +%s) -lt $deadline ]]; do
  if curl -sf --max-time 3 "$HEALTH_URL" >/dev/null 2>&1; then
    healthy=1
    break
  fi
  sleep 2
done

if [[ $healthy -eq 1 ]]; then
  ok "Сервис здоров: $HEALTH_URL → 200"
else
  c_red "✗ Сервис не поднялся за ${HEALTH_TIMEOUT}s. Логи:"
  journalctl -u "$SERVICE_NAME" -n 40 --no-pager >&2 || true
  die "Деплой не прошёл health-check. Откатитесь: git checkout <предыдущий коммит> && ./deploy.sh --skip-backup"
fi

echo
c_green "════════════════════════════════════════════"
c_green "  Деплой завершён успешно."
echo    "  Версия: $(sudo -u "$SERVICE_USER" -H git rev-parse --short HEAD) ($(sudo -u "$SERVICE_USER" -H git log -1 --format=%ci))"
echo    "  Логи:   journalctl -u $SERVICE_NAME -f"
c_green "════════════════════════════════════════════"
