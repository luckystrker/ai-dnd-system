package tools

import (
	"strings"

	"github.com/luckystriker/ai-dnd-system/internal/store"
)

// STYLE_SUFFIX — фиксированный стилистический суффикс.
const STYLE_SUFFIX = "high fantasy, cinematic lighting, detailed illustration"

// DEFAULT_NEGATIVE_PROMPT — дефолтный negative-промпт.
const DefaultNegativePrompt = "text, watermark, signature, blurry, low quality, deformed"

// ScenePromptInput — части детерминированного промпта.
type ScenePromptInput struct {
	SceneDescription string
	Appearances      []string
	Setting          string
	Theme            string
}

// BuildScenePrompt собирает промпт без LLM-вызовов.
func BuildScenePrompt(input ScenePromptInput) string {
	parts := []string{input.SceneDescription}
	parts = append(parts, input.Appearances...)
	if input.Setting != "" {
		parts = append(parts, "Setting: "+input.Setting)
	}
	if input.Theme != "" {
		parts = append(parts, "Theme: "+input.Theme)
	}
	parts = append(parts, STYLE_SUFFIX)
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		text := strings.TrimSpace(part)
		if text != "" {
			cleaned = append(cleaned, text)
		}
	}
	return strings.Join(cleaned, ", ")
}

// AppearancesForCharacters сопоставляет имена с листами без учёта регистра.
func AppearancesForCharacters(names []string, sheets []store.CharacterSheet) []string {
	appearances := make([]string, 0, len(names))
	for _, name := range names {
		needle := strings.ToLower(strings.TrimSpace(name))
		if needle == "" {
			continue
		}
		for _, sheet := range sheets {
			if strings.ToLower(sheet.Name) == needle && sheet.Appearance != nil && *sheet.Appearance != "" {
				appearances = append(appearances, *sheet.Appearance)
				break
			}
		}
	}
	return appearances
}
