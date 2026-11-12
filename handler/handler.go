package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"reposter/config"
	"reposter/database"
	"reposter/tgapi"

	embed "github.com/Clinet/discordgo-embed"
	"github.com/OvyFlash/telegram-bot-api"
	"github.com/bwmarrin/discordgo"
	"github.com/jinzhu/gorm"
)

func PrettyPrint(i interface{}) string {
	s, _ := json.MarshalIndent(i, "", "\t")
	return string(s)
}

func embedSetVideo(e *embed.Embed, url string) {
	e.MessageEmbed.Video = &discordgo.MessageEmbedVideo{
		URL: url,
	}
}

func embedSetTimestamp(e *embed.Embed, sec int64) {
	e.MessageEmbed.Timestamp = time.Unix(sec, 0).Format(time.RFC3339)
}

func getAuthorSignature(msg *tgbotapi.Message) string {
	authorSignature := ""
	if msg.AuthorSignature != "" {
		authorSignature = msg.AuthorSignature + " "
	}

	return authorSignature
}

func getForwardedFrom(msg *tgbotapi.Message) string {
	forwardedFrom := ""
	origin := msg.ForwardOrigin
	switch {
	case origin == nil:
		// Not a forwarded message
	case origin.IsChannel():
		chatName := ""
		if origin.Chat.UserName != "" {
			chatName = fmt.Sprintf(" (@%s)", origin.Chat.UserName)
		}
		forwardedFrom = fmt.Sprintf("forwarded from «%s»%s", origin.Chat.Title, chatName)
	case origin.IsChat():
		chatName := ""
		if origin.SenderChat.UserName != "" {
			chatName = fmt.Sprintf(" (@%s)", origin.SenderChat.UserName)
		}
		forwardedFrom = fmt.Sprintf("forwarded from «%s»%s", origin.SenderChat.Title, chatName)
	case origin.IsUser():
		lastName := ""
		if origin.SenderUser.LastName != "" {
			lastName = " " + origin.SenderUser.LastName
		}
		forwardedFrom = fmt.Sprintf("forwarded from %s%s", origin.SenderUser.FirstName, lastName)
	case origin.IsHiddenUser():
		forwardedFrom = fmt.Sprintf("forwarded from %s", origin.SenderUserName)
	}

	return forwardedFrom
}

func formatMessage(msg *tgbotapi.Message) string {
	authorSignature := getAuthorSignature(msg)
	forwardedFrom := getForwardedFrom(msg)

	linebreak := ""
	if authorSignature != "" || forwardedFrom != "" {
		linebreak = "\n\n"
	}

	return authorSignature + forwardedFrom + linebreak + msg.Caption + msg.Text
}

func getPostLink(msg *tgbotapi.Message) string {
	var result string
	if origin := msg.ForwardOrigin; origin != nil && origin.IsChannel() {
		if origin.Chat.UserName != `` && origin.MessageID != 0 {
			result = fmt.Sprintf(`https://t.me/%s/%d`, origin.Chat.UserName, origin.MessageID)
		}
	}
	return result
}

func formatEmbed(msg *tgbotapi.Message) *embed.Embed {
	forwardedFrom := getForwardedFrom(msg)
	authorSignature := getAuthorSignature(msg)
	result := embed.NewEmbed().
		//SetTitle(getAuthorSignature(msg) + getForwardedFrom(msg)).
		SetFooter(authorSignature + forwardedFrom).
		SetColor(0x30a3e6).
		Truncate()

	// Hide Telegram internal links if forward source hidden by user
	var textEntities, captionEntities []tgbotapi.MessageEntity
	text, textCaption := msg.Text, msg.Caption
	if forwardedFrom == "" && authorSignature == "" {
		for _, e := range msg.Entities {
			if e.IsTextLink() && e.URL[:13] == "https://t.me/" {
				e.URL = "https://t.me/#link-hidden-in-discord"
			}
			textEntities = append(textEntities, e)
		}
		for _, e := range msg.CaptionEntities {
			if e.IsTextLink() && e.URL[:13] == "https://t.me/" {
				e.URL = "https://t.me/#link-hidden-in-discord"
			}
			captionEntities = append(captionEntities, e)
		}
	} else {
		textEntities, captionEntities = msg.Entities, msg.CaptionEntities
	}

	text = tgapi.EntitiesToDiscordMarkdown(text, textEntities)
	textCaption = tgapi.EntitiesToDiscordMarkdown(textCaption, captionEntities)

	// Hide Telegram internal links if forward source hidden by user
	if forwardedFrom == "" && authorSignature == "" {
		var re = regexp.MustCompile(`(https[:]//t[.]me/)[^\s]+\b`)
		text = re.ReplaceAllString(text, `$1#link-hidden-in-discord`)
		textCaption = re.ReplaceAllString(textCaption, `$1#link-hidden-in-discord`)
	} else {
		if link := getPostLink(msg); link != `` {
			result.AddField(`—`, link).InlineAllFields()
		}
	}

	// embed.SetDescription() truncates text at 2048, but actual limit is 4096
	// https://discord.com/developers/docs/resources/channel#embed-limits
	// For now TG text limit also 4096 so no need any truncations.
	// https://core.telegram.org/bots/api#message
	result.Description = text
	result.Description += textCaption

	if msg.ForwardOrigin == nil {
		embedSetTimestamp(result, msg.Date)
	} else {
		embedSetTimestamp(result, msg.ForwardOrigin.Date)
	}

	return result
}

func isJustLink(msg *tgbotapi.Message) bool {
	if len(msg.Entities) == 1 && msg.Entities[0].IsURL() && msg.Entities[0].Length == len(msg.Text) {
		return true
	}
	return false
}

// mediaFileSize returns the reported size in bytes of the post's media file,
// 0 when Telegram did not report one.
func mediaFileSize(post *tgbotapi.Message) int64 {
	switch {
	case post.Document != nil:
		return int64(post.Document.FileSize)
	case post.Video != nil:
		return int64(post.Video.FileSize)
	case post.VideoNote != nil:
		return int64(post.VideoNote.FileSize)
	case post.Audio != nil:
		return int64(post.Audio.FileSize)
	case post.Voice != nil:
		return int64(post.Voice.FileSize)
	case post.Photo != nil && len(post.Photo) > 0:
		return int64(post.Photo[len(post.Photo)-1].FileSize)
	case post.Sticker != nil && post.Sticker.Thumbnail != nil:
		return int64(post.Sticker.Thumbnail.FileSize)
	}
	return 0
}

// isTooLargeErr reports whether the error means a file exceeded a size limit:
// Telegram refuses getFile to bots above 20 MiB ("file is too big") and
// Discord answers HTTP 413 to uploads above the per-file limit.
func isTooLargeErr(err error) bool {
	if err == nil {
		return false
	}
	if strings.Contains(err.Error(), "file is too big") {
		return true
	}
	var restErr *discordgo.RESTError
	return errors.As(err, &restErr) && restErr.Response != nil &&
		restErr.Response.StatusCode == http.StatusRequestEntityTooLarge
}

var LastMediaGroupID string

func HandleUpdate(conf *config.Config, db *database.Database, client *http.Client, tgbot *tgbotapi.BotAPI, dcbot *discordgo.Session, retry *RetryStore, u tgbotapi.Update) {
	// A post of another media group (or any non-post update) ends the
	// previous group: post its skipped-files notice, if any.
	groupID := ""
	if u.ChannelPost != nil {
		groupID = u.ChannelPost.MediaGroupID
	}
	if items := retry.takeIfChanged(groupID); len(items) > 0 {
		postGroupError(conf, dcbot, retry, items)
	}

	if u.ChannelPost != nil {
		var m *discordgo.Message

		var fileID *string
		var fileName string
		var contentType string
		embd := formatEmbed(u.ChannelPost)
		// Same condition the footer blanking below uses, captured before
		// LastMediaGroupID gets updated at the end of this branch.
		blankFooter := LastMediaGroupID != "" && LastMediaGroupID == u.ChannelPost.MediaGroupID
		limit := int64(conf.MaxFileSizeMB) << 20

		// Send repost to Discord text channel
		if u.ChannelPost.Text != "" {
			var err error
			// Post links as text to have preview
			if isJustLink(u.ChannelPost) {
				m, err = dcbot.ChannelMessageSend(conf.Discord.ChannelID, formatMessage(u.ChannelPost))
			} else {
				m, err = dcbot.ChannelMessageSendEmbed(conf.Discord.ChannelID, embd.MessageEmbed)
			}
			if err != nil {
				log.Printf("Cannot repost your post! See error: %s", err.Error())

				_, err2 := tgbot.Send(tgbotapi.NewMessage(u.ChannelPost.Chat.ID, err.Error()))
				if err2 != nil {
					log.Print("cannot send tg msg", err2)
				}
				return
			}
		} else if u.ChannelPost.Photo != nil {
			if len(u.ChannelPost.Photo) > 0 {
				size := mediaFileSize(u.ChannelPost)
				if size > limit {
					retry.AddOversized(conf, dcbot, newOversizedItem(u.ChannelPost, "photo.jpg", size, blankFooter), u.ChannelPost.MediaGroupID)
					return
				}
				p := u.ChannelPost.Photo
				url, err := tgbot.GetFileDirectURL(p[len(u.ChannelPost.Photo)-1].FileID)
				if err != nil {
					if isTooLargeErr(err) {
						retry.AddOversized(conf, dcbot, newOversizedItem(u.ChannelPost, "photo.jpg", size, blankFooter), u.ChannelPost.MediaGroupID)
						return
					}
					log.Printf("Cannot get direct file URL! See error: %s", err.Error())
					return
				}

				resp, err := client.Get(url)
				if err != nil {
					log.Printf("Cannot do GET request! See error: %s", err.Error())
					return
				}
				defer resp.Body.Close()

				fileName = "photo.jpg"
				embd.SetImage("attachment://" + fileName)

				// Set "forwarded from" only for first message in media group
				if LastMediaGroupID != "" && LastMediaGroupID == u.ChannelPost.MediaGroupID && embd != nil {
					embd.SetFooter("")
				}

				m, err = dcbot.ChannelMessageSendComplex(
					conf.Discord.ChannelID,
					&discordgo.MessageSend{
						Embed: embd.MessageEmbed,
						//Content: formatMessage(u.ChannelPost),
						Files: []*discordgo.File{
							{
								Name:        fileName,
								ContentType: "image/jpeg",
								Reader:      resp.Body,
							},
						},
					},
				)
				if err != nil {
					if isTooLargeErr(err) {
						retry.AddOversized(conf, dcbot, newOversizedItem(u.ChannelPost, fileName, size, blankFooter), u.ChannelPost.MediaGroupID)
						return
					}
					log.Printf("Cannot send file! See error: %s", err.Error())

					_, err2 := tgbot.Send(tgbotapi.NewMessage(u.ChannelPost.Chat.ID, err.Error()))
					if err2 != nil {
						log.Print("cannot send tg msg", err2)
					}
					return
				}
			}
		} else if u.ChannelPost.Document != nil {
			fileID = &u.ChannelPost.Document.FileID
			fileName = u.ChannelPost.Document.FileName
			contentType = "application/octet-stream"
		} else if u.ChannelPost.Video != nil {
			fileID = &u.ChannelPost.Video.FileID
			fileName = "video.mp4"
			contentType = "video/mp4"
			//embedSetVideo(embd, "attachment://" + fileName)
		} else if u.ChannelPost.VideoNote != nil {
			fileID = &u.ChannelPost.VideoNote.FileID
			fileName = "videonote.mp4"
			contentType = "video/mp4"
			// Looks like embed videos not works anymore
			//embedSetVideo(embd, "attachment://" + fileName)
		} else if u.ChannelPost.Audio != nil {
			fileID = &u.ChannelPost.Audio.FileID
			fileName = u.ChannelPost.Audio.Performer + " - " + u.ChannelPost.Audio.Title + ".mp3"
			contentType = "audio/mpeg"
		} else if u.ChannelPost.Voice != nil {
			fileID = &u.ChannelPost.Voice.FileID
			fileName = "voice.ogg"
			contentType = "audio/ogg"
		} else if u.ChannelPost.Sticker != nil {
			// Webp image loads as sticker without thumbnail
			if u.ChannelPost.Sticker.Thumbnail != nil {
				fileID = &u.ChannelPost.Sticker.Thumbnail.FileID
				fileName = "sticker.jpg"
				contentType = "image/jpeg"
				embd.SetTitle(u.ChannelPost.Sticker.Emoji)
				embd.SetImage("attachment://" + fileName)
			}
		} else if u.ChannelPost.Poll != nil {
			explanation := ""
			correctOption := make(map[int]string)

			embd.MessageEmbed.Description = fmt.Sprintf("Multiple answers: %v\n", u.ChannelPost.Poll.AllowsMultipleAnswers)
			if !u.ChannelPost.Poll.IsClosed {
				embd.MessageEmbed.Description += fmt.Sprintf("Poll is open.\n")
			}
			if u.ChannelPost.Poll.Type == "quiz" {
				embd.MessageEmbed.Description += fmt.Sprintf("Mode: quiz.\n")
				correctOption[u.ChannelPost.Poll.CorrectOptionID] = "✅"
				explanation = "\nExplanation:\n> " + tgapi.EntitiesToDiscordMarkdown(u.ChannelPost.Poll.Explanation, u.ChannelPost.Poll.ExplanationEntities)
			}
			embd.MessageEmbed.Description += "\n" + u.ChannelPost.Poll.Question
			options := "\n\n"

			for i, option := range u.ChannelPost.Poll.Options {
				options += fmt.Sprintf(" • %s %s (%d)\n", correctOption[i], option.Text, option.VoterCount)
			}
			embd.MessageEmbed.Description += options
			embd.MessageEmbed.Description += fmt.Sprintf("\nVotes: %d\n", u.ChannelPost.Poll.TotalVoterCount)
			embd.MessageEmbed.Description += explanation

			var err error
			m, err = dcbot.ChannelMessageSendEmbed(conf.Discord.ChannelID, embd.MessageEmbed)
			if err != nil {
				log.Printf("Cannot repost your post! See error: %s", err.Error())

				_, err2 := tgbot.Send(tgbotapi.NewMessage(u.ChannelPost.Chat.ID, err.Error()))
				if err2 != nil {
					log.Print("cannot send tg msg", err2)
				}
				return
			}
		}

		// If description is empty then no need embed
		if embd.MessageEmbed.Description == "" && embd.MessageEmbed.Image == nil {
			embd = nil
		}

		// Set "forwarded from" only for first message in media group
		if LastMediaGroupID != "" && LastMediaGroupID == u.ChannelPost.MediaGroupID && embd != nil {
			embd.SetFooter("")
		}
		LastMediaGroupID = u.ChannelPost.MediaGroupID

		if fileID != nil {
			size := mediaFileSize(u.ChannelPost)
			// Telegram refuses to serve bots files above the limit, so skip
			// such files upfront and offer resending the post without them.
			if size > limit {
				retry.AddOversized(conf, dcbot, newOversizedItem(u.ChannelPost, fileName, size, blankFooter), u.ChannelPost.MediaGroupID)
			} else {
				url, err := tgbot.GetFileDirectURL(*fileID)
				if err != nil {
					if isTooLargeErr(err) {
						retry.AddOversized(conf, dcbot, newOversizedItem(u.ChannelPost, fileName, size, blankFooter), u.ChannelPost.MediaGroupID)
						return
					}
					errr := fmt.Errorf("Cannot get direct file URL! Error: %s", err.Error())
					log.Print(errr)

					_, err2 := tgbot.Send(tgbotapi.NewMessage(u.ChannelPost.Chat.ID, errr.Error()))
					if err2 != nil {
						log.Print("cannot send tg msg", err2)
					}
					return
				}

				resp, err := client.Get(url)
				if err != nil {
					errr := fmt.Errorf("Cannot do GET request! See error: %s", err.Error())
					log.Print(errr)

					_, err2 := tgbot.Send(tgbotapi.NewMessage(u.ChannelPost.Chat.ID, errr.Error()))
					if err2 != nil {
						log.Print("cannot send tg msg", err2)
					}
					return
				}
				defer resp.Body.Close()

				files := []*discordgo.File{
					{
						Name:        fileName,
						ContentType: contentType,
						Reader:      resp.Body,
					},
				}
				var messageSend *discordgo.MessageSend
				if embd == nil {
					messageSend = &discordgo.MessageSend{
						Content: formatMessage(u.ChannelPost),
						Files:   files,
					}
				} else {
					messageSend = &discordgo.MessageSend{
						Embed: embd.MessageEmbed,
						Files: files,
					}
				}
				m, err = dcbot.ChannelMessageSendComplex(
					conf.Discord.ChannelID,
					messageSend,
				)
				if err != nil {
					if isTooLargeErr(err) {
						retry.AddOversized(conf, dcbot, newOversizedItem(u.ChannelPost, fileName, size, blankFooter), u.ChannelPost.MediaGroupID)
						return
					}
					errr := fmt.Errorf("Cannot send file! See error: %s", err.Error())
					log.Print(errr)

					_, err2 := tgbot.Send(tgbotapi.NewMessage(u.ChannelPost.Chat.ID, errr.Error()))
					if err2 != nil {
						log.Print("cannot send tg msg", err2)
					}
				}
			}
		}

		if m != nil {
			// Save new record with ids from Telegram and Discord
			pm := database.PostManager{
				DB: db.Conn,
				Data: &database.Post{
					Telegram: postKey(u.ChannelPost),
					Discord:  m.ID,
					IsEmbed:  embd != nil,
				},
			}
			if err := pm.Create(); err != nil {
				log.Printf("Cannot create new record in database! TG: %s. gSee error: %s", err.Error(), pm.Data.Telegram)
			}
		}

		// A standalone post (no media group) is its own batch: flush now.
		if u.ChannelPost.MediaGroupID == "" {
			if items := retry.takeBuffered(); len(items) > 0 {
				postGroupError(conf, dcbot, retry, items)
			}
		}
	} else if u.EditedChannelPost != nil {
		// Find Discord post id by Telegram post id
		pm := database.PostManager{
			DB: db.Conn,
			Data: &database.Post{
				Telegram: postKey(u.EditedChannelPost),
			},
		}
		err := pm.FindByTelegramPost()
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				// Record was cleaned up: post is older than the edit window
				log.Printf("Skipping edit: record not in database (post older than retention)! TG: %s", pm.Data.Telegram)
				return
			}
			log.Printf("Cannot read record in database! See error: %s", err.Error())
			return
		}

		// Edit it with id that we got
		if u.EditedChannelPost.Text != "" || u.EditedChannelPost.Caption != "" {
			if pm.Data.IsEmbed {
				_, err = dcbot.ChannelMessageEditEmbed(conf.Discord.ChannelID, pm.Data.Discord, formatEmbed(u.EditedChannelPost).MessageEmbed)
			} else {
				_, err = dcbot.ChannelMessageEdit(conf.Discord.ChannelID, pm.Data.Discord, u.EditedChannelPost.Caption+u.EditedChannelPost.Text)
			}
			if err != nil {
				log.Printf("Cannot edit repost! See error: %s", err.Error())
			}
		}
	} else if u.Message != nil {
		msg := tgbotapi.NewMessage(u.Message.Chat.ID, "Я просто бот. Какая тебе разница, чем я занят?")
		tgbot.Send(msg)
	}
}
