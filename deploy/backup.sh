#!/usr/bin/env bash
# Резервное копирование игрового состояния (data/) для бота.
# Готов к запуску по cron, ОСТАНАВЛИВАТЬ сервис НЕ нужно.
#
# Что бэкапим:
#   - SQLite-БД:     data/campaigns.db  (через sqlite3 .backup — согласованно с активным WAL)
#                     вся память кампаний — кампании, журнал, NPC, локации, фракции,
#                     лут-леджер, world-state, combat — в одной базе
#
# Ротация: храним последние BACKUP_KEEP копий (по умолч. 14), старые удаляем.
#
# Запуск:
#   sudo -u ai-dnd bash deploy/backup.sh
#   либо по cron (root, раз в сутки):
#   0 3 * * * /opt/ai-dnd-system/deploy/backup.sh

set -euo pipefail

# ─── Конфигурация (можно переопределить через env) ────────────────────────────
INSTALL_DIR="${INSTALL_DIR:-/opt/ai-dnd-system}"
DATA_DIR="${DATA_DIR:-$INSTALL_DIR/data}"
BACKUP_DIR="${BACKUP_DIR:-$INSTALL_DIR/data/backups}"
BACKUP_KEEP="${BACKUP_KEEP:-14}"
DB_PATH="${CAMPAIGN_DB_PATH:-$DATA_DIR/campaigns.db}"
# ───────────────────────────────────────────────────────────────────────────────

c_red()    { printf '\033[31m%s\033[0m\n' "$*"; }
c_green()  { printf '\033[32m%s\033[0m\n' "$*"; }
c_yellow() { printf '\033[33m%s\033[0m\n' "$*"; }
die()  { c_red "✗ $*" >&2; exit 1; }

ts() { date -u +"%Y%m%dT%H%M%SZ"; }

# ─── Проверки ──────────────────────────────────────────────────────────────────
command -v sqlite3 >/dev/null 2>&1 || die "sqlite3 не установлен (apt-get install sqlite3)."
[[ -d "$DATA_DIR" ]] || die "Каталог данных не найден: $DATA_DIR (бот ещё ни разу не запускался?)."

stamp="$(ts)"
dest="$BACKUP_DIR/$stamp"
mkdir -p "$dest"

echo "Бэкап от $stamp → $dest"

# 1. SQLite: согласованный снимок через .backup (не повреждает активный WAL).
if [[ -f "$DB_PATH" ]]; then
  echo "  • SQLite .backup..."
  # Чекпоинт WAL перед копией, чтобы файлы -wal/-shm не остались непустыми.
  sqlite3 "$DB_PATH" "PRAGMA wal_checkpoint(TRUNCATE);" >/dev/null 2>&1 || true
  sqlite3 "$DB_PATH" ".backup '$dest/$(basename "$DB_PATH")'"
  echo "    ✓ $(basename "$DB_PATH")"
else
  echo "  • БД $DB_PATH не найдена — пропускаю (бот ещё не запускался?)."
fi

# 2. Копируем .env, чтобы иметь под рукой конфигурацию на момент бэкапа.
if [[ -f "$INSTALL_DIR/.env" ]]; then
  cp "$INSTALL_DIR/.env" "$dest/env.txt"
fi

# 3. Ротация: оставляем BACKUP_KEEP последних копий.
echo "  • Ротация (оставляем $BACKUP_KEEP копий)..."
# Список бэкап-каталогов по времени имени, свежие сверху; удаляем всё после N.
deleted=0
mapfile -t dirs < <(ls -1d "$BACKUP_DIR"/*/ 2>/dev/null | sort -r)
for d in "${dirs[@]:$BACKUP_KEEP}"; do
  [[ -n "$d" ]] || continue
  rm -rf "${d%/}"
  deleted=$((deleted + 1))
done
echo "    ✓ удалено старых: $deleted"

# 4. Сводка.
total_size=$(du -sh "$dest" | cut -f1)
c_green "✓ Бэкап готов: $dest ($total_size)"
