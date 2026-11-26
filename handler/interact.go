package handler

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"reposter/config"
	"reposter/database"

	"github.com/OvyFlash/telegram-bot-api"
	"github.com/bwmarrin/discordgo"
)

const (
	// sendPrefix and skipPrefix mark the only inline buttons the bot serves;
	// the rest of the callback data is the post key.
	sendPrefix = "repost_no_files:"
	skipPrefix = "skip_no_files:"
	// skipLabel is the text of the button that forwards nothing.
	skipLabel = "Не переслать"
	// flushDelay flushes buffered oversized files even when no further update
	// ends the media group (e.g. the album was last in a long-poll batch).
	flushDelay = 5 * time.Second
)

// oversizedItem is one skipped file of a channel post.
type oversizedItem struct {
	post        *tgbotapi.Message
	fileName    string
	sizeBytes   int64
	blankFooter bool
}

func newOversizedItem(post *tgbotapi.Message, fileName string, sizeBytes int64, blankFooter bool) *oversizedItem {
	return &oversizedItem{
		post:        post,
		fileName:    fileName,
		sizeBytes:   sizeBytes,
		blankFooter: blankFooter,
	}
}

// pendingRetry is a posted Telegram notice with the decision buttons.
type pendingRetry struct {
	items       []*oversizedItem
	noticeMsgID int
}

// RetryStore buffers skipped files of the current media group until the group
// ends (one notice per group) and remembers posted notices so the buttons can
// find them. Safe for concurrent use: updates are handled in the main loop,
// the flush timer fires on its own.
type RetryStore struct {
	mu      sync.Mutex
	buf     []*oversizedItem
	group   string
	timer   *time.Timer
	pending map[string]*pendingRetry
}

func NewRetryStore() *RetryStore {
	return &RetryStore{pending: make(map[string]*pendingRetry)}
}

// AddOversized buffers a skipped file; the first buffered file of a group
// arms the flush timer.
func (r *RetryStore) AddOversized(conf *config.Config, tgbot *tgbotapi.BotAPI, item *oversizedItem, groupID string) {
	r.mu.Lock()
	if r.timer == nil {
		r.timer = time.AfterFunc(flushDelay, func() {
			if items := r.takeBuffered(); len(items) > 0 {
				postGroupError(conf, tgbot, r, items)
			}
		})
	}
	r.buf = append(r.buf, item)
	r.group = groupID
	r.mu.Unlock()
}

// takeIfChanged returns buffered files if they belong to another media group.
func (r *RetryStore) takeIfChanged(groupID string) []*oversizedItem {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.buf) > 0 && r.group != groupID {
		return r.takeLocked()
	}
	return nil
}

// takeBuffered returns buffered files regardless of their media group.
func (r *RetryStore) takeBuffered() []*oversizedItem {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.buf) > 0 {
		return r.takeLocked()
	}
	return nil
}

// takeLocked must be called with r.mu held.
func (r *RetryStore) takeLocked() []*oversizedItem {
	items := r.buf
	r.buf = nil
	r.group = ""
	if r.timer != nil {
		r.timer.Stop()
		r.timer = nil
	}
	return items
}

func (r *RetryStore) addPending(key string, p *pendingRetry) {
	r.mu.Lock()
	r.pending[key] = p
	r.mu.Unlock()
}

// popPending atomically removes and returns the notice, so a double click
// cannot repost twice.
func (r *RetryStore) popPending(key string) (*pendingRetry, bool) {
	r.mu.Lock()
	p, ok := r.pending[key]
	if ok {
		delete(r.pending, key)
	}
	r.mu.Unlock()
	return p, ok
}

func postKey(post *tgbotapi.Message) string {
	return fmt.Sprintf("%d,%d", post.Chat.ID, post.MessageID)
}

// resendButtonLabel picks singular/plural for the button text.
func resendButtonLabel(n int) string {
	if n > 1 {
		return "Переслать без этих файлов"
	}
	return "Переслать без файла"
}

// hasText reports whether the post carries text that can be forwarded
// without its file.
func hasText(post *tgbotapi.Message) bool {
	return post.Text != "" || post.Caption != ""
}

// anyHasText reports whether at least one of the skipped posts has text to
// forward: only then is there a decision to ask about.
func anyHasText(items []*oversizedItem) bool {
	for _, it := range items {
		if hasText(it.post) {
			return true
		}
	}
	return false
}

// oversizedNoticeText lists the skipped files of a post or media group; the
// question is added only when there is something to forward without them.
func oversizedNoticeText(limitMB int, items []*oversizedItem) string {
	var list strings.Builder
	for _, it := range items {
		list.WriteString("• " + it.fileName)
		if it.sizeBytes > 0 {
			fmt.Fprintf(&list, " — %.1f MiB", float64(it.sizeBytes)/(1<<20))
		}
		list.WriteString("\n")
	}

	var text string
	if len(items) == 1 {
		text = fmt.Sprintf("⚠️ Файл не отправлен — превышен лимит %d MiB:\n%s", limitMB, list.String())
	} else {
		text = fmt.Sprintf("⚠️ Файлы не отправлены — превышен лимит %d MiB:\n%s", limitMB, list.String())
	}
	if anyHasText(items) {
		if len(items) == 1 {
			text += "\nОтправить пост без файла?"
		} else {
			text += "\nОтправить посты без этих файлов?"
		}
	}
	return text
}

// postGroupError asks in Telegram what to do with the skipped files of a post
// or media group: nothing reaches Discord until the sender decides. Posts
// without text of their own get the same notice without buttons — there is
// nothing to forward, so nothing to decide.
func postGroupError(conf *config.Config, tgbot *tgbotapi.BotAPI, retry *RetryStore, items []*oversizedItem) {
	msg := tgbotapi.NewMessage(items[0].post.Chat.ID, oversizedNoticeText(conf.MaxFileSizeMB, items))
	msg.ReplyParameters.MessageID = items[0].post.MessageID
	// The post may be deleted by the time the notice goes out.
	msg.ReplyParameters.AllowSendingWithoutReply = true
	msg.DisableNotification = true
	msg.LinkPreviewOptions.IsDisabled = true

	key := postKey(items[0].post)
	decide := anyHasText(items)
	if decide {
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(resendButtonLabel(len(items)), sendPrefix+key),
				tgbotapi.NewInlineKeyboardButtonData(skipLabel, skipPrefix+key),
			),
		)
	}

	m, err := tgbot.Send(msg)
	if err != nil {
		log.Printf("Cannot send oversized-files notice! See error: %s", err.Error())
		return
	}
	if decide {
		retry.addPending(key, &pendingRetry{items: items, noticeMsgID: m.MessageID})
	}
}

// parseCallback splits the callback data of the notice buttons into the post
// key and the decision it carries; ok is false for any other callback.
func parseCallback(data string) (key string, skip bool, ok bool) {
	switch {
	case strings.HasPrefix(data, sendPrefix):
		key = strings.TrimPrefix(data, sendPrefix)
	case strings.HasPrefix(data, skipPrefix):
		key, skip = strings.TrimPrefix(data, skipPrefix), true
	default:
		return "", false, false
	}
	return key, skip, true
}

// HandleCallback serves presses on the notice buttons; nothing is forwarded
// until "resend" is pressed. Called from the main update loop.
func HandleCallback(conf *config.Config, db *database.Database, tgbot *tgbotapi.BotAPI, dcbot *discordgo.Session, retry *RetryStore, q *tgbotapi.CallbackQuery) {
	key, skip, ok := parseCallback(q.Data)
	if !ok {
		return
	}
	p, ok := retry.popPending(key)
	if !ok {
		// The notice outlived the process (bot restart) or was already served.
		answerCallback(tgbot, q.ID, "Кнопка устарела: пост уже обработан или бот перезапускался.", true)
		return
	}

	if skip {
		answerCallback(tgbot, q.ID, "Не переслано", false)
	} else {
		var failed []*oversizedItem
		for _, it := range p.items {
			if !resendWithoutFile(conf, db, dcbot, it) {
				failed = append(failed, it)
			}
		}
		if len(failed) > 0 {
			// Transient send failure: keep the notice with its buttons so the
			// posts that failed can be resent by pressing again.
			retry.addPending(key, &pendingRetry{items: failed, noticeMsgID: p.noticeMsgID})
			answerCallback(tgbot, q.ID, "Не удалось переслать, попробуйте ещё раз.", true)
			return
		}
		answerCallback(tgbot, q.ID, "Переслано", false)
	}

	// deleteMessage returns true, which Send cannot decode into a Message.
	if _, err := tgbot.Request(tgbotapi.NewDeleteMessage(p.items[0].post.Chat.ID, p.noticeMsgID)); err != nil {
		log.Printf("Cannot delete oversized-files notice! See error: %s", err.Error())
	}
}

// answerCallback acknowledges a button press with a toast, or an alert.
func answerCallback(tgbot *tgbotapi.BotAPI, id, text string, alert bool) {
	cb := tgbotapi.NewCallback(id, text)
	cb.ShowAlert = alert
	// answerCallbackQuery returns true as well.
	if _, err := tgbot.Request(cb); err != nil {
		log.Printf("Cannot answer button press! See error: %s", err.Error())
	}
}

// resendWithoutFile forwards the text part of a skipped post: the file was
// the reason the post was skipped, so only the embed is sent. It reports
// whether the repost happened (a database-record failure still counts —
// the post itself is already in the channel).
func resendWithoutFile(conf *config.Config, db *database.Database, dcbot *discordgo.Session, it *oversizedItem) bool {
	embd := formatEmbed(it.post)
	if embd.MessageEmbed.Description == "" {
		return true // the file was the whole post, nothing to forward
	}
	if it.blankFooter {
		embd.SetFooter("")
	}

	m, err := dcbot.ChannelMessageSendComplex(conf.Discord.ChannelID, &discordgo.MessageSend{
		Embed: embd.MessageEmbed,
	})
	if err != nil {
		log.Printf("Cannot repost post without file! See error: %s", err.Error())
		return false
	}

	pm := database.PostManager{
		DB: db.Conn,
		Data: &database.Post{
			Telegram: postKey(it.post),
			Discord:  m.ID,
			IsEmbed:  true,
		},
	}
	if err := pm.Create(); err != nil {
		log.Printf("Cannot create new record in database! See error: %s. TG: %s", err.Error(), pm.Data.Telegram)
	}
	return true
}
