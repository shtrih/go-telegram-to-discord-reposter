package tgapi

import (
	"github.com/OvyFlash/telegram-bot-api"
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
