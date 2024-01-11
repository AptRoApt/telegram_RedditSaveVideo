package main

import (
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"reddit_save_video/internal/reddit"
	"time"

	tele "gopkg.in/telebot.v3"
)

const errorMessage = "На стороне бота произошла ошибка.\nПожалуйста, напишите @aptroapt чтобы её исправили."

var client *http.Client

func getLinks(c tele.Context) ([]*url.URL, error) {
	var messageURLs []*url.URL = make([]*url.URL, 0, 1)
	for _, e := range c.Message().Entities {
		if e.Type == "url" {
			link, err := url.Parse(c.Message().EntityText(e))
			if err != nil {
				slog.Error("Error during processing of a link from telegram.", "error", err.Error(), "link", link)
				return nil, nil
			}
			if reddit.IsSupported(link) {
				messageURLs = append(messageURLs, link)
			}
		}
	}
	return messageURLs, nil
}

func onTextHandler(c tele.Context) error {
	//Проверка на наличие ссылок.

	//Применим ли здесь reflect?
	messageURLs, err := getLinks(c)
	if err != nil {
		slog.Error("Error during processing of a link from telegram.", "error", err.Error())
		return nil
	}
	if len(messageURLs) == 0 {
		return nil
	}
	//перевести работу с перенаправлениями в клиент?

	message, err := c.Bot().Reply(c.Message(), "Getting links...")
	if err != nil {
		slog.Error("Error on message reply.", "errorText", err.Error())
		return err
	}
	for _, link := range messageURLs {
		//Получаем ссылку на скачивание
		downloadLink, err := reddit.GetDownloadLink(client, link)
		if err != nil {
			if os.IsTimeout(err) {
				_, err = c.Bot().Edit(message, "No connection to reddit.com.")
				return err
			}
			slog.Error("Error during getting download link", "error", err.Error(), "link", link)
			_, err = c.Bot().Edit(message, errorMessage)
			return err
		}
		if downloadLink == nil {
			_, err = c.Bot().Edit(message, "This post doesn't have any video.")
			return err
		}

		message, err = c.Bot().Edit(message, "Sending video...")
		if err != nil {
			slog.Error("Error on message reply.", "errorText", err.Error())
			return err
		}

		cmd := reddit.ComposeffmpegCommand(downloadLink.String())
		stdout, err := cmd.StdoutPipe()

		if err != nil {
			slog.Error("Error while getting pipe.", "error", err.Error())
			return nil
		}

		if err = cmd.Start(); err != nil {
			slog.Error("Starting ffmpeg error", "error", err.Error())
			return nil
		}
		video := &tele.Video{File: tele.FromReader(stdout)}
		r_err := c.Reply(video)
		if err = cmd.Wait(); err != nil {
			slog.Error("ffmpeg error", "error", err.Error())
			return nil
		}
		_ = c.Bot().Delete(message)
		return r_err
	}
	return nil
}

func initializeBot() *tele.Bot {
	// В библиотеке нет проверки на пустой токен.
	pref := tele.Settings{
		Token:  os.Getenv("RSV_TOKEN"),
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	}

	bot, err := tele.NewBot(pref)
	if err != nil {
		slog.Error(err.Error())
		os.Exit(0)
	}
	return bot
}

func main() {
	client = reddit.GetClient()
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	log.Println("Initializing bot...")
	bot := initializeBot() // отключить флаг synchronous?
	log.Println("Bot is initialized.")
	bot.Handle(tele.OnText, onTextHandler)
	log.Println("Start polling.")
	bot.Start()
}
