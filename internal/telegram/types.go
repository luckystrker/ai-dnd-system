package telegram

import (
	"strconv"
	"strings"
)

// RawUpdate — один апдейт long-poll: сырой JSON и его update_id. Сырой payload
// пишется в durable-inbox до разбора и обработки.
type RawUpdate struct {
	UpdateID int64
	Payload  []byte
}

// Update — разобранный апдейт. Поддерживаются сообщения и callback-кнопки;
// остальные типы (edited_message, channel_post и т.п.) игнорируются.
type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

// CallbackQuery — нажатие инлайн-кнопки (бот отвечает «Unsupported action.»).
type CallbackQuery struct {
	ID   string `json:"id"`
	Data string `json:"data"`
	From *User  `json:"from"`
}

// User — автор сообщения.
type User struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
}

// DisplayName — имя для транскрипта и регистрации: имя+фамилия, иначе username.
func (u *User) DisplayName() string {
	if u == nil {
		return ""
	}
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if name != "" {
		return name
	}
	return u.Username
}

// Chat — чат.
type Chat struct {
	ID      int64  `json:"id"`
	Type    string `json:"type"` // private | group | supergroup | channel
	Title   string `json:"title"`
	IsForum bool   `json:"is_forum"`
}

// PhotoSize — вариант фотографии; берём самый крупный.
type PhotoSize struct {
	FileID   string `json:"file_id"`
	FileSize int64  `json:"file_size"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

// FileMeta — общие поля файловых вложений.
type FileMeta struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	MimeType string `json:"mime_type"`
	FileSize int64  `json:"file_size"`
	Duration int    `json:"duration"`
}

// Sticker — стикер (картинка webp).
type Sticker struct {
	FileID     string `json:"file_id"`
	FileSize   int64  `json:"file_size"`
	Emoji      string `json:"emoji"`
	IsAnimated bool   `json:"is_animated"`
}

// Message — входящее сообщение.
type Message struct {
	MessageID       int64    `json:"message_id"`
	MessageThreadID *int     `json:"message_thread_id"`
	IsTopicMessage  bool     `json:"is_topic_message"`
	Date            int64    `json:"date"`
	From            *User    `json:"from"`
	Chat            Chat     `json:"chat"`
	Text            string   `json:"text"`
	Caption         string   `json:"caption"`
	ReplyToMessage  *Message `json:"reply_to_message"`

	Photo     []PhotoSize `json:"photo"`
	Document  *FileMeta   `json:"document"`
	Audio     *FileMeta   `json:"audio"`
	Voice     *FileMeta   `json:"voice"`
	Video     *FileMeta   `json:"video"`
	VideoNote *FileMeta   `json:"video_note"`
	Animation *FileMeta   `json:"animation"`
	Sticker   *Sticker    `json:"sticker"`
}

// AttachmentKind — класс вложения для binary-части хода.
type AttachmentKind string

const (
	AttachmentImage    AttachmentKind = "image"
	AttachmentAudio    AttachmentKind = "audio"
	AttachmentVideo    AttachmentKind = "video"
	AttachmentDocument AttachmentKind = "document"
)

// Attachment — вложение сообщения, которое можно скачать через getFile.
type Attachment struct {
	FileID   string
	FileName string
	MimeType string
	Size     int64
	Kind     AttachmentKind
}

// Attachments собирает вложения сообщения в порядке приоритета: фото/стикер,
// документ, аудио, видео. Пустой список — сообщение без файлов.
func (m *Message) Attachments() []Attachment {
	if m == nil {
		return nil
	}
	out := []Attachment{}
	if len(m.Photo) > 0 {
		best := m.Photo[0]
		for _, photo := range m.Photo[1:] {
			if photo.Width*photo.Height > best.Width*best.Height {
				best = photo
			}
		}
		out = append(out, Attachment{
			FileID: best.FileID, FileName: "photo.jpg", MimeType: "image/jpeg",
			Size: best.FileSize, Kind: AttachmentImage,
		})
	}
	if m.Sticker != nil && m.Sticker.FileID != "" {
		out = append(out, Attachment{
			FileID: m.Sticker.FileID, FileName: "sticker.webp", MimeType: "image/webp",
			Size: m.Sticker.FileSize, Kind: AttachmentImage,
		})
	}
	appendFile := func(meta *FileMeta, fallbackName, fallbackMime string, kind AttachmentKind) {
		if meta == nil || meta.FileID == "" {
			return
		}
		name := meta.FileName
		if name == "" {
			name = fallbackName
		}
		mime := meta.MimeType
		if mime == "" {
			mime = fallbackMime
		}
		out = append(out, Attachment{
			FileID: meta.FileID, FileName: name, MimeType: mime, Size: meta.FileSize, Kind: kind,
		})
	}
	appendFile(m.Document, "document", "application/octet-stream", AttachmentDocument)
	appendFile(m.Audio, "audio.mp3", "audio/mpeg", AttachmentAudio)
	appendFile(m.Voice, "voice.ogg", "audio/ogg", AttachmentAudio)
	appendFile(m.Video, "video.mp4", "video/mp4", AttachmentVideo)
	appendFile(m.VideoNote, "video-note.mp4", "video/mp4", AttachmentVideo)
	appendFile(m.Animation, "animation.mp4", "video/mp4", AttachmentVideo)
	return out
}

// TextContent — текст сообщения или подпись к вложению.
func (m *Message) TextContent() string {
	if m == nil {
		return ""
	}
	if strings.TrimSpace(m.Text) != "" {
		return m.Text
	}
	return m.Caption
}

// IsCommand сообщение-команда (/join, /join@bot). target — адресат после @;
// он должен совпадать с username бота (регистр не важен) или отсутствовать.
func (m *Message) IsCommand(botUsername string) bool {
	text := strings.TrimSpace(m.TextContent())
	if !strings.HasPrefix(text, "/") {
		return false
	}
	head := text
	if index := strings.IndexAny(head, " \t\n"); index >= 0 {
		head = head[:index]
	}
	command := strings.TrimPrefix(head, "/")
	target := ""
	if index := strings.Index(command, "@"); index >= 0 {
		target = command[index+1:]
	}
	if target == "" {
		return true
	}
	return botUsername != "" && strings.EqualFold(target, botUsername)
}

// CommandName — имя команды без слэша и адресата (lowercase), "" — не команда.
func (m *Message) CommandName() string {
	text := strings.TrimSpace(m.TextContent())
	if !strings.HasPrefix(text, "/") {
		return ""
	}
	head := text
	if index := strings.IndexAny(head, " \t\n"); index >= 0 {
		head = head[:index]
	}
	name := strings.TrimPrefix(head, "/")
	if index := strings.Index(name, "@"); index >= 0 {
		name = name[:index]
	}
	return strings.ToLower(name)
}

// FileInfo — результат getFile.
type FileInfo struct {
	FileID   string `json:"file_id"`
	FileSize int64  `json:"file_size"`
	FilePath string `json:"file_path"`
}

// ChatIDString — строковый id чата (так он живёт в bound_chat_id и Session).
func ChatIDString(chatID int64) string { return strconv.FormatInt(chatID, 10) }
