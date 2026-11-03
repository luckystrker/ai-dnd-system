package tools

import (
	"strings"

	"github.com/luckystriker/ai-dnd-system/internal/store"
)

// tableTools — тулы за столом без побочных эффектов в памяти. Сейчас это
// явный структурный сигнал «ход ждёт решения другого игрока»: гарантия в
// internal/dm дописывает закрывающий вопрос, только если модель его объявила
// вызовом ask_player (PLAN 41).
func tableTools(d Deps) ([]namedTool, error) {
	ask, err := tool("ask_player",
		"Hand the decision to another player's character: call this in the same turn when your reply ends by asking that character what they do, and pass their character name. The reply must end with an open question to that character; the system appends the question if you forgot it.",
		func(args askPlayerArgs) (any, error) { return d.askPlayer(args) })
	if err != nil {
		return nil, err
	}
	return []namedTool{{name: "ask_player", tool: ask}}, nil
}

// askPlayerArgs — персонаж, чьё решение ждёт ход.
type askPlayerArgs struct {
	Session   *Session `json:"-" aginject:"ttrpg.session"`
	Character string   `json:"character" agschema:"description=Character name of the other player whose decision you wait for,required"`
}

// askPlayer подтверждает передачу решения другому игроку и не пишет память:
// значение сигнала — в самом вызове тула, который видит middleware.
func (d Deps) askPlayer(args askPlayerArgs) (any, error) {
	if err := d.requireUser(args.Session); err != nil {
		return nil, err
	}
	campaign, err := d.campaign(args.Session)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(args.Character)
	if name == "" {
		return nil, errInvalid("Не указано имя персонажа.",
			"Pass the name of the other player's character whose decision the turn waits for.")
	}
	var sheet *store.CharacterSheet
	sheet, err = d.characterByName(campaign.ID, name)
	if err != nil {
		return nil, err
	}
	if sheet == nil {
		return nil, errNotFound("Персонаж «"+name+"» не найден в партии.",
			"Pass a party character name from the memory block.")
	}
	if sheet.OwnerUserID == args.Session.UserID {
		return nil, errInvalid("Это персонаж говорящего игрока: ход не ждёт решения другого игрока.",
			"Call ask_player only for another player's character; if the decision is the acting player's own, end the reply with the question without this tool.")
	}
	return map[string]any{
		"ok":          true,
		"waiting_for": sheet.Name,
		"hint":        "End the reply with an open question to " + sheet.Name + ".",
	}, nil
}
