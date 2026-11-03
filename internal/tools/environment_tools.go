package tools

import (
	"context"
	"fmt"
	"log"

	agocontext "github.com/luckystrker/ago/context"
	agotools "github.com/luckystrker/ago/tools"

	"github.com/luckystriker/ai-dnd-system/internal/store"
)

func environmentTools(d Deps) ([]namedTool, error) {
	builders := []func() (*agotools.FunctionTool, error){}

	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("advance_day",
			"Advance the in-game day of the active campaign. Use when a new day begins in the story (overnight rest, travel, big time skip).",
			func(args advanceDayArgs, cx *agocontext.Context) (any, error) {
				// Контекст хода доходит до DayCloser: при остановке процесса
				// постановка закрытия дня не переживает отмену.
				ctx := context.Background()
				if cx != nil {
					ctx = cx.TurnContext()
				}
				return d.advanceDay(ctx, args.Session, args)
			})
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("advance_time",
			"Advance the in-game time of day by one phase (morning -> day -> evening -> night -> morning). Wrapping past night to morning means a new day starts — call advance_day next. Night imposes disadvantage on sight-based checks (Perception/Investigation/Survival) and advantage on Stealth. Optionally set the in-game calendar date string.",
			func(args advanceTimeArgs) (any, error) { return d.advanceTime(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("set_weather",
			`Set the current weather/environment of the campaign (e.g. «ясно», «туман», «шторм», «сильный ветер»). Weather has mechanical effects: fog/reduced visibility gives disadvantage on sight-based checks (Perception/Investigation/Survival); strong wind/storm gives disadvantage on ranged attacks.`,
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

func (d Deps) advanceDay(ctx context.Context, sess *Session, args advanceDayArgs) (any, error) {
	campaign, err := d.campaign(sess)
	if err != nil {
		return nil, err
	}
	// previous читается внутри блокировки store: два одновременных вызова не
	// закроют один и тот же день дважды.
	updated, previous, err := d.DB.AdvanceDayForMember(campaign.ID)
	if err != nil {
		return nil, fromStoreError(err)
	}
	day := 1
	if updated.CurrentDay != nil {
		day = *updated.CurrentDay
	}
	if err := d.DB.EnsureDay(updated.Slug, day, args.Note, nil); err != nil {
		// День уже сдвинут: возвращать ошибку нельзя, иначе модель повторит
		// advance_day и сдвинет день ещё раз. Закрытие предыдущего дня всё
		// равно ставим в очередь ниже; заметка нового дня — не критична.
		log.Printf("tools: advance_day: EnsureDay failed for campaign %s day %d: %v", updated.ID, day, err)
	}
	closed := false
	queued := false
	closeFailed := false
	if d.DayCloser != nil {
		if err := d.DayCloser.CloseDay(ctx, updated, previous); err != nil {
			// День уже сдвинут: не просим модель повторить advance_day (это
			// сдвинуло бы день ещё раз). Закрытие повторит код/очередь.
			log.Printf("tools: advance_day: DayCloser failed for campaign %s day %d: %v", updated.ID, previous, err)
			closeFailed = true
		} else {
			queued = true
		}
	}
	result := map[string]any{
		"ok": true, "campaignSlug": updated.Slug, "previousDay": previous, "day": day,
		"closed": closed, "queued": queued,
	}
	switch {
	case closeFailed:
		result["note"] = fmt.Sprintf("Наступил игровой день %d. Поставить закрытие дня %d в очередь не удалось; день уже сдвинут, повторный вызов advance_day не нужен — закрытие повторит код.", day, previous)
	case queued:
		result["note"] = fmt.Sprintf("Наступил игровой день %d. Закрытие дня %d поставлено в очередь летописца; повторный вызов advance_day не нужен.", day, previous)
	default:
		result["note"] = fmt.Sprintf("Наступил игровой день %d.", day)
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
	campaign, err := d.campaign(sess)
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
		note = "Прошла полночь — наступило утро. Теперь вызови advance_day чтобы перевести игру на новый день; закрытие старого дня выполнит код."
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
	campaign, err := d.campaign(sess)
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
