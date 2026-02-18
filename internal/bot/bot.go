package bot

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"

	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/maniack/catwatch/internal/l10n"
	"github.com/maniack/catwatch/internal/storage"
	"github.com/sirupsen/logrus"
)

type Config struct {
	Token            string
	API              *APIClient
	Logger           *logrus.Logger
	Debug            bool
	HealthListenAddr string // e.g. ":8080"
}

type Bot struct {
	api              *tgbotapi.BotAPI
	client           *APIClient
	log              *logrus.Logger
	states           map[int64]*ConversationState
	tokens           map[int64]string
	healthListenAddr string
}

type ConversationState struct {
	Step                 string
	Cat                  storage.Cat
	Record               storage.Record
	CatID                string // for editing
	PhotoCount           int    // added to track media group/multiple photos
	ObservationCondition int    // added to store condition during observation flow
}

func NewBot(cfg Config) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(cfg.Token)
	if err != nil {
		return nil, err
	}
	api.Debug = cfg.Debug

	return &Bot{
		api:              api,
		client:           cfg.API,
		log:              cfg.Logger,
		states:           make(map[int64]*ConversationState),
		tokens:           make(map[int64]string),
		healthListenAddr: cfg.HealthListenAddr,
	}, nil
}

func (b *Bot) Start(ctx context.Context) error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.api.GetUpdatesChan(u)

	// Start health check server if configured
	if b.client.BaseURL != "" && b.api != nil { // Basic check
		go b.startHealthServer(ctx)
	}

	// Start reminders loop
	go b.remindersLoop(ctx)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case update := <-updates:
			b.handleUpdate(update)
		}
	}
}

func (b *Bot) handleUpdate(update tgbotapi.Update) {
	lang := "en"
	if update.Message != nil && update.Message.From != nil {
		lang = update.Message.From.LanguageCode
	} else if update.CallbackQuery != nil && update.CallbackQuery.From != nil {
		lang = update.CallbackQuery.From.LanguageCode
	}

	b.log.WithFields(logrus.Fields{
		"update_id": update.UpdateID,
		"user":      getUserInfo(update),
		"lang":      lang,
	}).Debug("received update from telegram")

	if update.Message != nil {
		b.handleMessage(update.Message, lang)
	} else if update.CallbackQuery != nil {
		b.handleCallback(update.CallbackQuery, lang)
	}
}

func getUserInfo(update tgbotapi.Update) string {
	var user *tgbotapi.User
	if update.Message != nil {
		user = update.Message.From
	} else if update.CallbackQuery != nil {
		user = update.CallbackQuery.From
	}
	if user == nil {
		return "unknown"
	}
	return fmt.Sprintf("%s %s (@%s, id:%d)", user.FirstName, user.LastName, user.UserName, user.ID)
}

func (b *Bot) handleMessage(msg *tgbotapi.Message, lang string) {
	if msg.IsCommand() {
		// Cancel current conversation if any command received
		delete(b.states, msg.Chat.ID)

		switch msg.Command() {
		case "start":
			// Register user
			name := msg.From.FirstName
			if msg.From.LastName != "" {
				name += " " + msg.From.LastName
			}
			if err := b.client.RegisterBotUser(msg.Chat.ID, name); err != nil {
				b.log.Errorf("failed to register bot user: %v", err)
			}
			b.sendWelcome(msg.Chat.ID, lang)
		case "help":
			b.sendHelp(msg.Chat.ID, lang)
		case "stop":
			if err := b.client.UnlinkBot(msg.Chat.ID); err != nil {
				b.log.Errorf("failed to unlink bot: %v", err)
			}
			delete(b.tokens, msg.Chat.ID)
			b.reply(msg.Chat.ID, l10n.T(lang, "msg_logged_out"))
		case "delete_me":
			token, ok := b.ensureAuth(msg.Chat.ID, lang)
			if !ok {
				return
			}
			if err := b.client.DeleteUser(token); err != nil {
				b.log.Errorf("failed to delete user via bot: %v", err)
				b.reply(msg.Chat.ID, l10n.T(lang, "err_api"))
			} else {
				delete(b.tokens, msg.Chat.ID)
				b.reply(msg.Chat.ID, l10n.T(lang, "msg_user_deleted"))
			}
		case "cats":
			b.sendCatsList(msg.Chat.ID, lang)
		case "add_cat":
			b.states[msg.Chat.ID] = &ConversationState{Step: "add_name"}
			b.replyWithKeyboard(msg.Chat.ID, l10n.T(lang, "msg_add_cat_title"), b.cancelKeyboard(lang))
		case "cancel":
			delete(b.states, msg.Chat.ID)
			b.sendMainMenu(msg.Chat.ID, lang, l10n.T(lang, "msg_op_cancelled"))
		default:
			b.sendMainMenu(msg.Chat.ID, lang, l10n.T(lang, "msg_unknown_cmd"))
		}
		return
	}

	state, ok := b.states[msg.Chat.ID]
	if ok {
		b.log.WithFields(logrus.Fields{
			"chat_id": msg.Chat.ID,
			"step":    state.Step,
		}).Debug("handling conversation step")
		b.handleConversation(msg, state, lang)
		return
	}

	// Handle text buttons (like BotFather)
	txt := strings.TrimSpace(msg.Text)
	switch {
	case txt == l10n.T("en", "menu_cats") || txt == l10n.T("ru", "menu_cats") || txt == "Cats":
		b.sendCatsList(msg.Chat.ID, lang)
		return
	case txt == l10n.T("en", "menu_add_cat") || txt == l10n.T("ru", "menu_add_cat") || txt == "Add cat":
		b.states[msg.Chat.ID] = &ConversationState{Step: "add_name"}
		b.replyWithKeyboard(msg.Chat.ID, l10n.T(lang, "msg_add_cat_title"), b.cancelKeyboard(lang))
		return
	case txt == l10n.T("en", "menu_upcoming") || txt == l10n.T("ru", "menu_upcoming") || txt == "Upcoming":
		b.sendUpcomingEvents(msg.Chat.ID, lang)
		return
	case txt == l10n.T("en", "menu_help") || txt == l10n.T("ru", "menu_help") || txt == "Help":
		b.sendHelp(msg.Chat.ID, lang)
		return
	case txt == l10n.T("en", "menu_cancel") || txt == l10n.T("ru", "menu_cancel") || txt == "Cancel":
		delete(b.states, msg.Chat.ID)
		b.sendMainMenu(msg.Chat.ID, lang, l10n.T(lang, "msg_op_cancelled_short"))
		return
	default:
		b.sendMainMenu(msg.Chat.ID, lang, l10n.T(lang, "msg_unknown_msg"))
	}
}

func (b *Bot) handleConversation(msg *tgbotapi.Message, state *ConversationState, lang string) {
	if strings.TrimSpace(msg.Text) == l10n.T(lang, "menu_cancel") || strings.TrimSpace(msg.Text) == "❌ Cancel" {
		delete(b.states, msg.Chat.ID)
		b.sendMainMenu(msg.Chat.ID, lang, l10n.T(lang, "msg_op_cancelled_short"))
		return
	}

	switch state.Step {
	case "add_name":
		state.Cat.Name = msg.Text
		state.Step = "add_desc"
		b.replyWithKeyboard(msg.Chat.ID, l10n.T(lang, "msg_enter_desc"), b.skipCancelKeyboard(lang))
	case "add_desc":
		if strings.ToLower(msg.Text) != l10n.T(lang, "menu_skip") && strings.ToLower(msg.Text) != "skip" {
			state.Cat.Description = msg.Text
		}
		// Finish creating
		token, ok := b.ensureAuth(msg.Chat.ID, lang)
		if !ok {
			return
		}
		newCat, err := b.client.CreateCat(state.Cat, token)
		if err != nil {
			b.log.Errorf("failed to create cat: %v", err)
			b.reply(msg.Chat.ID, l10n.T(lang, "err_create_cat"))
		} else {
			b.reply(msg.Chat.ID, l10n.T(lang, "msg_cat_added", map[string]string{"Name": newCat.Name}))
			b.sendCatDetails(msg.Chat.ID, newCat.ID, lang)
			b.sendMainMenu(msg.Chat.ID, lang, l10n.T(lang, "msg_done_next"))
		}
		delete(b.states, msg.Chat.ID)

	case "edit_name":
		state.Cat.Name = msg.Text
		b.saveCatEdit(msg.Chat.ID, state, lang)
	case "edit_desc":
		state.Cat.Description = msg.Text
		b.saveCatEdit(msg.Chat.ID, state, lang)
	case "edit_color":
		state.Cat.Color = msg.Text
		b.saveCatEdit(msg.Chat.ID, state, lang)
	case "edit_birth":
		if strings.ToLower(strings.TrimSpace(msg.Text)) == l10n.T(lang, "menu_clear") || strings.ToLower(strings.TrimSpace(msg.Text)) == "clear" {
			state.Cat.BirthDate = nil
			b.saveCatEdit(msg.Chat.ID, state, lang)
			return
		}
		// Try parse YYYY-MM-DD first
		text := strings.TrimSpace(msg.Text)
		var t time.Time
		var err error
		if len(text) == 10 && text[4] == '-' && text[7] == '-' {
			t, err = time.Parse("2006-01-02", text)
		} else {
			t, err = time.Parse(time.RFC3339, text)
		}
		if err != nil {
			b.reply(msg.Chat.ID, l10n.T(lang, "err_invalid_date"))
			return
		}
		state.Cat.BirthDate = &t
		b.saveCatEdit(msg.Chat.ID, state, lang)
	case "edit_gender":
		g := strings.ToLower(strings.TrimSpace(msg.Text))
		// We should match localized gender or english ones
		if g == l10n.T(lang, "gender_male") || g == "male" {
			state.Cat.Gender = "male"
		} else if g == l10n.T(lang, "gender_female") || g == "female" {
			state.Cat.Gender = "female"
		} else if g == l10n.T(lang, "gender_unknown") || g == "unknown" {
			state.Cat.Gender = "unknown"
		} else {
			b.reply(msg.Chat.ID, l10n.T(lang, "err_invalid_gender"))
			return
		}
		b.saveCatEdit(msg.Chat.ID, state, lang)
	case "edit_sterilized":
		val := strings.ToLower(strings.TrimSpace(msg.Text))
		state.Cat.IsSterilized = (val == strings.ToLower(l10n.T(lang, "btn_yes")) || val == "yes" || val == "true" || val == "1")
		b.saveCatEdit(msg.Chat.ID, state, lang)
	case "edit_needattention":
		val := strings.ToLower(strings.TrimSpace(msg.Text))
		state.Cat.NeedAttention = (val == strings.ToLower(l10n.T(lang, "btn_yes")) || val == "yes" || val == "true" || val == "1")
		b.saveCatEdit(msg.Chat.ID, state, lang)
	case "add_tag":
		name := strings.TrimSpace(msg.Text)
		if name == "" {
			b.reply(msg.Chat.ID, l10n.T(lang, "err_tag_empty"))
			return
		}
		state.Cat.Tags = append(state.Cat.Tags, storage.Tag{Name: name})
		b.saveCatEdit(msg.Chat.ID, state, lang)
	case "rem_tag":
		name := strings.TrimSpace(msg.Text)
		var filtered []storage.Tag
		for _, t := range state.Cat.Tags {
			if !strings.EqualFold(t.Name, name) {
				filtered = append(filtered, t)
			}
		}
		state.Cat.Tags = filtered
		b.saveCatEdit(msg.Chat.ID, state, lang)
	case "add_photo":
		if strings.TrimSpace(msg.Text) == l10n.T(lang, "menu_done") || strings.TrimSpace(msg.Text) == "✅ Done" {
			b.reply(msg.Chat.ID, l10n.T(lang, "msg_upload_complete"))
			b.sendCatDetails(msg.Chat.ID, state.CatID, lang)
			delete(b.states, msg.Chat.ID)
			return
		}

		if len(msg.Photo) == 0 {
			b.reply(msg.Chat.ID, l10n.T(lang, "msg_add_photo_prompt"))
			return
		}

		if state.PhotoCount >= 5 {
			b.reply(msg.Chat.ID, l10n.T(lang, "msg_max_photos"))
			return
		}

		if err := b.processIncomingPhoto(msg.Chat.ID, state, msg, lang); err != nil {
			b.log.Errorf("photo upload failed: %v", err)
			b.reply(msg.Chat.ID, l10n.T(lang, "err_upload_failed"))
			return
		}
		state.PhotoCount++

		// Send feedback
		b.reply(msg.Chat.ID, l10n.T(lang, "msg_photo_uploaded", map[string]int{"Count": state.PhotoCount}))

		// If reached 5, finish automatically
		if state.PhotoCount >= 5 {
			b.reply(msg.Chat.ID, l10n.T(lang, "msg_max_reached"))
			b.sendCatDetails(msg.Chat.ID, state.CatID, lang)
			delete(b.states, msg.Chat.ID)
		}
	case "add_loc":
		if strings.TrimSpace(msg.Text) == l10n.T(lang, "btn_just_seen") || strings.TrimSpace(msg.Text) == "✅ Just seen" || strings.TrimSpace(msg.Text) == "✅ Только пометку" {
			b.observeCat(msg.Chat.ID, state.CatID, lang)
			delete(b.states, msg.Chat.ID)
			b.sendMainMenu(msg.Chat.ID, lang, l10n.T(lang, "msg_done_next"))
			return
		}
		if msg.Location == nil {
			b.reply(msg.Chat.ID, l10n.T(lang, "msg_add_loc_prompt"))
			return
		}
		token, ok := b.ensureAuth(msg.Chat.ID, lang)
		if !ok {
			return
		}
		err := b.client.AddCatLocation(state.CatID, msg.Location.Latitude, msg.Location.Longitude, "", token)
		if err != nil {
			b.log.Errorf("add location: %v", err)
			b.reply(msg.Chat.ID, l10n.T(lang, "err_save_loc"))
		} else {
			b.reply(msg.Chat.ID, l10n.T(lang, "msg_loc_saved"))
			b.sendCatDetails(msg.Chat.ID, state.CatID, lang)
			b.sendMainMenu(msg.Chat.ID, lang, l10n.T(lang, "msg_done_next"))
		}
		delete(b.states, msg.Chat.ID)

	case "plan_type":
		state.Record.Type = strings.TrimSpace(msg.Text)
		state.Step = "plan_time"
		exampleTime := time.Now().Add(1 * time.Hour).Format("2006-01-02 15:04")
		prompt := l10n.T(lang, "msg_plan_time", map[string]string{"Example": exampleTime})
		b.replyWithKeyboard(msg.Chat.ID, prompt, b.cancelKeyboard(lang))
	case "plan_time":
		text := strings.TrimSpace(msg.Text)
		var t time.Time
		var err error
		// Try YYYY-MM-DD HH:MM first
		if len(text) == 16 && text[4] == '-' && text[7] == '-' && text[10] == ' ' && text[13] == ':' {
			t, err = time.ParseInLocation("2006-01-02 15:04", text, time.Local)
		} else {
			t, err = time.Parse(time.RFC3339, text)
		}
		if err != nil {
			b.reply(msg.Chat.ID, l10n.T(lang, "err_invalid_time"))
			return
		}
		state.Record.PlannedAt = &t
		state.Step = "plan_note"
		b.replyWithKeyboard(msg.Chat.ID, l10n.T(lang, "msg_plan_note"), b.skipCancelKeyboard(lang))
	case "plan_note":
		if strings.ToLower(msg.Text) != l10n.T(lang, "menu_skip") && strings.ToLower(msg.Text) != "skip" {
			state.Record.Note = msg.Text
		}
		state.Step = "plan_recurrence"
		b.replyWithKeyboard(msg.Chat.ID, l10n.T(lang, "msg_plan_recur"), b.recurrenceKeyboard(lang))
	case "plan_recurrence":
		r := strings.ToLower(strings.TrimSpace(msg.Text))
		if r == l10n.T(lang, "recur_none") || r == "none" || r == "" {
			b.savePlannedRecord(msg.Chat.ID, state, lang)
			return
		}
		// Match localized or english
		if r == l10n.T(lang, "recur_daily") || r == "daily" {
			state.Record.Recurrence = "daily"
		} else if r == l10n.T(lang, "recur_weekly") || r == "weekly" {
			state.Record.Recurrence = "weekly"
		} else if r == l10n.T(lang, "recur_monthly") || r == "monthly" {
			state.Record.Recurrence = "monthly"
		} else {
			state.Record.Recurrence = r
		}
		state.Step = "plan_interval"
		b.replyWithKeyboard(msg.Chat.ID, l10n.T(lang, "msg_plan_interval"), b.cancelKeyboard(lang))
	case "plan_interval":
		val, err := strconv.Atoi(strings.TrimSpace(msg.Text))
		if err != nil || val <= 0 {
			b.reply(msg.Chat.ID, l10n.T(lang, "err_invalid_inter"))
			return
		}
		state.Record.Interval = val
		state.Step = "plan_end_date"
		b.replyWithKeyboard(msg.Chat.ID, l10n.T(lang, "msg_plan_end"), b.skipCancelKeyboard(lang))
	case "plan_end_date":
		text := strings.TrimSpace(msg.Text)
		if strings.ToLower(text) != l10n.T(lang, "menu_skip") && strings.ToLower(text) != "skip" {
			var t time.Time
			var err error
			if len(text) == 10 && text[4] == '-' && text[7] == '-' {
				t, err = time.Parse("2006-01-02", text)
			} else {
				t, err = time.Parse(time.RFC3339, text)
			}
			if err != nil {
				b.reply(msg.Chat.ID, l10n.T(lang, "err_invalid_date"))
				return
			}
			state.Record.EndDate = &t
		}
		b.savePlannedRecord(msg.Chat.ID, state, lang)
	case "obs_cond":
		text := strings.TrimSpace(msg.Text)
		cond := 3
		if strings.Contains(text, "1") {
			cond = 1
		} else if strings.Contains(text, "2") {
			cond = 2
		} else if strings.Contains(text, "3") {
			cond = 3
		} else if strings.Contains(text, "4") {
			cond = 4
		} else if strings.Contains(text, "5") {
			cond = 5
		}

		state.ObservationCondition = cond

		token, ok := b.ensureAuth(msg.Chat.ID, lang)
		if ok {
			cat, err := b.client.GetCat(state.CatID, token)
			if err == nil {
				cat.Condition = cond
				_, _ = b.client.UpdateCat(state.CatID, *cat, token)
			}
		}
		state.Step = "obs_note"
		b.replyWithKeyboard(msg.Chat.ID, l10n.T(lang, "msg_obs_note_prompt"), b.skipCancelKeyboard(lang))

	case "obs_note":
		if strings.ToLower(msg.Text) != l10n.T(lang, "menu_skip") && strings.ToLower(msg.Text) != "skip" && strings.ToLower(msg.Text) != "пропустить" {
			state.Record.Note = msg.Text
		}
		state.Step = "obs_photo"
		b.replyWithKeyboard(msg.Chat.ID, l10n.T(lang, "msg_obs_photo_prompt"), b.skipCancelKeyboard(lang))

	case "obs_photo":
		if strings.ToLower(msg.Text) != l10n.T(lang, "menu_skip") && strings.ToLower(msg.Text) != "skip" && strings.ToLower(msg.Text) != "пропустить" {
			if len(msg.Photo) > 0 {
				if err := b.processIncomingPhoto(msg.Chat.ID, state, msg, lang); err != nil {
					b.log.Errorf("photo upload failed: %v", err)
					b.reply(msg.Chat.ID, l10n.T(lang, "err_upload_failed"))
					return
				}
			} else {
				b.reply(msg.Chat.ID, l10n.T(lang, "msg_add_photo_prompt"))
				return
			}
		}
		state.Step = "obs_loc"
		btn := tgbotapi.KeyboardButton{Text: l10n.T(lang, "btn_send_loc"), RequestLocation: true}
		kb := tgbotapi.NewReplyKeyboard(
			tgbotapi.NewKeyboardButtonRow(btn),
			tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton(l10n.T(lang, "menu_skip")), tgbotapi.NewKeyboardButton(l10n.T(lang, "menu_cancel"))),
		)
		kb.ResizeKeyboard = true
		b.replyWithKeyboard(msg.Chat.ID, l10n.T(lang, "msg_obs_loc_prompt"), kb)

	case "obs_loc":
		if strings.ToLower(msg.Text) != l10n.T(lang, "menu_skip") && strings.ToLower(msg.Text) != "skip" && strings.ToLower(msg.Text) != "пропустить" {
			if msg.Location != nil {
				token, ok := b.ensureAuth(msg.Chat.ID, lang)
				if ok {
					_ = b.client.AddCatLocation(state.CatID, msg.Location.Latitude, msg.Location.Longitude, "", token)
				}
			} else {
				b.reply(msg.Chat.ID, l10n.T(lang, "msg_add_loc_prompt"))
				return
			}
		}
		// Final step
		b.observeCat(msg.Chat.ID, state.CatID, lang)
		b.sendCatDetails(msg.Chat.ID, state.CatID, lang)
		b.sendMainMenu(msg.Chat.ID, lang, l10n.T(lang, "msg_done_next"))
		delete(b.states, msg.Chat.ID)
	}
}

func (b *Bot) savePlannedRecord(chatID int64, state *ConversationState, lang string) {
	token, ok := b.ensureAuth(chatID, lang)
	if !ok {
		return
	}

	if err := b.client.CreateRecord(state.CatID, state.Record, token); err != nil {
		b.log.Errorf("failed to plan record for cat %s: %v", state.CatID, err)
		b.reply(chatID, l10n.T(lang, "err_plan_event"))
	} else {
		b.reply(chatID, l10n.T(lang, "msg_event_planned"))
		b.sendSchedule(chatID, state.CatID, lang)
		b.sendMainMenu(chatID, lang, l10n.T(lang, "msg_done_next"))
	}
	delete(b.states, chatID)
}

func (b *Bot) saveCatEdit(chatID int64, state *ConversationState, lang string) {
	token, ok := b.ensureAuth(chatID, lang)
	if !ok {
		return
	}
	_, err := b.client.UpdateCat(state.CatID, state.Cat, token)
	if err != nil {
		b.log.Errorf("failed to update cat %s: %v", state.CatID, err)
		b.reply(chatID, l10n.T(lang, "err_update_cat"))
	} else {
		b.reply(chatID, l10n.T(lang, "msg_data_updated"))
		b.sendCatDetails(chatID, state.CatID, lang)
		b.sendMainMenu(chatID, lang, l10n.T(lang, "msg_done_next"))
	}
	delete(b.states, chatID)
}

func (b *Bot) handleCallback(cb *tgbotapi.CallbackQuery, lang string) {
	data := cb.Data
	parts := strings.Split(data, ":")
	if len(parts) == 0 {
		return
	}

	action := parts[0]
	id := ""
	if len(parts) >= 2 {
		id = parts[1]
	}

	switch action {
	case "home":
		b.sendMainMenu(cb.Message.Chat.ID, lang, l10n.T(lang, "msg_welcome_home"))
	case "v": // view
		b.log.WithFields(logrus.Fields{"cat_id": id, "chat_id": cb.Message.Chat.ID}).Debug("bot: view cat")
		b.sendCatDetails(cb.Message.Chat.ID, id, lang)
	case "f": // feed
		b.log.WithFields(logrus.Fields{"cat_id": id, "chat_id": cb.Message.Chat.ID}).Debug("bot: feed cat")
		b.feedCat(cb.Message.Chat.ID, id, lang)
	case "o": // observe
		b.log.WithFields(logrus.Fields{"cat_id": id, "chat_id": cb.Message.Chat.ID}).Debug("bot: observe cat")
		b.startObserveFlow(cb.Message.Chat.ID, id, lang)
	case "s": // schedule
		b.sendSchedule(cb.Message.Chat.ID, id, lang)
	case "pr": // plan_record
		b.startRecordPlan(cb.Message.Chat.ID, id, lang)
	case "rd": // rec_done
		b.markRecordDone(cb.Message.Chat.ID, "", id, lang) // id here is actually recordID
	case "em": // edit_menu
		b.sendEditMenu(cb.Message.Chat.ID, id, lang)
	case "cm": // cond_menu
		b.sendConditionMenu(cb.Message.Chat.ID, id, lang)
	case "sc": // setcond
		if len(parts) >= 3 {
			b.applyCondition(cb.Message.Chat.ID, parts[1], parts[2], lang)
		}
	case "e": // edit
		if len(parts) >= 3 {
			b.startEdit(cb.Message.Chat.ID, parts[1], parts[2], lang) // parts[2] is field name
		}
	case "tm": // tags_menu
		b.sendTagsMenu(cb.Message.Chat.ID, id, lang)
	case "at": // add_tag
		b.startTagAdd(cb.Message.Chat.ID, id, lang)
	case "rt": // rem_tag
		b.startTagRemove(cb.Message.Chat.ID, id, lang)
	case "pm": // photos_menu
		b.sendPhotosMenu(cb.Message.Chat.ID, id, lang)
	case "lm": // loc_menu
		b.promptAddLocation(cb.Message.Chat.ID, id, lang)
	case "pd": // photos_delete
		b.sendPhotosDeleteList(cb.Message.Chat.ID, id, lang)
	case "ap": // add_photo
		b.promptAddPhoto(cb.Message.Chat.ID, id, lang)
	case "di": // delimg
		b.deletePhoto(cb.Message.Chat.ID, "", id, lang) // id here is actually imgID
	case "dc": // delete_confirm
		b.sendDeleteConfirm(cb.Message.Chat.ID, id, lang)
	case "d": // delete
		b.deleteCat(cb.Message.Chat.ID, id, lang)
	}

	// Answer callback to remove loading state
	callback := tgbotapi.NewCallback(cb.ID, "")
	if _, err := b.api.Request(callback); err != nil {
		b.log.Errorf("callback answer: %v", err)
	}
}

func (b *Bot) sendWelcome(chatID int64, lang string) {
	// Try to get token first to see if already authorized
	token, err := b.getToken(chatID)
	if err == nil && token != "" {
		welcomeText := l10n.T(lang, "msg_welcome_auth")
		b.replyMarkdown(chatID, welcomeText)
		b.sendMainMenu(chatID, lang)
		return
	}

	text, loginKB := b.getLoginMessage(chatID, lang)
	welcomeText := l10n.T(lang, "msg_welcome_intro") + text

	msg := tgbotapi.NewMessage(chatID, welcomeText)
	msg.ParseMode = tgbotapi.ModeMarkdown
	msg.ReplyMarkup = loginKB
	b.api.Send(msg)

	// Send separate message with main menu keyboard
	b.sendMainMenu(chatID, lang, l10n.T(lang, "msg_main_menu"))
}

func (b *Bot) sendHelp(chatID int64, lang string) {
	helpText := l10n.T(lang, "msg_help_title") + l10n.T(lang, "msg_help_body")
	b.replyMarkdown(chatID, helpText)
}

func (b *Bot) replyMarkdown(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = tgbotapi.ModeMarkdown
	b.api.Send(msg)
}

func (b *Bot) getLoginMessage(chatID int64, lang string) (string, tgbotapi.InlineKeyboardMarkup) {
	meta, err := b.client.GetAuthMetadata()
	if err != nil {
		b.log.WithError(err).Warn("failed to get auth metadata for login message")
		return l10n.T(lang, "msg_auth_needed"), tgbotapi.InlineKeyboardMarkup{}
	}

	b.log.WithFields(logrus.Fields{
		"chat_id": chatID,
		"issuer":  meta.Issuer,
	}).Debug("building login message using auth metadata")

	loginURL := fmt.Sprintf("%s?tg_chat_id=%d", meta.AuthorizationEndpoint, chatID)
	// If using PublicBaseURL for bot (if it differs from internal API URL), try to replace it.
	// This is useful when backend is behind a reverse proxy or in Docker, but returns internal host in metadata.
	if b.client.PublicBaseURL != "" && !strings.HasPrefix(meta.AuthorizationEndpoint, b.client.PublicBaseURL) {
		u, err := url.Parse(meta.AuthorizationEndpoint)
		if err == nil {
			p, err := url.Parse(b.client.PublicBaseURL)
			if err == nil {
				u.Scheme = p.Scheme
				u.Host = p.Host
				loginURL = fmt.Sprintf("%s?tg_chat_id=%d", u.String(), chatID)
			}
		}
	}

	rows := [][]tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonURL(l10n.T(lang, "label_auth_generic"), loginURL)),
	}

	return l10n.T(lang, "msg_auth_needed") + l10n.T(lang, "msg_auth_success"), tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// mainMenuKeyboard forms a permanent keyboard like BotFather
func (b *Bot) mainMenuKeyboard(lang string) tgbotapi.ReplyKeyboardMarkup {
	rows := [][]tgbotapi.KeyboardButton{
		{tgbotapi.NewKeyboardButton(l10n.T(lang, "menu_cats")), tgbotapi.NewKeyboardButton(l10n.T(lang, "menu_add_cat"))},
		{tgbotapi.NewKeyboardButton(l10n.T(lang, "menu_upcoming")), tgbotapi.NewKeyboardButton(l10n.T(lang, "menu_help"))},
		{tgbotapi.NewKeyboardButton(l10n.T(lang, "menu_cancel"))},
	}
	kb := tgbotapi.NewReplyKeyboard(rows...)
	kb.ResizeKeyboard = true
	kb.OneTimeKeyboard = false
	return kb
}

// sendMainMenu sends main menu with optional text
func (b *Bot) sendMainMenu(chatID int64, lang string, optionalText ...string) {
	text := l10n.T(lang, "msg_main_menu_prompt")
	if len(optionalText) > 0 && strings.TrimSpace(optionalText[0]) != "" {
		text = optionalText[0]
	}
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = b.mainMenuKeyboard(lang)
	b.api.Send(msg)
}

// replyRemoveKeyboard sends a message and hides custom keyboard during input
// emojiForCondition maps 1..5 to the given emoji scale
func emojiForCondition(c int) string {
	switch c {
	case 1:
		return "🙀"
	case 2:
		return "😿"
	case 3:
		return "😺"
	case 4:
		return "😽"
	case 5:
		return "😻"
	default:
		return ""
	}
}

func (b *Bot) recordTypeKeyboard(lang string) tgbotapi.ReplyKeyboardMarkup {
	kb := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton(l10n.T(lang, "rec_feeding")), tgbotapi.NewKeyboardButton(l10n.T(lang, "rec_medical"))),
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton(l10n.T(lang, "rec_observation")), tgbotapi.NewKeyboardButton(l10n.T(lang, "rec_sterilization"))),
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton(l10n.T(lang, "rec_vaccination")), tgbotapi.NewKeyboardButton(l10n.T(lang, "rec_vet_visit"))),
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton(l10n.T(lang, "rec_medication"))),
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton(l10n.T(lang, "menu_cancel"))),
	)
	kb.ResizeKeyboard = true
	return kb
}

func (b *Bot) recurrenceKeyboard(lang string) tgbotapi.ReplyKeyboardMarkup {
	kb := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton(l10n.T(lang, "recur_none")), tgbotapi.NewKeyboardButton(l10n.T(lang, "recur_daily"))),
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton(l10n.T(lang, "recur_weekly")), tgbotapi.NewKeyboardButton(l10n.T(lang, "recur_monthly"))),
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton(l10n.T(lang, "menu_cancel"))),
	)
	kb.ResizeKeyboard = true
	return kb
}

func (b *Bot) genderKeyboard(lang string) tgbotapi.ReplyKeyboardMarkup {
	kb := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton(l10n.T(lang, "gender_male")), tgbotapi.NewKeyboardButton(l10n.T(lang, "gender_female"))),
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton(l10n.T(lang, "gender_unknown"))),
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton(l10n.T(lang, "menu_cancel"))),
	)
	kb.ResizeKeyboard = true
	return kb
}

func (b *Bot) yesNoKeyboard(lang string) tgbotapi.ReplyKeyboardMarkup {
	kb := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton(l10n.T(lang, "btn_yes")), tgbotapi.NewKeyboardButton(l10n.T(lang, "btn_no"))),
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton(l10n.T(lang, "menu_cancel"))),
	)
	kb.ResizeKeyboard = true
	return kb
}

func (b *Bot) cancelKeyboard(lang string) tgbotapi.ReplyKeyboardMarkup {
	kb := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton(l10n.T(lang, "menu_cancel"))),
	)
	kb.ResizeKeyboard = true
	return kb
}

func (b *Bot) skipCancelKeyboard(lang string) tgbotapi.ReplyKeyboardMarkup {
	kb := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton(l10n.T(lang, "menu_skip")), tgbotapi.NewKeyboardButton(l10n.T(lang, "menu_cancel"))),
	)
	kb.ResizeKeyboard = true
	return kb
}

func (b *Bot) doneCancelKeyboard(lang string) tgbotapi.ReplyKeyboardMarkup {
	kb := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton(l10n.T(lang, "menu_done"))),
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton(l10n.T(lang, "menu_cancel"))),
	)
	kb.ResizeKeyboard = true
	return kb
}

func (b *Bot) replyWithKeyboard(chatID int64, text string, kb interface{}) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = kb
	b.api.Send(msg)
}

func (b *Bot) replyRemoveKeyboard(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = tgbotapi.NewRemoveKeyboard(true)
	b.api.Send(msg)
}

func (b *Bot) sendCatsList(chatID int64, lang string) {
	cats, err := b.client.ListCats()
	if err != nil {
		b.log.Errorf("list cats: %v", err)
		b.reply(chatID, l10n.T(lang, "err_api"))
		return
	}

	if len(cats) == 0 {
		b.reply(chatID, l10n.T(lang, "msg_no_cats"))
		return
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, cat := range cats {
		label := "🐱 " + cat.Name
		if cat.NeedAttention {
			label = "⚠️ " + label
		}
		if cat.Condition > 0 {
			label += " " + emojiForCondition(cat.Condition)
		}
		btn := tgbotapi.NewInlineKeyboardButtonData(label, "v:"+cat.ID)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(btn))
	}

	msg := tgbotapi.NewMessage(chatID, l10n.T(lang, "msg_cats_list_title"))
	msg.ParseMode = tgbotapi.ModeMarkdown
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	b.api.Send(msg)
}

func (b *Bot) getToken(chatID int64) (string, error) {
	t, err := b.client.GetBotToken(chatID)
	if err != nil {
		return "", err
	}
	b.tokens[chatID] = t
	return t, nil
}

func (b *Bot) ensureAuth(chatID int64, lang string) (string, bool) {
	token, err := b.getToken(chatID)
	if err != nil {
		text, loginKB := b.getLoginMessage(chatID, lang)
		promptText := l10n.T(lang, "msg_auth_first") + text
		msg := tgbotapi.NewMessage(chatID, promptText)
		msg.ParseMode = tgbotapi.ModeMarkdown
		msg.ReplyMarkup = loginKB
		b.api.Send(msg)
		return "", false
	}
	return token, true
}

func (b *Bot) isPublicURL(u string) bool {
	return u != "" && !strings.Contains(u, "localhost") && !strings.Contains(u, "127.0.0.1") && !strings.Contains(u, "backend")
}

func (b *Bot) getPhotoFileData(catID string, img storage.Image) tgbotapi.RequestFileData {
	if img.URL != "" && b.isPublicURL(img.URL) {
		return tgbotapi.FileURL(img.URL)
	}
	// Try to fetch bytes from backend via internal BaseURL
	data, _, err := b.client.GetCatImageBinary(catID, img.ID)
	if err != nil {
		b.log.Errorf("failed to fetch image %s from backend: %v", img.ID, err)
		// fallback to public URL even if it might fail (e.g. localhost)
		url := fmt.Sprintf("%s/api/cats/%s/images/%s", b.client.PublicBaseURL, catID, img.ID)
		return tgbotapi.FileURL(url)
	}
	// Use image ID as filename
	return tgbotapi.FileBytes{Name: img.ID + ".webp", Bytes: data}
}

func (b *Bot) sendCatDetails(chatID int64, id string, lang string) {
	token, _ := b.getToken(chatID) // Optional for public view
	cat, err := b.client.GetCat(id, token)
	if err != nil {
		b.log.Errorf("get cat %s: %v", id, err)
		b.reply(chatID, l10n.T(lang, "msg_cat_not_found"))
		return
	}

	headerEmoji := "🐱"
	if cat.NeedAttention {
		headerEmoji = "⚠️"
	}
	text := fmt.Sprintf("%s *%s*\n", headerEmoji, cat.Name)
	if cat.Description != "" {
		text += fmt.Sprintf("%s\n", cat.Description)
	}
	// Show condition with emoji
	condEmoji := emojiForCondition(cat.Condition)
	if condEmoji != "" {
		text += l10n.T(lang, "label_cond", map[string]any{"Emoji": condEmoji, "Value": cat.Condition}) + "\n"
	}
	if cat.NeedAttention {
		text += "⚠️ *" + l10n.T(lang, "btn_edit_needattention") + "*\n"
	}
	if len(cat.Locations) > 0 {
		// show latest
		latest := cat.Locations[0]
		for _, l := range cat.Locations[1:] {
			if l.CreatedAt.After(latest.CreatedAt) {
				latest = l
			}
		}
		locStr := fmt.Sprintf("%.6f, %.6f", latest.Latitude, latest.Longitude)
		if latest.Name != "" {
			locStr = latest.Name
		}
		text += l10n.T(lang, "label_last_loc", map[string]string{"Location": locStr, "Time": latest.CreatedAt.Local().Format("02.01.2006 15:04")}) + "\n"
	}
	if cat.LastSeen != nil {
		text += l10n.T(lang, "label_last_seen", map[string]string{"Time": cat.LastSeen.Local().Format("02.01.2006 15:04")}) + "\n"
	}

	// Show next upcoming event if authorized
	if token != "" {
		now := time.Now()
		start := now
		end := now.AddDate(0, 0, 30) // Look ahead 30 days
		recs, err := b.client.ListRecords(cat.ID, "planned", &start, &end, token)
		if err == nil && len(recs) > 0 {
			// ListRecords should already sort by PlannedAt ASC for 'planned' status
			next := recs[0]
			timeStr := l10n.T(lang, "label_planned")
			if next.PlannedAt != nil {
				timeStr = next.PlannedAt.Local().Format("02.01.2006 15:04")
			}
			text += l10n.T(lang, "msg_next_event", map[string]string{"Type": next.Type, "Time": timeStr})
		}
	}

	var tagNames []string
	for _, t := range cat.Tags {
		tagNames = append(tagNames, "#"+t.Name)
	}
	if len(tagNames) > 0 {
		text += "\n" + strings.Join(tagNames, " ") + "\n"
	}

	onlineURL := fmt.Sprintf("%s/#/cat/view/%s", b.client.PublicBaseURL, cat.ID)
	text += "\n" + l10n.T(lang, "label_view_online", map[string]string{"URL": onlineURL}) + "\n"

	btnSeen := tgbotapi.NewInlineKeyboardButtonData(l10n.T(lang, "btn_seen"), "lm:"+cat.ID)
	btnFeed := tgbotapi.NewInlineKeyboardButtonData(l10n.T(lang, "btn_feed"), "f:"+cat.ID)
	btnObserve := tgbotapi.NewInlineKeyboardButtonData(l10n.T(lang, "btn_observed"), "o:"+cat.ID)

	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(btnSeen),
		tgbotapi.NewInlineKeyboardRow(btnFeed, btnObserve),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(l10n.T(lang, "btn_edit"), "em:"+cat.ID),
			tgbotapi.NewInlineKeyboardButtonData(l10n.T(lang, "btn_photos"), "pm:"+cat.ID),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(l10n.T(lang, "btn_schedule"), "s:"+cat.ID),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(l10n.T(lang, "menu_home"), "home"),
		),
	)

	// If we have at least one image, attach the freshest one as a photo with caption
	if len(cat.Images) > 0 {
		// pick latest by CreatedAt
		latest := cat.Images[0]
		for _, im := range cat.Images[1:] {
			if im.CreatedAt.After(latest.CreatedAt) {
				latest = im
			}
		}

		photo := tgbotapi.NewPhoto(chatID, b.getPhotoFileData(cat.ID, latest))
		photo.Caption = text
		photo.ParseMode = tgbotapi.ModeMarkdown
		photo.ReplyMarkup = kb
		b.api.Send(photo)
		return
	}

	// Fallback: no images — send text message
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = tgbotapi.ModeMarkdown
	msg.ReplyMarkup = kb
	b.api.Send(msg)
}

func (b *Bot) sendTagsMenu(chatID int64, id string, lang string) {
	msg := tgbotapi.NewMessage(chatID, l10n.T(lang, "msg_photos_title"))
	msg.ParseMode = tgbotapi.ModeMarkdown
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(l10n.T(lang, "btn_add_tag"), "at:"+id),
			tgbotapi.NewInlineKeyboardButtonData(l10n.T(lang, "btn_rem_tag"), "rt:"+id),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(l10n.T(lang, "menu_back_to_edit"), "em:"+id),
		),
	)
	b.api.Send(msg)
}

func (b *Bot) startTagAdd(chatID int64, id string, lang string) {
	token, ok := b.ensureAuth(chatID, lang)
	if !ok {
		return
	}
	cat, err := b.client.GetCat(id, token)
	if err != nil {
		b.reply(chatID, l10n.T(lang, "err_get_cat"))
		return
	}

	state := &ConversationState{Step: "add_tag", CatID: id, Cat: *cat}
	b.states[chatID] = state
	b.replyWithKeyboard(chatID, l10n.T(lang, "msg_add_tag_prompt"), b.cancelKeyboard(lang))
}

func (b *Bot) startTagRemove(chatID int64, id string, lang string) {
	token, ok := b.ensureAuth(chatID, lang)
	if !ok {
		return
	}
	cat, err := b.client.GetCat(id, token)
	if err != nil {
		b.reply(chatID, l10n.T(lang, "err_get_cat"))
		return
	}

	state := &ConversationState{Step: "rem_tag", CatID: id, Cat: *cat}
	b.states[chatID] = state
	b.replyWithKeyboard(chatID, l10n.T(lang, "msg_rem_tag_prompt"), b.cancelKeyboard(lang))
}

func (b *Bot) sendPhotosMenu(chatID int64, id string, lang string) {
	// Ensure auth to get full cat object with images
	token, ok := b.ensureAuth(chatID, lang)
	if !ok {
		return
	}
	cat, err := b.client.GetCat(id, token)
	if err != nil {
		b.reply(chatID, l10n.T(lang, "err_load_photos"))
		return
	}

	// Send all photos as media groups (batches up to 10)
	if len(cat.Images) == 0 {
		b.reply(chatID, l10n.T(lang, "msg_no_photos"))
	} else {
		// Sort images by CreatedAt desc (freshest first)
		sort.Slice(cat.Images, func(i, j int) bool { return cat.Images[i].CreatedAt.After(cat.Images[j].CreatedAt) })
		var media []interface{}
		for _, im := range cat.Images {
			ph := tgbotapi.NewInputMediaPhoto(b.getPhotoFileData(cat.ID, im))
			media = append(media, ph)
		}
		// Telegram allows up to 10 media per group
		for i := 0; i < len(media); i += 10 {
			end := i + 10
			if end > len(media) {
				end = len(media)
			}
			group := tgbotapi.NewMediaGroup(chatID, media[i:end])
			// Set caption on the very first photo only to provide context
			if i == 0 && len(group.Media) > 0 {
				if p, ok := group.Media[0].(tgbotapi.InputMediaPhoto); ok {
					p.Caption = l10n.T(lang, "msg_photos_of", map[string]string{"Name": cat.Name})
					p.ParseMode = tgbotapi.ModeMarkdown
					group.Media[0] = p
				}
			}
			if _, err := b.api.SendMediaGroup(group); err != nil {
				b.log.WithError(err).Warn("failed to send media group")
			}
		}
	}

	// Send management menu below
	msg := tgbotapi.NewMessage(chatID, l10n.T(lang, "msg_photos_title"))
	msg.ParseMode = tgbotapi.ModeMarkdown
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(l10n.T(lang, "btn_add_photo"), "ap:"+id),
			tgbotapi.NewInlineKeyboardButtonData(l10n.T(lang, "btn_del_photos"), "pd:"+id),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(l10n.T(lang, "menu_back_to_cat"), "v:"+id),
		),
	)
	b.api.Send(msg)
}

func (b *Bot) promptAddPhoto(chatID int64, id string, lang string) {
	b.states[chatID] = &ConversationState{Step: "add_photo", CatID: id}
	b.replyWithKeyboard(chatID, l10n.T(lang, "msg_add_photo_prompt"), b.doneCancelKeyboard(lang))
}

func (b *Bot) promptAddLocation(chatID int64, id string, lang string) {
	b.states[chatID] = &ConversationState{Step: "add_loc", CatID: id}
	// Telegram button to request location
	btn := tgbotapi.KeyboardButton{Text: l10n.T(lang, "btn_send_loc"), RequestLocation: true}
	kb := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(btn),
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton(l10n.T(lang, "btn_just_seen"))),
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton(l10n.T(lang, "menu_cancel"))),
	)
	kb.ResizeKeyboard = true
	b.replyWithKeyboard(chatID, l10n.T(lang, "msg_seen_prompt"), kb)
}

func (b *Bot) deletePhoto(chatID int64, catID, imgID string, lang string) {
	token, ok := b.ensureAuth(chatID, lang)
	if !ok {
		return
	}
	img, err := b.client.DeleteCatImage(catID, imgID, token)
	if err != nil {
		b.reply(chatID, l10n.T(lang, "err_del_photo"))
		return
	}

	if catID == "" && img != nil {
		catID = img.CatID
	}

	b.reply(chatID, l10n.T(lang, "msg_photo_deleted"))
	b.sendCatDetails(chatID, catID, lang)
}

func (b *Bot) sendPhotosDeleteList(chatID int64, catID string, lang string) {
	token, ok := b.ensureAuth(chatID, lang)
	if !ok {
		return
	}
	cat, err := b.client.GetCat(catID, token)
	if err != nil {
		b.reply(chatID, l10n.T(lang, "err_load_photos"))
		return
	}
	if len(cat.Images) == 0 {
		b.reply(chatID, l10n.T(lang, "msg_no_photos"))
		return
	}
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, img := range cat.Images {
		label := l10n.T(lang, "label_delete_img", map[string]string{"ID": img.ID[len(img.ID)-4:]})
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, "di:"+img.ID),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(l10n.T(lang, "menu_back"), "pm:"+catID)))
	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	msg := tgbotapi.NewMessage(chatID, l10n.T(lang, "msg_delete_img_prompt"))
	msg.ReplyMarkup = kb
	b.api.Send(msg)
}

func (b *Bot) processIncomingPhoto(chatID int64, state *ConversationState, msg *tgbotapi.Message, lang string) error {
	// Pick the largest size
	ps := msg.Photo[len(msg.Photo)-1]
	file, err := b.api.GetFile(tgbotapi.FileConfig{FileID: ps.FileID})
	if err != nil {
		return err
	}
	url := file.Link(b.api.Token)
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	mime := resp.Header.Get("Content-Type")
	filename := path.Base(file.FilePath)

	token, ok := b.ensureAuth(chatID, lang)
	if !ok {
		return fmt.Errorf("auth failed")
	}
	_, err = b.client.UploadCatImage(state.CatID, filename, mime, data, token)
	return err
}

func (b *Bot) feedCat(chatID int64, id string, lang string) {
	token, ok := b.ensureAuth(chatID, lang)
	if !ok {
		return
	}
	now := time.Now()
	rec := storage.Record{
		Type:      "feeding",
		Note:      "Fed via Telegram bot",
		Timestamp: now,
		DoneAt:    &now,
	}

	if err := b.client.CreateRecord(id, rec, token); err != nil {
		b.log.Errorf("create feeding record for cat %s: %v", id, err)
		b.reply(chatID, l10n.T(lang, "err_api"))
		return
	}

	b.reply(chatID, l10n.T(lang, "msg_rec_done"))
}

func (b *Bot) sendSchedule(chatID int64, catID string, lang string) {
	token, ok := b.ensureAuth(chatID, lang)
	if !ok {
		return
	}

	// Fetch 2 most recent done records
	doneRecs, err := b.client.ListRecords(catID, "done", nil, nil, token)
	if err != nil {
		b.log.Errorf("list done records for cat %s: %v", catID, err)
	}
	if len(doneRecs) > 2 {
		doneRecs = doneRecs[:2]
	}

	// Fetch 3 closest planned records
	plannedRecs, err := b.client.ListRecords(catID, "planned", nil, nil, token)
	if err != nil {
		b.log.Errorf("list planned records for cat %s: %v", catID, err)
	}
	if len(plannedRecs) > 3 {
		plannedRecs = plannedRecs[:3]
	}

	if len(doneRecs) == 0 && len(plannedRecs) == 0 {
		b.reply(chatID, l10n.T(lang, "msg_no_planned"))
	} else {
		// Show done records
		if len(doneRecs) > 0 {
			b.replyMarkdown(chatID, l10n.T(lang, "label_past_events"))
			for _, rec := range doneRecs {
				timeStr := l10n.T(lang, "label_done")
				if rec.DoneAt != nil {
					timeStr = rec.DoneAt.Local().Format("02.01.2006 15:04")
				} else if !rec.Timestamp.IsZero() {
					timeStr = rec.Timestamp.Local().Format("02.01.2006 15:04")
				}
				text := l10n.T(lang, "msg_event_details", map[string]string{"Time": timeStr, "Type": rec.Type, "Note": rec.Note})
				b.replyMarkdown(chatID, text)
			}
		}

		// Show planned records
		if len(plannedRecs) > 0 {
			b.replyMarkdown(chatID, l10n.T(lang, "label_upcoming_events"))
			for _, rec := range plannedRecs {
				timeStr := l10n.T(lang, "label_planned")
				if rec.PlannedAt != nil {
					timeStr = rec.PlannedAt.Local().Format("02.01.2006 15:04")
				}
				text := l10n.T(lang, "msg_event_details", map[string]string{"Time": timeStr, "Type": rec.Type, "Note": rec.Note})
				if rec.Recurrence != "" {
					text += l10n.T(lang, "msg_recur_info", map[string]any{"Recurrence": rec.Recurrence, "Interval": rec.Interval})
				}

				msg := tgbotapi.NewMessage(chatID, text)
				msg.ParseMode = tgbotapi.ModeMarkdown
				msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData(l10n.T(lang, "btn_mark_done"), "rd:"+rec.ID),
					),
				)
				b.api.Send(msg)
			}
		}
	}

	// Schedule menu
	msg := tgbotapi.NewMessage(chatID, l10n.T(lang, "msg_schedule_title"))
	msg.ParseMode = tgbotapi.ModeMarkdown
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(l10n.T(lang, "btn_plan_event"), "pr:"+catID),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(l10n.T(lang, "menu_back_to_cat"), "v:"+catID),
		),
	)
	b.api.Send(msg)
}

func (b *Bot) markRecordDone(chatID int64, catID, recordID string, lang string) {
	token, ok := b.ensureAuth(chatID, lang)
	if !ok {
		return
	}

	rec, err := b.client.MarkRecordDone(catID, recordID, token)
	if err != nil {
		b.log.Errorf("mark record %s done: %v", recordID, err)
		b.reply(chatID, l10n.T(lang, "err_mark_done"))
		return
	}

	if catID == "" && rec != nil {
		catID = rec.CatID
	}

	b.reply(chatID, l10n.T(lang, "msg_rec_done"))
	b.sendSchedule(chatID, catID, lang)
}

func (b *Bot) startRecordPlan(chatID int64, catID string, lang string) {
	b.states[chatID] = &ConversationState{
		Step:  "plan_type",
		CatID: catID,
	}
	b.replyWithKeyboard(chatID, l10n.T(lang, "msg_plan_type"), b.recordTypeKeyboard(lang))
}

func (b *Bot) startObserveFlow(chatID int64, id string, lang string) {
	b.states[chatID] = &ConversationState{Step: "obs_cond", CatID: id}
	kb := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🙀 1"),
			tgbotapi.NewKeyboardButton("😿 2"),
			tgbotapi.NewKeyboardButton("😺 3"),
			tgbotapi.NewKeyboardButton("😽 4"),
			tgbotapi.NewKeyboardButton("😻 5"),
		),
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton(l10n.T(lang, "menu_cancel"))),
	)
	kb.ResizeKeyboard = true
	b.replyWithKeyboard(chatID, l10n.T(lang, "msg_select_cond"), kb)
}

func (b *Bot) observeCat(chatID int64, id string, lang string) {
	token, ok := b.ensureAuth(chatID, lang)
	if !ok {
		return
	}

	note := l10n.T(lang, "rec_observation")
	state, hasState := b.states[chatID]
	if hasState && state.ObservationCondition > 0 {
		condLabel := b.labelForCondition(state.ObservationCondition, lang)
		note = fmt.Sprintf("%s: %s %s", l10n.T(lang, "btn_edit_cond"), emojiForCondition(state.ObservationCondition), condLabel)
		if state.Record.Note != "" {
			note += "\n" + l10n.T(lang, "label_note") + ": " + state.Record.Note
		}
	}

	now := time.Now()
	rec := storage.Record{
		Type:      "observation",
		Note:      note,
		Timestamp: now,
		DoneAt:    &now,
	}

	if err := b.client.CreateRecord(id, rec, token); err != nil {
		b.log.Errorf("create observation record for cat %s: %v", id, err)
		b.reply(chatID, l10n.T(lang, "err_api"))
		return
	}

	b.reply(chatID, l10n.T(lang, "msg_rec_done"))
}

func (b *Bot) labelForCondition(cond int, lang string) string {
	key := fmt.Sprintf("label_cond_%d", cond)
	return l10n.T(lang, key)
}

func (b *Bot) sendEditMenu(chatID int64, id string, lang string) {
	msg := tgbotapi.NewMessage(chatID, l10n.T(lang, "msg_edit_profile"))
	msg.ParseMode = tgbotapi.ModeMarkdown
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(l10n.T(lang, "edit_name"), "e:"+id+":name"),
			tgbotapi.NewInlineKeyboardButtonData(l10n.T(lang, "edit_desc"), "e:"+id+":desc"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(l10n.T(lang, "btn_edit_cond"), "cm:"+id),
			tgbotapi.NewInlineKeyboardButtonData(l10n.T(lang, "btn_edit_color"), "e:"+id+":color"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(l10n.T(lang, "btn_edit_birth"), "e:"+id+":birth"),
			tgbotapi.NewInlineKeyboardButtonData(l10n.T(lang, "btn_edit_gender"), "e:"+id+":gender"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(l10n.T(lang, "btn_edit_sterilized"), "e:"+id+":sterilized"),
			tgbotapi.NewInlineKeyboardButtonData(l10n.T(lang, "btn_edit_needattention"), "e:"+id+":needattention"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(l10n.T(lang, "btn_edit_tags"), "tm:"+id),
			tgbotapi.NewInlineKeyboardButtonData(l10n.T(lang, "btn_delete"), "dc:"+id),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(l10n.T(lang, "menu_back_to_cat"), "v:"+id),
		),
	)
	b.api.Send(msg)
}

func (b *Bot) sendConditionMenu(chatID int64, id string, lang string) {
	msg := tgbotapi.NewMessage(chatID, l10n.T(lang, "msg_select_cond"))
	row := tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🙀 1", "sc:"+id+":1"),
		tgbotapi.NewInlineKeyboardButtonData("😿 2", "sc:"+id+":2"),
		tgbotapi.NewInlineKeyboardButtonData("😺 3", "sc:"+id+":3"),
		tgbotapi.NewInlineKeyboardButtonData("😽 4", "sc:"+id+":4"),
		tgbotapi.NewInlineKeyboardButtonData("😻 5", "sc:"+id+":5"),
	)
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(row, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(l10n.T(lang, "menu_back"), "em:"+id)))
	b.api.Send(msg)
}

func (b *Bot) applyCondition(chatID int64, id string, valStr string, lang string) {
	token, ok := b.ensureAuth(chatID, lang)
	if !ok {
		return
	}
	cat, err := b.client.GetCat(id, token)
	if err != nil {
		b.reply(chatID, l10n.T(lang, "err_get_cat"))
		return
	}
	switch strings.TrimSpace(valStr) {
	case "1":
		cat.Condition = 1
	case "2":
		cat.Condition = 2
	case "3":
		cat.Condition = 3
	case "4":
		cat.Condition = 4
	case "5":
		cat.Condition = 5
	default:
		cat.Condition = 3
	}
	if _, err := b.client.UpdateCat(id, *cat, token); err != nil {
		b.log.Errorf("failed to set condition: %v", err)
		b.reply(chatID, l10n.T(lang, "err_save_cond"))
		return
	}
	b.reply(chatID, l10n.T(lang, "msg_cond_updated"))
	b.sendCatDetails(chatID, id, lang)
}

func (b *Bot) startEdit(chatID int64, id string, field string, lang string) {
	token, ok := b.ensureAuth(chatID, lang)
	if !ok {
		return
	}
	cat, err := b.client.GetCat(id, token)
	if err != nil {
		b.reply(chatID, l10n.T(lang, "err_get_cat"))
		return
	}

	state := &ConversationState{
		CatID: id,
		Cat:   *cat,
	}

	var kb interface{}
	kb = b.cancelKeyboard(lang)

	var prompt string
	switch field {
	case "name":
		state.Step = "edit_name"
		prompt = l10n.T(lang, "msg_edit_name")
	case "desc":
		state.Step = "edit_desc"
		prompt = l10n.T(lang, "msg_edit_desc")
	case "color":
		state.Step = "edit_color"
		prompt = l10n.T(lang, "msg_edit_color")
	case "birth":
		state.Step = "edit_birth"
		prompt = l10n.T(lang, "msg_edit_birth")
		kb_ := tgbotapi.NewReplyKeyboard(
			tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton(l10n.T(lang, "menu_clear"))),
			tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton(l10n.T(lang, "menu_cancel"))),
		)
		kb_.ResizeKeyboard = true
		kb = kb_
	case "gender":
		state.Step = "edit_gender"
		prompt = l10n.T(lang, "msg_edit_gender")
		kb = b.genderKeyboard(lang)
	case "sterilized":
		state.Step = "edit_sterilized"
		prompt = l10n.T(lang, "msg_edit_sterilized")
		kb = b.yesNoKeyboard(lang)
	case "needattention":
		state.Step = "edit_needattention"
		prompt = l10n.T(lang, "msg_edit_needattention")
		kb = b.yesNoKeyboard(lang)
	}

	b.states[chatID] = state
	b.replyWithKeyboard(chatID, prompt, kb)
}

func (b *Bot) sendDeleteConfirm(chatID int64, id string, lang string) {
	msg := tgbotapi.NewMessage(chatID, l10n.T(lang, "msg_delete_confirm"))
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(l10n.T(lang, "btn_confirm_delete"), "d:"+id),
			tgbotapi.NewInlineKeyboardButtonData(l10n.T(lang, "menu_cancel"), "v:"+id),
		),
	)
	b.api.Send(msg)
}

func (b *Bot) deleteCat(chatID int64, id string, lang string) {
	token, ok := b.ensureAuth(chatID, lang)
	if !ok {
		return
	}
	if err := b.client.DeleteCat(id, token); err != nil {
		b.log.Errorf("delete cat %s: %v", id, err)
		b.reply(chatID, l10n.T(lang, "err_delete_cat"))
	} else {
		b.reply(chatID, l10n.T(lang, "msg_cat_deleted"))
		b.sendCatsList(chatID, lang)
	}
}

func (b *Bot) reply(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	b.api.Send(msg)
}

func (b *Bot) sendUpcomingEvents(chatID int64, lang string) {
	// Show events for next 7 days
	now := time.Now().UTC()
	start := now
	end := now.AddDate(0, 0, 7)

	recs, err := b.client.ListAllPlannedRecords(start, end)
	if err != nil {
		b.log.Errorf("failed to list planned records: %v", err)
		b.reply(chatID, l10n.T(lang, "err_upcoming"))
		return
	}

	if len(recs) == 0 {
		b.reply(chatID, l10n.T(lang, "msg_no_upcoming"))
		return
	}

	// Sort by PlannedAt
	sort.Slice(recs, func(i, j int) bool {
		if recs[i].PlannedAt == nil || recs[j].PlannedAt == nil {
			return false
		}
		return recs[i].PlannedAt.Before(*recs[j].PlannedAt)
	})

	var sb strings.Builder
	sb.WriteString(l10n.T(lang, "msg_upcoming_title"))

	for _, rec := range recs {
		catName := l10n.T(lang, "label_cat")
		cat, err := b.client.GetCat(rec.CatID, "")
		if err == nil {
			catName = cat.Name
		}

		timeStr := l10n.T(lang, "label_planned")
		if rec.PlannedAt != nil {
			timeStr = rec.PlannedAt.Local().Format("02.01 15:04")
		}

		sb.WriteString(fmt.Sprintf("🔹 *%s*: %s (%s)\n", timeStr, catName, rec.Type))
		if rec.Note != "" {
			sb.WriteString(fmt.Sprintf("   _%s: %s_\n", l10n.T(lang, "label_note"), rec.Note))
		}
	}

	msg := tgbotapi.NewMessage(chatID, sb.String())
	msg.ParseMode = "Markdown"
	b.api.Send(msg)
}

func (b *Bot) remindersLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	b.log.Info("Reminders loop started")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.checkReminders()
		}
	}
}

func (b *Bot) checkReminders() {
	// Check events for next 30 minutes
	now := time.Now().UTC()
	start := now
	end := now.Add(30 * time.Minute)

	recs, err := b.client.ListAllPlannedRecords(start, end)
	if err != nil {
		b.log.Errorf("failed to list planned records: %v", err)
		return
	}

	if len(recs) > 0 {
		b.log.WithField("count", len(recs)).Debug("found planned records for reminders")
	} else {
		return
	}

	users, err := b.client.ListBotUsers()
	if err != nil {
		b.log.Errorf("failed to list bot users: %v", err)
		return
	}
	b.log.WithField("count", len(users)).Debug("found bot users for reminders")

	for _, rec := range recs {
		// Fetch cat details to get the name
		cat, err := b.client.GetCat(rec.CatID, "")
		catName := l10n.T("en", "label_cat") // Default to EN for background loop
		if err == nil {
			catName = cat.Name
		}

		for _, user := range users {
			var chatID int64
			fmt.Sscanf(user.ProviderID, "%d", &chatID)
			if chatID == 0 {
				continue
			}

			// Try to mark as sent first (atomic check-and-set in backend)
			notif := storage.BotNotification{
				RecordID: rec.ID,
				ChatID:   chatID,
				SentAt:   time.Now(),
			}

			if err := b.client.MarkNotificationSent(notif); err != nil {
				if err == ErrAlreadyExists {
					continue // Already sent to this user
				}
				b.log.Errorf("failed to mark notification as sent for user %d: %v", chatID, err)
				continue
			}

			// Successfully marked as "sending now", send the message
			lang := "en" // Default for background loop
			timeStr := l10n.T(lang, "label_today")
			if rec.PlannedAt != nil {
				timeStr = rec.PlannedAt.Local().Format("15:04")
			}
			msgText := l10n.T(lang, "msg_reminder_title") + l10n.T(lang, "msg_reminder_body", map[string]string{
				"Name": catName,
				"Type": rec.Type,
				"Time": timeStr,
				"Note": rec.Note,
			})

			onlineURL := fmt.Sprintf("%s/#/cat/view/%s", b.client.PublicBaseURL, rec.CatID)
			msgText += "\n\n" + l10n.T(lang, "label_view_online", map[string]string{"URL": onlineURL})

			msg := tgbotapi.NewMessage(chatID, msgText)
			msg.ParseMode = "Markdown"

			// Add inline button to see cat details
			keyboard := tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData(l10n.T(lang, "msg_view_cat"), "v:"+rec.CatID),
				),
			)
			msg.ReplyMarkup = keyboard

			if _, err := b.api.Send(msg); err != nil {
				b.log.Errorf("failed to send reminder to chat %d: %v", chatID, err)
			}
		}
	}
}

func (b *Bot) startHealthServer(ctx context.Context) {
	if b.client == nil || b.api == nil {
		return
	}
	// We use a simple mux for health checks
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz/alive", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
	mux.HandleFunc("/healthz/ready", func(w http.ResponseWriter, r *http.Request) {
		// Check Telegram connectivity
		_, err := b.api.GetMe()
		if err != nil {
			b.log.Errorf("healthz: telegram connection failed: %v", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("FAIL"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	addr := b.healthListenAddr
	if addr == "" {
		addr = ":8080" // Default
	}

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Override addr if needed (I will update Bot struct)
	// For now, let's just use :8080 as it's standard for Docker healthchecks.

	go func() {
		b.log.Infof("Health server listening on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			b.log.Errorf("Health server failed: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}
