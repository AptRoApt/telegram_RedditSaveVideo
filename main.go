package main

import (
	"log"
	"log/slog"
	"net/url"
	"os"
	"reddit_save_video/internal/redditVideoDownloader"
	"time"

	tele "gopkg.in/telebot.v3"
)

const errorMessage = "На стороне бота произошла ошибка.\nПожалуйста, напишите @aptroapt чтобы её исправили."

var rvd = redditVideoDownloader.NewRedditVideoDownloader("My_Test_App/0.1 by Clean_Wheel4197", os.Getenv("My_Test_Bot_ID"), os.Getenv("My_Test_Bot_Secret"))

func getLinks(c tele.Context) ([]*url.URL, error) {
	var messageURLs []*url.URL = make([]*url.URL, 0, 1)
	for _, e := range c.Message().Entities {
		if e.Type == "url" {
			link, err := url.Parse(c.Message().EntityText(e))
			if err != nil {
				slog.Error("Error during processing of a link from telegram.", "error", err.Error(), "link", link)
				return nil, nil
			}
			if rvd.IsRedditPost(link) {
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

	message, err := c.Bot().Reply(c.Message(), "Getting video...")
	if err != nil {
		slog.Error("Error on message reply.", "errorText", err.Error())
		return err
	}

	for _, link := range messageURLs {
		//Получаем ссылку на скачивание
		downloadLink, err := rvd.GetDownloadLink(link)
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

		videoPath, err := rvd.GetVideo(downloadLink)
		if err != nil {
			slog.Error("FFMPEG error", "errorText", err.Error())
			return nil
		}
		defer func() {
			err = os.Remove(videoPath)
			if err != nil {
				slog.Error(err.Error())
			}
		}()
		video := &tele.Video{File: tele.FromDisk(videoPath)}
		r_err := c.Reply(video)
		_ = c.Bot().Delete(message)
		return r_err
	}
	return nil
}

func initializeBot() *tele.Bot {
	// В библиотеке нет проверки на пустой токен.
	pref := tele.Settings{
		Token:  os.Getenv("RSV_TOKEN"),
		Poller: &tele.LongPoller{Timeout: 30 * time.Second},
	}

	bot, err := tele.NewBot(pref)
	if err != nil {
		slog.Error(err.Error())
		os.Exit(0)
	}
	return bot
}

func main() {
	log.Println("Initializing bot...")
	bot := initializeBot()
	log.Println("Bot is initialized.")
	bot.Handle(tele.OnText, onTextHandler)
	log.Println("Start polling.")
	bot.Start()
}
