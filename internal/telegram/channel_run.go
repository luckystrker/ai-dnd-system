package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/luckystrker/ago/events"
	"github.com/luckystrker/ago/stream"

	"github.com/luckystriker/ai-dnd-system/internal/store"
)

// Run запускает канал: снимает вебхук, дочитывает необработанные апдейты из
// durable-inbox и уходит в long-poll до отмены ctx. После отмены очередь
// останавливается через Stop.
func (c *Channel) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	c.runCancel = cancel
	c.mu.Unlock()
	defer cancel()

	if err := c.api.DeleteWebhook(runCtx); err != nil {
		return fmt.Errorf("telegram: deleteWebhook: %w", err)
	}
	offset, err := c.store.TelegramOffset()
	if err != nil {
		return err
	}
	// После рестарта сначала дочитываем необработанное: offset уже сдвинут
	// при записи в inbox, поэтому Telegram эти апдейты не повторит.
	pending, err := c.store.PendingTelegramUpdates()
	if err != nil {
		return err
	}
	for _, item := range pending {
		if runCtx.Err() != nil {
			return nil
		}
		c.recoverUpdate(item)
	}
	return c.poll(runCtx, offset)
}

// Stop останавливает очередь и агентов чатов: идущие ходы дожидаются, при
// исчерпании ctx — отменяются.
func (c *Channel) Stop(ctx context.Context) error {
	c.mu.Lock()
	cancel := c.runCancel
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if err := c.queue.Stop(ctx); err != nil {
		return err
	}
	c.mu.Lock()
	sessions := make([]*chatSession, 0, len(c.chats))
	for _, session := range c.chats {
		sessions = append(sessions, session)
	}
	c.chats = map[string]*chatSession{}
	c.mu.Unlock()
	for _, session := range sessions {
		if err := session.dm.Close(); err != nil {
			c.log.Warn("telegram: close agent", "chat", session.key, "error", err)
		}
	}
	return nil
}

// poll — long-poll getUpdates. Offset сохраняется сразу после записи апдейта
// в inbox: падение между этими шагами даёт повторную доставку, которую
// отсекает дедуп по update_id.
func (c *Channel) poll(ctx context.Context, offset int64) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		updates, err := c.api.GetUpdates(ctx, offset, c.pollTimeout, []string{"message", "callback_query"})
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			c.log.Warn("telegram: getUpdates failed", "error", err)
			if !sleepCtx(ctx, c.errorBackoff) {
				return nil
			}
			continue
		}
		for _, raw := range updates {
			if ctx.Err() != nil {
				return nil
			}
			if len(raw.Payload) > c.maxUpdateBytes {
				c.log.Warn("telegram: update too large; skipped", "update", raw.UpdateID, "bytes", len(raw.Payload))
				offset = raw.UpdateID + 1
				continue
			}
			if err := c.ingest(raw); err != nil {
				c.log.Error("telegram: update ingest failed", "update", raw.UpdateID, "error", err)
				continue
			}
			offset = raw.UpdateID + 1
			if err := c.store.SaveTelegramOffset(offset); err != nil {
				c.log.Error("telegram: save offset failed", "offset", offset, "error", err)
			}
		}
	}
}

// ingest пишет апдейт в inbox до обработки и маршрутизирует его.
func (c *Channel) ingest(raw RawUpdate) error {
	var update Update
	if err := json.Unmarshal(raw.Payload, &update); err != nil {
		// Битый апдейт не должен блокировать offset: записываем и считаем
		// обработанным.
		if _, err := c.store.SaveTelegramUpdate(raw.UpdateID, "invalid", string(raw.Payload)); err != nil {
			return err
		}
		return c.store.MarkTelegramUpdateProcessed(raw.UpdateID)
	}
	chatKey := "unknown"
	if update.Message != nil {
		chatKey = ChatKey(update.Message)
	} else if update.CallbackQuery != nil {
		chatKey = "callback"
	}
	inserted, err := c.store.SaveTelegramUpdate(raw.UpdateID, chatKey, string(raw.Payload))
	if err != nil {
		return err
	}
	if !inserted {
		// Дедуп: этот update_id уже обработан или стоит в очереди.
		return nil
	}
	return c.route(raw.UpdateID, &update)
}

// recoverUpdate дочитывает апдейт из inbox после рестарта.
func (c *Channel) recoverUpdate(item store.TelegramUpdate) {
	var update Update
	if err := json.Unmarshal([]byte(item.Payload), &update); err != nil {
		_ = c.store.MarkTelegramUpdateProcessed(item.UpdateID)
		return
	}
	if err := c.route(item.UpdateID, &update); err != nil {
		c.log.Error("telegram: recover update failed", "update", item.UpdateID, "error", err)
		_ = c.store.MarkTelegramUpdateProcessed(item.UpdateID)
	}
}

// route — маршрутизация разобранного апдейта: callback, /join, лимиты, гейт.
// update_id попадает в батч и помечается обработанным после хода.
func (c *Channel) route(updateID int64, update *Update) error {
	if update.CallbackQuery != nil {
		_ = c.api.AnswerCallbackQuery(context.Background(), update.CallbackQuery.ID, "Unsupported action.")
		return c.store.MarkTelegramUpdateProcessed(updateID)
	}
	message := update.Message
	if message == nil {
		return c.store.MarkTelegramUpdateProcessed(updateID)
	}
	if message.From == nil || message.From.IsBot || message.Chat.Type == "channel" {
		return c.store.MarkTelegramUpdateProcessed(updateID)
	}
	text := strings.TrimSpace(message.TextContent())
	attachments := message.Attachments()
	if text == "" && len(attachments) == 0 {
		return c.store.MarkTelegramUpdateProcessed(updateID)
	}

	now := c.clock()
	chatKey := ChatKey(message)
	userID := strconv.FormatInt(message.From.ID, 10)
	if !c.limiter.AllowChat(chatKey, c.limits, now) || !c.limiter.AllowUser(userID, c.limits, now) {
		// В TS сообщение молча терялось; теперь оно остаётся в транскрипте:
		// DM видит, что игроки писали, но LLM не будится.
		c.recordTranscript(Incoming{Message: message, UpdateID: updateID})
		c.log.Info("telegram: rate limited", "chat", chatKey, "user", userID)
		return c.store.MarkTelegramUpdateProcessed(updateID)
	}

	if message.IsCommand(c.botUsername) && message.CommandName() == "join" {
		c.handleJoin(message)
		return c.store.MarkTelegramUpdateProcessed(updateID)
	}

	if err := c.queue.Enqueue(Incoming{Message: message, UpdateID: updateID}); err != nil {
		if errors.Is(err, ErrQueueClosed) {
			// Очередь остановлена: апдейт остаётся необработанным в inbox.
			return nil
		}
		return err
	}
	return nil
}

// handleJoin — /join без LLM: регистрирует автора в кампании чата.
func (c *Channel) handleJoin(message *Message) {
	if message.From == nil || message.From.IsBot {
		return
	}
	identity := IdentityFromMessage(message)
	campaign, err := c.store.FindByBoundChat(identity.ChatID, TopicOf(message), true)
	if err != nil {
		c.log.Error("telegram: /join lookup failed", "chat", identity.ChatID, "error", err)
		return
	}
	if campaign == nil {
		c.postText(message, "В этом чате нет кампании, к которой можно присоединиться.")
		return
	}
	before := false
	for _, member := range campaign.Members {
		if member.UserID == identity.UserID {
			before = true
			break
		}
	}
	var name *string
	if identity.Name != "" {
		name = &identity.Name
	}
	var username *string
	if identity.Username != "" {
		username = &identity.Username
	}
	updated, err := c.store.AutoRegister(campaign.ID, store.NewMemberInput{
		UserID: identity.UserID, Name: name, Username: username,
	})
	if err != nil {
		c.log.Error("telegram: /join failed", "campaign", campaign.ID, "error", err)
		c.postText(message, "Не получилось присоединиться к кампании. Попробуй ещё раз.")
		return
	}
	after := false
	for _, member := range updated.Members {
		if member.UserID == identity.UserID {
			after = true
			break
		}
	}
	switch {
	case before:
		c.postText(message, fmt.Sprintf("Ты уже участвуешь в кампании «%s».", campaign.Title))
	case after:
		c.postText(message, fmt.Sprintf("Ты присоединился к кампании «%s»! Теперь можно создать персонажа.", campaign.Title))
	default:
		c.postText(message, fmt.Sprintf("Партия кампании «%s» заполнена (максимум %d игроков).", campaign.Title, store.MaxParty))
	}
}

// postText отправляет служебный текст в чат сообщения.
func (c *Channel) postText(message *Message, text string) {
	if err := c.api.SendRichText(context.Background(), ChatIDString(message.Chat.ID), TopicOf(message), text); err != nil {
		c.log.Warn("telegram: send failed", "chat", message.Chat.ID, "error", err)
	}
}

// recordTranscript пишет сообщение в транскрипт кампании без вызова LLM:
// гейт и rate-limit не теряют переписку. Ключ записи — update_id, поэтому
// повторная доставка после падения процесса не плодит строки.
func (c *Channel) recordTranscript(item Incoming) {
	message := item.Message
	if c.store == nil || message == nil || message.From == nil {
		return
	}
	identity := IdentityFromMessage(message)
	campaign, err := FindCampaignForIdentity(c.store, identity)
	if err != nil || campaign == nil {
		return
	}
	author := identity.Username
	if author == "" {
		author = identity.Name
	}
	if author == "" {
		author = identity.UserID
	}
	day := 1
	if campaign.CurrentDay != nil {
		day = *campaign.CurrentDay
	}
	text := strings.TrimSpace(message.TextContent())
	if text == "" {
		for _, attachment := range message.Attachments() {
			text = "[вложение: " + attachment.FileName + "]"
			break
		}
	}
	if text == "" {
		return
	}
	entry := store.TranscriptEntry{Kind: "player", Text: text}
	if author != "" {
		entry.Author = &author
	}
	if item.UpdateID > 0 {
		eventID := fmt.Sprintf("tg-update:%d", item.UpdateID)
		entry.EventID = &eventID
	}
	if err := c.store.AppendTranscriptEntry(campaign.Slug, day, entry); err != nil {
		c.log.Warn("telegram: transcript write failed", "campaign", campaign.ID, "error", err)
	}
}

// handleBatch — один склеенный ход чата: гейт, сессия, вызов DM, ответ.
func (c *Channel) handleBatch(ctx context.Context, batch Batch) error {
	last := batch.Last()
	if last == nil {
		return nil
	}
	updateIDs := make([]int64, 0, len(batch.Messages))
	for _, item := range batch.Messages {
		updateIDs = append(updateIDs, item.UpdateID)
	}
	markProcessed := func() {
		for _, updateID := range updateIDs {
			if err := c.store.MarkTelegramUpdateProcessed(updateID); err != nil {
				c.log.Warn("telegram: mark processed failed", "update", updateID, "error", err)
			}
		}
	}

	identity := IdentityFromMessage(last)
	if identity.UserID == "" {
		markProcessed()
		return nil
	}
	campaign, err := FindCampaignForIdentity(c.store, identity)
	if err != nil {
		c.log.Warn("telegram: campaign lookup failed", "chat", batch.Key, "error", err)
	}

	if !c.gate.ShouldReply(batch.Key, batch.Messages) {
		// Переписка игроков между собой и OOC: в транскрипт, но без LLM.
		for _, item := range batch.Messages {
			c.recordTranscript(item)
		}
		markProcessed()
		return nil
	}

	chat, err := c.chatSession(batch.Key)
	if err != nil {
		markProcessed()
		return err
	}
	session := SessionFor(campaign, identity)
	chat.setSession(session)

	c.typing(ctx, batch.ChatID(), batch.ThreadID())
	parts := c.turnParts(ctx, batch)
	if len(parts) == 0 {
		markProcessed()
		return nil
	}
	turn := NewTurn()
	answer, err := chat.dm.AskInputs(WithTurn(ctx, turn), parts...)
	turn.Close()
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			// Остановка процесса: апдейт остаётся необработанным, после
			// рестарта его дочитают из inbox.
			return err
		}
		c.log.Error("telegram: turn failed", "chat", batch.Key, "error", err)
		markProcessed()
		return nil
	}
	answer = strings.TrimSpace(answer)
	if answer != "" {
		if err := c.api.SendRichText(ctx, batch.ChatID(), batch.ThreadID(), answer); err != nil {
			// Ход уже выполнен (и записан в память): повторная обработка
			// апдейта продублировала бы действия. Сбой отправки логируем.
			c.log.Error("telegram: reply send failed", "chat", batch.Key, "error", err)
		}
		c.gate.MarkBotReply(batch.Key)
	}
	markProcessed()
	return nil
}

// typing показывает «печатает…» с коротким таймаутом: индикатор не критичен.
func (c *Channel) typing(ctx context.Context, chatID string, threadID *int) {
	sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := c.api.SendChatAction(sendCtx, chatID, threadID, "typing"); err != nil && ctx.Err() == nil {
		c.log.Debug("telegram: typing failed", "error", err)
	}
}

// turnParts собирает ввод модели: строка «Имя: текст» на сообщение, затем
// вложения как BinaryInput. Говорящий для авторизации — последний автор.
func (c *Channel) turnParts(ctx context.Context, batch Batch) []events.Input {
	lines := make([]string, 0, len(batch.Messages))
	attachments := make([]Attachment, 0)
	for _, item := range batch.Messages {
		message := item.Message
		if message == nil || message.From == nil {
			continue
		}
		speaker := message.From.DisplayName()
		if speaker == "" {
			speaker = message.From.Username
		}
		if speaker == "" {
			speaker = strconv.FormatInt(message.From.ID, 10)
		}
		text := strings.TrimSpace(message.TextContent())
		if text != "" {
			lines = append(lines, speaker+": "+text)
		}
		for _, attachment := range message.Attachments() {
			label := string(attachment.Kind)
			if attachment.FileName != "" {
				label += ": " + attachment.FileName
			}
			lines = append(lines, fmt.Sprintf("%s: [вложение %s]", speaker, label))
			attachments = append(attachments, attachment)
		}
	}
	parts := make([]events.Input, 0, 1+len(attachments))
	if len(lines) > 0 {
		parts = append(parts, events.TextInput{Content: strings.Join(lines, "\n")})
	}
	for _, attachment := range attachments {
		parts = append(parts, c.downloadAttachment(ctx, attachment)...)
	}
	return parts
}

// downloadAttachment скачивает вложение с лимитом TS (25 МБ); ошибка не
// ломает ход — модель продолжает по тексту.
func (c *Channel) downloadAttachment(ctx context.Context, attachment Attachment) []events.Input {
	info, err := c.api.GetFile(ctx, attachment.FileID)
	if err != nil {
		c.log.Warn("telegram: getFile failed", "file", attachment.FileID, "error", err)
		return nil
	}
	if info.FileSize > c.maxAttachmentBytes {
		c.log.Warn("telegram: attachment too large", "file", attachment.FileName, "size", info.FileSize)
		return nil
	}
	data, err := c.api.DownloadFile(ctx, info, c.maxAttachmentBytes)
	if err != nil {
		c.log.Warn("telegram: attachment download failed", "file", attachment.FileName, "error", err)
		return nil
	}
	kind := events.BinaryDocument
	switch attachment.Kind {
	case AttachmentImage:
		kind = events.BinaryImage
	case AttachmentAudio:
		kind = events.BinaryAudio
	case AttachmentVideo:
		kind = events.BinaryVideo
	}
	return []events.Input{events.BinaryInput{
		Data: data, MediaType: attachment.MimeType, Kind: kind,
		VendorMetadata: map[string]any{"file_name": attachment.FileName},
	}}
}

// chatSession открывает (один раз) DM-агента чата на durable-потоке.
func (c *Channel) chatSession(chatKey string) (*chatSession, error) {
	c.mu.Lock()
	if existing, ok := c.chats[chatKey]; ok {
		c.mu.Unlock()
		return existing, nil
	}
	c.mu.Unlock()

	conversation := stream.NewMemoryStream(
		stream.WithStreamID(StreamID(chatKey)),
		stream.WithStorage(store.NewEventStreamStorage(c.store)),
	)
	conversation.History().SetCap(DefaultHistoryEventCap)
	if _, err := RepairHistory(conversation.History()); err != nil {
		return nil, err
	}
	holder := &chatSession{key: chatKey}
	agent, err := c.newDM(chatKey, conversation, holder.session)
	if err != nil {
		return nil, err
	}
	holder.dm = agent

	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.chats[chatKey]; ok {
		_ = agent.Close()
		return existing, nil
	}
	c.chats[chatKey] = holder
	return holder, nil
}

// RepairHistory убирает хвост прерванного хода: после рестарта история
// продолжается с последнего ответа модели без висящих вызовов тулов.
//
// Ответ с tool_calls, за которым не сохранились результаты, — невалидное
// начало следующего запроса для провайдера, поэтому опорной точкой считается
// последний ModelResponse БЕЗ вызовов тулов (нормальный ход всегда
// заканчивается таким). Всё после него, включая запрос игрока оборванного
// хода, выбрасывается: апдейт дочитается из inbox и ход начнётся заново.
// Возвращает число выброшенных событий.
func RepairHistory(history *stream.History) (int, error) {
	evs, err := history.GetEvents()
	if err != nil {
		return 0, err
	}
	lastResponse := -1
	for index := len(evs) - 1; index >= 0; index-- {
		response, ok := events.AsValue(evs[index]).(events.ModelResponse)
		if !ok || len(response.ToolCalls.Calls) > 0 {
			continue
		}
		lastResponse = index
		break
	}
	if lastResponse == len(evs)-1 {
		return 0, nil
	}
	kept := evs[:lastResponse+1]
	dropped := len(evs) - len(kept)
	if err := history.Replace(kept); err != nil {
		return 0, err
	}
	return dropped, nil
}

func sleepCtx(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
