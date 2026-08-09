#!/usr/bin/env bash
# Запуск бота на VPS БЕЗ публичного домена: long polling вместо webhook.
#
# Одна команда поднимает оба процесса (eve dev + npm run poll) в двух
# tmux-сессиях, поэтому они переживают отключение SSH. Это замена
# "двум терминалам" из локальной разработки (см. README, раздел
# «Получение сообщений без публичного URL»).
#
# Использование:
#   ./deploy/start-polling.sh          # запустить (идемпотентно)
#   tmux attach -t bot                 # логи eve (Ctrl-b d — свернуть)
#   tmux attach -t poll                # логи поллера
#
# Требуется: установленный tmux (sudo apt install tmux) и заполненный .env.

set -euo pipefail

cd "$(dirname "$0")/.." || exit 1

c_green() { printf '\033[32m%s\033[0m\n' "$*"; }
c_yellow(){ printf '\033[33m%s\033[0m\n' "$*"; }

command -v tmux >/dev/null 2>&1 || {
  echo "tmux не установлен. Сначала: sudo apt install tmux" >&2
  exit 1
}

start() { # $1=имя сессии, $2..=команда
  local name="$1"; shift
  if tmux has-session -t "$name" 2>/dev/null; then
    c_yellow "[start] сессия '$name' уже запущена"
  else
    tmux new-session -d -s "$name" "$*"
    c_green "[start] '$name' запущена: $*"
  fi
}

start bot  "npm run dev -- --no-ui"
start poll "npm run poll"

echo
cat <<EOF
$(c_green "✓ Всё запущено.")

  Логи:    tmux attach -t bot   |  tmux attach -t poll
  Свернуть: Ctrl-b, затем d
  Остановить: tmux kill-session -t bot && tmux kill-session -t poll
EOF
