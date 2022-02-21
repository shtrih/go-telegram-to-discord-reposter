package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"reposter/config"
	"reposter/database"
	"reposter/tgapi"
	"time"

	embed "github.com/Clinet/discordgo-embed"
	"github.com/bwmarrin/discordgo"
	"github.com/go-telegram-bot-api/telegram-bot-api/v5"
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

func embedSetTimestamp(e *embed.Embed, sec int) {
	e.MessageEmbed.Timestamp = time.Unix(int64(sec), 0).Format(time.RFC3339)
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
	if msg.ForwardFromChat != nil {
		chatName := ""
		if msg.ForwardFromChat.UserName != "" {
			chatName = fmt.Sprintf(" (@%s)", msg.ForwardFromChat.UserName)
		}
		forwardedFrom = fmt.Sprintf("forwarded from «%s»%s", msg.ForwardFromChat.Title, chatName)
	} else if msg.ForwardFrom != nil {
		lastName := ""
		if msg.ForwardFrom.LastName != "" {
			lastName = " " + msg.ForwardFrom.LastName
		}
		forwardedFrom = fmt.Sprintf("forwarded from %s%s", msg.ForwardFrom.FirstName, lastName)
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

func formatEmbed(msg *tgbotapi.Message) *embed.Embed {
	forwardedFrom := getForwardedFrom(msg)
	result := embed.NewEmbed().
		//SetTitle(getAuthorSignature(msg) + getForwardedFrom(msg)).
		SetFooter(getAuthorSignature(msg) + forwardedFrom).
		SetColor(0x30a3e6).
		Truncate()

	// Hide Telegram internal links if forward source hidden by user
	var textEntities, captionEntities []tgbotapi.MessageEntity
	text, textCaption := msg.Text, msg.Caption
	if forwardedFrom == "" {
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
	if forwardedFrom == "" {
		var re = regexp.MustCompile(`(https[:]//t[.]me/)[^\s]+\b`)
		text = re.ReplaceAllString(text, `$1#link-hidden-in-discord`)
		textCaption = re.ReplaceAllString(textCaption, `$1#link-hidden-in-discord`)
	}

	// embed.SetDescription() truncates text at 2048, but actual limit is 4096
	// https://discord.com/developers/docs/resources/channel#embed-limits
	// For now TG text limit also 4096 so no need any truncations.
	// https://core.telegram.org/bots/api#message
	result.Description = text
	result.Description += textCaption

	if msg.ForwardDate == 0 {
		embedSetTimestamp(result, msg.Date)
	} else {
		embedSetTimestamp(result, msg.ForwardDate)
	}

	return result
}

var LastMediaGroupID string

func HandleUpdate(conf *config.Config, db *database.Database, client *http.Client, tgbot *tgbotapi.BotAPI, dcbot *discordgo.Session, u tgbotapi.Update) {
	if u.ChannelPost != nil {
		var m *discordgo.Message

		var fileID *string
		var fileName string
		var contentType string
		embd := formatEmbed(u.ChannelPost)

		// Send repost to Discord text channel
		if u.ChannelPost.Text != "" {
			var err error
			m, err = dcbot.ChannelMessageSendEmbed(conf.Discord.ChannelID, embd.MessageEmbed)
			if err != nil {
				log.Printf("Cannot repost your post! See error: %s", err.Error())
				return
			}
		} else if u.ChannelPost.Photo != nil {
			if len(u.ChannelPost.Photo) > 0 {
				p := u.ChannelPost.Photo
				url, err := tgbot.GetFileDirectURL(p[len(u.ChannelPost.Photo)-1].FileID)
				if err != nil {
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
					log.Printf("Cannot send file! See error: %s", err.Error())
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
			url, err := tgbot.GetFileDirectURL(*fileID)
			if err != nil {
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
				errr := fmt.Errorf("Cannot send file! See error: %s", err.Error())
				log.Print(errr)

				_, err2 := tgbot.Send(tgbotapi.NewMessage(u.ChannelPost.Chat.ID, errr.Error()))
				if err2 != nil {
					log.Print("cannot send tg msg", err2)
				}
			}
		}

		if m != nil {
			// Save new record with ids from Telegram and Discord
			pm := database.PostManager{
				DB: db.Conn,
				Data: &database.Post{
					Telegram: fmt.Sprintf("%d,%d", u.EditedChannelPost.Chat.ID, u.ChannelPost.MessageID),
					Discord:  m.ID,
					IsEmbed:  embd != nil,
				},
			}
			if err := pm.Create(); err != nil {
				log.Printf("Cannot create new record in database! TG: %s. gSee error: %s", err.Error(), pm.Data.Telegram)
			}
		}
	} else if u.EditedChannelPost != nil {
		// Find Discord post id by Telegram post id
		pm := database.PostManager{
			DB: db.Conn,
			Data: &database.Post{
				Telegram: fmt.Sprintf("%d,%d", u.EditedChannelPost.Chat.ID, u.EditedChannelPost.MessageID),
			},
		}
		err := pm.FindByTelegramPost()
		if err != nil {
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
