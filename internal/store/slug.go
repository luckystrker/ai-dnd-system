package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

var cyrillicToLatin = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e",
	'ж': "zh", 'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m",
	'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u",
	'ф': "f", 'х': "h", 'ц': "ts", 'ч': "ch", 'ш': "sh", 'щ': "sch",
	'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu", 'я': "ya",
}

var (
	slugNonAlnum  = regexp.MustCompile(`[^a-z0-9]+`)
	slugDashes    = regexp.MustCompile(`^-+|-+$`)
	slugTrailDash = regexp.MustCompile(`-+$`)
	slugPattern   = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
)

// Slugify — порт slugify из store.ts: транслитерация кириллицы, нижний
// регистр, только [a-z0-9-], максимум 60 символов, fallback «untitled».
func Slugify(text string) string {
	var transliterated strings.Builder
	for _, r := range strings.ToLower(text) {
		if mapped, ok := cyrillicToLatin[r]; ok {
			transliterated.WriteString(mapped)
			continue
		}
		transliterated.WriteRune(r)
	}
	slug := slugNonAlnum.ReplaceAllString(transliterated.String(), "-")
	slug = slugDashes.ReplaceAllString(slug, "")
	if len(slug) > 60 {
		slug = slug[:60]
	}
	slug = slugTrailDash.ReplaceAllString(slug, "")
	if slug == "" {
		return "untitled"
	}
	return slug
}

// AssertCampaignSlug валидирует slug (в т.ч. приходящий от LLM): только
// безопасные символы, без path traversal.
func AssertCampaignSlug(slug string) error {
	if slug == "" || len(slug) > 60 || !slugPattern.MatchString(slug) {
		return storeError(fmt.Sprintf("Недопустимый slug «%s».", slug), ErrNotFound)
	}
	return nil
}

// newUUID — UUID v4 без внешней зависимости (как crypto.randomUUID).
func newUUID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		panic("store: crypto/rand: " + err.Error())
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(bytes[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]
}
