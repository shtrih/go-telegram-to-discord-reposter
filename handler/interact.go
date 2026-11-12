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
	// buttonPrefix marks the only message component the bot serves.
	buttonPrefix = "repost_no_files:"
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

// pendingRetry is a posted notice with the resend button.
type pendingRetry struct {
	items    []*oversizedItem
	errMsgID string
}

// RetryStore buffers skipped files of the current media group until the group
// ends (one notice per group) and remembers posted notices so the button can
// find them. Safe for concurrent use: updates are handled in the main loop,
// the flush timer fires on its own, button clicks arrive on discordgo
// goroutines.
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
func (r *RetryStore) AddOversized(conf *config.Config, dcbot *discordgo.Session, item *oversizedItem, groupID string) {
	r.mu.Lock()
	if r.timer == nil {
		r.timer = time.AfterFunc(flushDelay, func() {
			if items := r.takeBuffered(); len(items) > 0 {
				postGroupError(conf, dcbot, r, items)
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

// postGroupError posts one notice listing every skipped file of a post or
// media group, with the resend button attached.
func postGroupError(conf *config.Config, dcbot *discordgo.Session, retry *RetryStore, items []*oversizedItem) {
	embd := formatEmbed(items[0].post)
	if items[0].blankFooter {
		embd.SetFooter("")
	}

	var list strings.Builder
	for _, it := range items {
		list.WriteString("• " + it.fileName)
		if it.sizeBytes > 0 {
			fmt.Fprintf(&list, " — %.1f MiB", float64(it.sizeBytes)/(1<<20))
		}
		list.WriteString("\n")
	}
	if len(items) == 1 {
		embd.MessageEmbed.Description = fmt.Sprintf("⚠️ Файл не отправлен — превышен лимит %d MiB:\n%s", conf.MaxFileSizeMB, list.String())
	} else {
		embd.MessageEmbed.Description = fmt.Sprintf("⚠️ Файлы не отправлены — превышен лимит %d MiB:\n%s", conf.MaxFileSizeMB, list.String())
	}

	key := postKey(items[0].post)
	m, err := dcbot.ChannelMessageSendComplex(conf.Discord.ChannelID, &discordgo.MessageSend{
		Embed: embd.MessageEmbed,
		Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{
						Label:    resendButtonLabel(len(items)),
						Style:    discordgo.PrimaryButton,
						CustomID: buttonPrefix + key,
					},
				},
			},
		},
	})
	if err != nil {
		log.Printf("Cannot send oversized-files notice! See error: %s", err.Error())
		return
	}
	retry.addPending(key, &pendingRetry{items: items, errMsgID: m.ID})
}

// HandleInteraction serves clicks on the resend button. Registered in main.go
// via dcbot.AddHandler; discordgo calls it on its own goroutines.
func HandleInteraction(conf *config.Config, db *database.Database, dcbot *discordgo.Session, retry *RetryStore, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionMessageComponent {
		return
	}
	data := i.MessageComponentData()
	if !strings.HasPrefix(data.CustomID, buttonPrefix) {
		return
	}

	key := strings.TrimPrefix(data.CustomID, buttonPrefix)
	p, ok := retry.popPending(key)
	if !ok {
		// The notice outlived the process (bot restart): state is gone.
		if err := dcbot.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "⚠️ Кнопка устарела (бот перезапускался), пересылка невозможна.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		}); err != nil {
			log.Printf("Cannot answer stale button click! See error: %s", err.Error())
		}
		return
	}

	// Acknowledge within 3s, then repost (embeds only, nothing to download).
	if err := dcbot.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
		Data: &discordgo.InteractionResponseData{},
	}); err != nil {
		log.Printf("Cannot acknowledge button click! See error: %s", err.Error())
	}

	var failed []*oversizedItem
	for _, it := range p.items {
		if !resendWithoutFile(conf, db, dcbot, it) {
			failed = append(failed, it)
		}
	}
	if len(failed) > 0 {
		// Transient send failure: keep the notice with its button so the
		// posts that failed can be resent by clicking again.
		retry.addPending(key, &pendingRetry{items: failed, errMsgID: p.errMsgID})
		return
	}
	if err := dcbot.ChannelMessageDelete(conf.Discord.ChannelID, p.errMsgID); err != nil {
		log.Printf("Cannot delete oversized-files notice! See error: %s", err.Error())
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
