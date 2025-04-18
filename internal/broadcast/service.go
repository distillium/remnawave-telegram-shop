package broadcast

import (
	"context"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"log/slog"
	"remnawave-tg-shop-bot/internal/database"
	"remnawave-tg-shop-bot/internal/translation"
	"sync"
)

type BroadcastService struct {
	customerRepository *database.CustomerRepository
	telegramBot        *bot.Bot
	adminID            int64
	tm                 *translation.Manager
	waitingMessages    sync.Map
	pendingMessages    sync.Map
}

func NewBroadcastService(
	customerRepository *database.CustomerRepository,
	telegramBot *bot.Bot,
	adminID int64,
	tm *translation.Manager,
) *BroadcastService {
	return &BroadcastService{
		customerRepository: customerRepository,
		telegramBot:        telegramBot,
		adminID:            adminID,
		tm:                 tm,
	}
}

func (s *BroadcastService) HandleBroadcastCommand(ctx context.Context, b *bot.Bot, update *models.Update) {
	lang := update.Message.From.LanguageCode

	if update.Message == nil || update.Message.From.ID != s.adminID {
		_ = s.sendText(ctx, update.Message.Chat.ID, s.tm.GetText(lang, "broadcast_access_denied"))
		return
	}

	s.waitingMessages.Store(update.Message.Chat.ID, true)
	_ = s.sendText(ctx, update.Message.Chat.ID, s.tm.GetText(lang, "broadcast_send_prompt"))
}

func (s *BroadcastService) HandleTextMessage(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}

	chatID := update.Message.Chat.ID
	if _, waiting := s.waitingMessages.Load(chatID); !waiting {
		return
	}

	s.waitingMessages.Delete(chatID)
	s.pendingMessages.Store(chatID, *update.Message)

	lang := update.Message.From.LanguageCode
	previewText := s.tm.GetText(lang, "broadcast_preview")
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   previewText,
		ReplyMarkup: models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{
					{Text: s.tm.GetText(lang, "broadcast_send_button"), CallbackData: "broadcast_confirm"},
					{Text: s.tm.GetText(lang, "broadcast_cancel_button"), CallbackData: "broadcast_cancel"},
				},
			},
		},
	})
}

func (s *BroadcastService) HandleCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	cb := update.CallbackQuery
	lang := cb.From.LanguageCode
	chatID := cb.From.ID

	switch cb.Data {
	case "broadcast_confirm":
		raw, ok := s.pendingMessages.Load(chatID)
		if !ok {
			_ = s.sendText(ctx, chatID, s.tm.GetText(lang, "broadcast_no_message"))
			return
		}
		s.pendingMessages.Delete(chatID)

		msg := raw.(models.Message)
		s.sendToAllUsers(ctx, &msg)

		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   s.tm.GetText(lang, "broadcast_sent"),
		})
	case "broadcast_cancel":
		s.pendingMessages.Delete(chatID)
		_ = s.sendText(ctx, chatID, s.tm.GetText(lang, "broadcast_canceled"))
	}
}

func (s *BroadcastService) sendToAllUsers(ctx context.Context, msg *models.Message) {
	customers, err := s.customerRepository.FindAll(ctx)
	if err != nil {
		slog.Error("Ошибка при получении пользователей", "error", err)
		return
	}

	for _, c := range customers {
		var err error

		switch {
		case msg.Text != "":
			_, err = s.telegramBot.SendMessage(ctx, &bot.SendMessageParams{
				ChatID: c.TelegramID,
				Text:   msg.Text,
			})
		case msg.Photo != nil:
			photo := msg.Photo[len(msg.Photo)-1]
			_, err = s.telegramBot.SendPhoto(ctx, &bot.SendPhotoParams{
				ChatID:  c.TelegramID,
				Photo: &models.InputFileString{Data: photo.FileID},
				Caption: msg.Caption,
			})

		case msg.Document != nil:
			_, err = s.telegramBot.SendDocument(ctx, &bot.SendDocumentParams{
				ChatID:   c.TelegramID,
				Document: &models.InputFileString{Data: msg.Document.FileID},
				Caption:  msg.Caption,
			})

		default:
			slog.Warn("Unsupported message type for broadcast", "user_id", c.TelegramID)
			continue
		}

		if err != nil {
			slog.Error("Ошибка при отправке рассылки", "user_id", c.TelegramID, "error", err)
		}
	}
}

func (s *BroadcastService) sendText(ctx context.Context, chatID int64, text string) error {
	_, err := s.telegramBot.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   text,
	})
	return err
}
