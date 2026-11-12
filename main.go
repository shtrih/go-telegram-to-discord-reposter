package main // import "reposter"

import (
	"flag"
	"fmt"
	"github.com/OvyFlash/telegram-bot-api"
	"net/http"
	"os"
	"os/signal"
	"reposter/config"
	"reposter/database"
	"reposter/dcapi"
	"reposter/handler"
	"reposter/proxy"
	"reposter/tgapi"
	"time"

	"github.com/bwmarrin/discordgo"
)

var (
	path = flag.String(
		"config",
		"",
		"enter path to config file",
	)
)

func main() {
	// Parse at first startup
	flag.Parse()

	// Read config
	conf, err := config.NewConfig(*path)
	if err != nil {
		fmt.Println("Incorrect path or config itself! See help.")
		os.Exit(2)
	}

	// Init discord api
	dcbot, err := dcapi.NewSession(conf)
	if err != nil {
		fmt.Println("Discord bot cannot be initialized! See, error:")
		panic(err)
	}

	var tr *http.Transport

	// http client with proxy
	var client *http.Client
	if conf.Proxy == nil {
		client = &http.Client{}
	} else {
		// Init http proxy transport
		tr = proxy.NewProxyTransport(conf)
		client = &http.Client{
			Transport: tr,
		}
	}

	// Init telegram api
	tgbot, err := tgapi.NewBot(conf, tr)
	if err != nil {
		fmt.Println("Telegram bot cannot be initialized! See, error:")
		panic(err)
	}

	tgbot.Debug = conf.Debug == "true"

	fmt.Printf("Authorized on account @%s\n", tgbot.Self.UserName)

	// Init database
	db, err := database.NewDatabase(conf)
	if err != nil {
		fmt.Println("Database cannot be initialized! See, error:")
		panic(err)
	}

	// Try auto migration for first start
	err = db.AutoMigrate()
	if err != nil {
		fmt.Println("Cannot auto migrate! See, error:")
		panic(err)
	}

	// Skipped oversized files and their resend buttons. Interaction events
	// (button clicks) arrive on discordgo goroutines.
	retry := handler.NewRetryStore()
	dcbot.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		handler.HandleInteraction(conf, db, s, retry, i)
	})

	// Clean records older than the edit window once and every hour after
	cleanup := func() {
		if n, err := db.Cleanup(conf.EditWindow); err != nil {
			fmt.Println("Database cleanup failed! See, error:")
			fmt.Println(err)
		} else if n > 0 {
			fmt.Printf("Database cleanup: removed %d old record(s)\n", n)
		}
	}
	cleanup()
	go func() {
		for range time.Tick(time.Hour) {
			cleanup()
		}
	}()

	uc := tgbotapi.NewUpdate(0)
	uc.Timeout = 60

	updates := tgbot.GetUpdatesChan(uc)

	// Graceful shutdown
	s := make(chan os.Signal, 1)
	signal.Notify(s, os.Interrupt, os.Kill)

	go func() {
		<-s
		updates.Clear()
		dcbot.Close()
		os.Exit(1)
	}()

	// Main loop, check all changes in Telegram Channel
	for u := range updates {
		handler.HandleUpdate(conf, db, client, tgbot, dcbot, retry, u)
	}
}
