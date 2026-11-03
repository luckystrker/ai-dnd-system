#!/usr/bin/env bash
# Запуск Go-бота вручную (без systemd): long-poll встроен в бинарь.
#
# Для боевого сервера используйте systemd (deploy/setup.sh + ai-dnd.service).
#
# Использование:
#   ./deploy/start-polling.sh          # из корня репозитория
#   BIN=/opt/ai-dnd-system/bin/ai-dnd ./deploy/start-polling.sh
#
# Требуется заполненный .env в корне (переменные читаются из окружения).

set -euo pipefail

cd "$(dirname "$0")/.." || exit 1

BIN="${BIN:-./bin/ai-dnd}"
if [[ ! -x "$BIN" ]]; then
  echo "Нет бинаря $BIN. Соберите: go build -o bin/ai-dnd ./cmd/bot" >&2
  exit 1
fi

if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  . ./.env
  set +a
fi

exec "$BIN" -mode telegram
