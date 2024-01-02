package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"time"

	tele "gopkg.in/telebot.v3"
)

var redditHeader = http.Header{
	"Accept":                    {"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8"},
	"Accept-Encoding":           {"gzip"},
	"Accept-Language":           {"en-US,en;q=0.5"},
	"Connection":                {"keep-alive"},
	"Host":                      {"www.reddit.com"},
	"Sec-Fetch-Dest":            {"document"},
	"Sec-Fetch-Mode":            {"navigate"},
	"Sec-Fetch-Site":            {"cross-site"},
	"Sec-GPC":                   {"1"},
	"Upgrade-Insecure-Requests": {"1"},
	"User-Agent":                {"Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0"},
}

var redditPostRegex = regexp.MustCompile("https://www.reddit.com/r/[^/]*/(comments|s)/.*")

func IsSupported(link *url.URL) bool {
	return redditPostRegex.MatchString(link.String())
}

func MakeJSONLink(link *url.URL) *url.URL {
	link.RawQuery = ""
	return link.JoinPath("/.json")
}

// В библиотеке нет проверки на пустой токен.
func initializeBot() *tele.Bot {
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

func getDownloadLink(c *http.Client, link *url.URL) (*url.URL, error) {

	//А обработать код ответа?)
	//а он может ответить в gzip)
	req, err := http.NewRequest("GET", link.String(), nil)
	if err != nil {
		slog.Error("Ошибка при создании запроса.", "error", err.Error(), "link", link)
		return nil, err
	}
	req.Header = redditHeader
	resp, err := c.Do(req)
	if err != nil {
		slog.Error("Ошибка при получении json.", "error", err.Error(), "link", link)
		return nil, err
	}
	defer resp.Body.Close()

	gz, err := gzip.NewReader(resp.Body)
	defer gz.Close()

	dec := json.NewDecoder(gz)

	for {
		t, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			slog.Error("Ошибка при обработкe JSON", "error", err.Error(), "link", link)
			return nil, err
		}
		if t == "secure_media" {
			t, _ = dec.Token()
			if t == nil {
				return nil, nil
			}
			t, _ = dec.Token() // reddit_video
			if t != "reddit_video" {
				slog.Error("В secure_media МОЖЕТ БЫТЬ НЕ ТОЛЬКО reddit_video, но и %v!", t)
			}
			for {
				t, _ = dec.Token()
				if t == "hls_url" {
					t, _ = dec.Token()
					link, err := url.Parse(t.(string))
					if err != nil {
						slog.Error("Ошибка при обработкe ссылки на видео", "error", err.Error())
						return nil, err
					}
					return link, nil
				}
			}
		}
	}
	return nil, nil
}

func onTextHandler(c tele.Context) error {
	//проверка на наличие ссылок

	//Применим ли здесь reflect?
	var messageURLs []*url.URL = make([]*url.URL, 0, 1)
	for _, e := range c.Message().Entities {
		if e.Type == "url" {
			link, err := url.Parse(c.Message().EntityText(e))
			if err != nil {
				slog.Warn("Ошибка при обработки ссылки от телеграмма.", "error", err.Error(), "link", link)
				return nil
			}
			if IsSupported(link) {
				link = MakeJSONLink(link)
				messageURLs = append(messageURLs, link)
			}
		}
	}
	if len(messageURLs) == 0 {
		return nil
	}
	client := &http.Client{Timeout: 10 * time.Second}
	for _, link := range messageURLs {
		//Получаем ссылку на скачивание
		downloadLink, err := getDownloadLink(client, link)
		if err != nil {
			if os.IsTimeout(err) {
				return c.Reply("Нет подключения к реддиту.")
			}
			return c.Reply("На стороне бота произошла ошибка при получении видео.\nПожалуйста, напишите @aptroapt чтобы её исправили.")
		}
		if downloadLink == nil {
			return nil
		}
		return c.Reply(fmt.Sprintf("Тут должен быть выбор качества, но я пока пошёл разбираться с ffmpeg.\nВот хотя бы ссылка на m3u8: %v", downloadLink))
	}
	return nil
}

func main() {
	log.Println("Initializing bot...")
	bot := initializeBot() // отключить флаг synchronous
	log.Println("Bot is initialized.")
	bot.Handle(tele.OnText, onTextHandler)
	log.Println("Start polling.")
	bot.Start()
}
