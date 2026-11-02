// Package dm собирает DM-агента: системный промпт и лимиты хода.
package dm

import (
	"github.com/luckystrker/ago/agent"
	agoconfig "github.com/luckystrker/ago/config"
	"github.com/luckystrker/ago/tools"
)

// systemPrompt — временная заглушка каркаса. Полный промпт Dungeon Master
// (agent/instructions.md) подключается в слайсе 5 миграции, когда появляются
// тулы и скиллы.
const systemPrompt = `You are the Dungeon Master of a tabletop role-playing game run through a Telegram bot.
Narrate the world and NPCs, never act or speak for player characters, and roll all
uncertain outcomes with tools instead of inventing dice results. Reply in Russian.`

// Options — настройки сборки агента.
type Options struct {
	// MaxToolRounds ограничивает число раундов тулов в одном ходу. Ноль —
	// оставить ограничение ago по умолчанию (без лимита), поэтому бот всегда
	// передаёт сюда значение из конфига.
	MaxToolRounds int
	// Tools — тулы агента. В слайсе 0 пусто, дальше сюда придут toolkit-ы
	// DM и летописца.
	Tools []tools.Tool
}

// New создаёт DM-агента поверх выбранной модели.
func New(model agoconfig.ModelConfig, opts Options) *agent.Agent {
	agentOpts := []agent.Option{agent.WithConfig(model)}
	if len(opts.Tools) > 0 {
		agentOpts = append(agentOpts, agent.WithTools(opts.Tools...))
	}
	if opts.MaxToolRounds > 0 {
		agentOpts = append(agentOpts, agent.WithMaxToolRounds(opts.MaxToolRounds))
	}
	return agent.New("dm", systemPrompt, agentOpts...)
}
