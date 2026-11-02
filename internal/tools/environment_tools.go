package tools

import (
	"context"
	"fmt"

	agotools "github.com/luckystrker/ago/tools"

	"github.com/luckystriker/ai-dnd-system/internal/store"
)

func environmentTools(d Deps) ([]namedTool, error) {
	builders := []func() (*agotools.FunctionTool, error){}

	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("advance_day",
			"Advance the in-game day of the active campaign. Use when a new day begins in the story (overnight rest, travel, big time skip).",
			func(args advanceDayArgs) (any, error) { return d.advanceDay(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("advance_time",
			"Advance the in-game time of day by one phase (morning -> day -> evening -> night -> morning). Wrapping past night to morning means a new day starts — call advance_day next. Night imposes disadvantage on sight-based checks (Perception/Investigation/Survival) and advantage on Stealth. Optionally set the in-game calendar date string.",
			func(args advanceTimeArgs) (any, error) { return d.advanceTime(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("set_weather",
			"Set the current weather/environment of the campaign (e.g. «ясно» «туман» «шторм» «сильный ветер»). Weather has mechanical effects: fog/reduced visibility gives disadvantage on sight-based checks (Perception/Investigation/Survival); strong wind/storm gives disadvantage on ranged attacks.",
			func(args setWeatherArgs) (any, error) { return d.setWeather(args.Session, args) })
	})

	out := make([]namedTool, 0, len(builders))
	for _, build := range builders {
		created, err := build()
		if err != nil {
			return nil, err
		}
		out = append(out, namedTool{name: created.Name(), tool: created})
	}
	return out, nil
}

type advanceDayArgs struct {
	Note    *string  `json:"note" agschema:"description=Short note for the new day: time of day place weather"`
	Session *Session `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) advanceDay(sess *Session, args advanceDayArgs) (any, error) {
	campaign, err := d.campaignForWrite(sess)
	if err != nil {
		return nil, err
	}
	previous := 1
	if campaign.CurrentDay != nil {
		previous = *campaign.CurrentDay
	}
	updated, err := d.DB.AdvanceDayForMember(campaign.ID)
	if err != nil {
		return nil, fromStoreError(err)
	}
	day := 1
	if updated.CurrentDay != nil {
		day = *updated.CurrentDay
	}
	if err := d.DB.EnsureDay(updated.Slug, day, args.Note, nil); err != nil {
		return nil, fromStoreError(err)
	}
	closed := false
	if d.DayCloser != nil {
		if err := d.DayCloser.CloseDay(context.Background(), updated, previous); err != nil {
			return nil, errUnavailable("Не удалось закрыть день "+fmt.Sprint(previous)+" летописцем.",
				"Day is advanced but the chronicler failed; retry closing the day or continue and retry later.")
		}
		closed = true
	}
	result := map[string]any{
		"ok": true, "campaignSlug": updated.Slug, "previousDay": previous, "day": day,
	}
	if closed {
		result["note"] = fmt.Sprintf("Наступил игровой день %d. День %d закрыт летописцем.", day, previous)
	} else {
		result["note"] = fmt.Sprintf("Наступил игровой день %d. Теперь вызови субагента chronicler, чтобы закрыть день %d: передай ему slug кампании номер закрытого дня и попроси записать саммари дня ключевые события и изменения NPC и персонажей.", day, previous)
	}
	return result, nil
}

type advanceTimeArgs struct {
	TimeOfDay  *string  `json:"timeOfDay" agschema:"enum=morning|day|evening|night,description=Explicit time of day; otherwise the next phase"`
	InGameDate *string  `json:"inGameDate" agschema:"description=Date in the world calendar"`
	Session    *Session `json:"-" aginject:"ttrpg.session"`
}

var dayOrder = []store.TimeOfDay{store.TimeMorning, store.TimeDay, store.TimeEvening, store.TimeNight}

func (d Deps) advanceTime(sess *Session, args advanceTimeArgs) (any, error) {
	campaign, err := d.campaignForWrite(sess)
	if err != nil {
		return nil, err
	}
	current := store.TimeMorning
	if campaign.TimeOfDay != nil {
		current = *campaign.TimeOfDay
	}
	next := current
	if args.TimeOfDay != nil {
		next = store.TimeOfDay(*args.TimeOfDay)
		switch next {
		case store.TimeMorning, store.TimeDay, store.TimeEvening, store.TimeNight:
		default:
			return nil, errInvalid("Недопустимое время суток «"+*args.TimeOfDay+"».",
				"Use morning, day, evening or night.")
		}
	} else {
		for index, phase := range dayOrder {
			if phase == current {
				next = dayOrder[(index+1)%len(dayOrder)]
				break
			}
		}
	}
	updated, err := d.DB.SetEnvironment(campaign.ID, store.EnvironmentPatch{
		TimeOfDay: &next, InGameDate: args.InGameDate,
	})
	if err != nil {
		return nil, fromStoreError(err)
	}
	note := "Игровое время: " + timeLabel(next) + "."
	if next == store.TimeMorning && current != store.TimeMorning {
		note = "Прошла полночь — наступило утро. Теперь вызови advance_day чтобы перевести игру на новый день (и закрыть старый через chronicler)."
	}
	return map[string]any{
		"ok": true, "timeOfDay": string(next), "inGameDate": nullableString(updated.InGameDate), "note": note,
	}, nil
}

func timeLabel(value store.TimeOfDay) string {
	switch value {
	case store.TimeMorning:
		return "утро"
	case store.TimeDay:
		return "день"
	case store.TimeEvening:
		return "вечер"
	default:
		return "ночь"
	}
}

type setWeatherArgs struct {
	Weather string   `json:"weather" agschema:"description=Current weather or environment: «ясно» «туман» «дождь» «шторм» «сильный ветер»,required"`
	Session *Session `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) setWeather(sess *Session, args setWeatherArgs) (any, error) {
	campaign, err := d.campaignForWrite(sess)
	if err != nil {
		return nil, err
	}
	if _, err := d.DB.SetEnvironment(campaign.ID, store.EnvironmentPatch{Weather: &args.Weather}); err != nil {
		return nil, fromStoreError(err)
	}
	return map[string]any{
		"ok": true, "weather": args.Weather,
		"note": "Погода установлена: " + args.Weather + ".",
	}, nil
}
