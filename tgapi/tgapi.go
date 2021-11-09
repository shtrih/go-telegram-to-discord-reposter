package tgapi

import (
	"github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"net/http"
	"reposter/config"
)

func NewBot(conf *config.Config, tr *http.Transport) (*tgbotapi.BotAPI, error) {
	if conf.Proxy != nil {
		return tgbotapi.NewBotAPIWithClient(conf.Telegram.Token, tgbotapi.APIEndpoint, &http.Client{
			Transport: tr,
		})
	} else {
		return tgbotapi.NewBotAPI(conf.Telegram.Token)
	}
}
