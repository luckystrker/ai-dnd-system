package tools

import (
	"context"
	"log"

	agocontext "github.com/luckystrker/ago/context"
	agotools "github.com/luckystrker/ago/tools"
)

const telegramCaptionMaxLength = 1024

func illustrateTool(d Deps) (*agotools.FunctionTool, error) {
	return tool("illustrate_scene",
		"Generate an illustration of the current scene and post it to the campaign chat. Works only inside a chat with an active campaign. Call it when a significant new scene begins. Describe the scene in English. If it fails, just continue narrating in text and do not mention the failure.",
		func(args illustrateArgs, cx *agocontext.Context) (any, error) {
			// Контекст хода, а не Background: дедлайн/отмена хода должны
			// доходить до Runware и отправки в чат, иначе тул доставит
			// картинку после нейтрального ответа игрокам.
			ctx := context.Background()
			if cx != nil {
				ctx = cx.TurnContext()
			}
			return d.illustrateScene(ctx, args.Session, args)
		})
}

type illustrateArgs struct {
	SceneDescription string   `json:"sceneDescription" agschema:"description=Brief English description of the scene: place action atmosphere; becomes the image caption,required"`
	Characters       []string `json:"characters" agschema:"description=Player character names in the frame; their appearance is included in the prompt"`
	Session          *Session `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) illustrateScene(ctx context.Context, sess *Session, args illustrateArgs) (any, error) {
	campaign, err := d.campaign(sess)
	if err != nil {
		return nil, err
	}
	if campaign.BoundChat == nil {
		return nil, errUnavailable("Иллюстрации доступны только в рамках кампании привязанной к чату.",
			"Bind the campaign to a chat with start_campaign before illustrating scenes.")
	}
	if d.Images == nil {
		return nil, errUnavailable("Генерация картинок не настроена (нет RUNWARE_API_KEY). Продолжай текстом.",
			"Set RUNWARE_API_KEY on the bot host; otherwise continue with text narration.")
	}
	sheets, err := d.DB.ListCharacters(campaign.ID)
	if err != nil {
		return nil, fromStoreError(err)
	}
	prompt := BuildScenePrompt(ScenePromptInput{
		SceneDescription: args.SceneDescription,
		Appearances:      AppearancesForCharacters(args.Characters, sheets),
		Setting:          campaign.Setting,
		Theme:            campaign.Theme,
	})
	imageURL, err := d.Images.Generate(ctx, prompt)
	if err != nil {
		log.Printf("tools: illustrate_scene: generation failed for campaign %s: %v", campaign.ID, err)
		return nil, errUnavailable("Не получилось сгенерировать картинку. Продолжи сцену текстом.",
			"Continue the scene in text; retry illustration only for a new significant scene.")
	}
	if d.Poster == nil {
		return nil, errUnavailable("Картинка сгенерирована но отправка в чат не настроена. Продолжи текстом.",
			"The Telegram image poster is not wired yet; continue in text.")
	}
	caption := truncateRunes(args.SceneDescription, telegramCaptionMaxLength)
	if err := d.Poster.PostImage(ctx, campaign.BoundChat.ChatID, campaign.BoundChat.MessageThreadID, imageURL, caption); err != nil {
		log.Printf("tools: illustrate_scene: post failed for campaign %s: %v", campaign.ID, err)
		return nil, errUnavailable("Картинка сгенерирована но отправить её в чат не вышло. Продолжи текстом.",
			"Continue the scene in text; the image URL is available in the result.")
	}
	return map[string]any{
		"ok": true, "imageUrl": imageURL,
		"note": "Иллюстрация отправлена в чат кампании. Продолжай повествование не пересказывая картинку.",
	}, nil
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
