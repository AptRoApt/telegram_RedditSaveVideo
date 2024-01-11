package reddit

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var redditRefRegex = regexp.MustCompile("https://www.reddit.com/r/[^/]*/(s|comments)/.*")
var headers = map[string][]string{
	"User-Agent":                {"Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0"},
	"Accept":                    {"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8"},
	"Accept-Language":           {"en-US,en;q=0.5"},
	"DNT":                       {"1"},
	"Sec-GPC":                   {"1"},
	"Connection":                {"keep-alive"},
	"Upgrade-Insecure-Requests": {"1"},
	"Sec-Fetch-Dest":            {"document"},
	"Sec-Fetch-Mode":            {"navigate"},
	"Sec-Fetch-Site":            {"none"},
	"Sec-Fetch-User":            {"?1"},
	"TE":                        {"trailers"},
}

func IsSupported(link *url.URL) bool {
	return redditRefRegex.MatchString(link.String())
}

func GetDownloadLink(c *http.Client, link *url.URL) (*url.URL, error) {
	//Запрашиваем заголовки, чтобы получить ссылку-перенаправление
	req, err := http.NewRequest("HEAD", link.String(), nil)
	if err != nil {
		slog.Error("Creating HEAD request error.", "error", err.Error(), "link", link)
		return nil, err
	}
	req.Header = headers
	resp, err := c.Do(req)
	if err != nil {
		slog.Error("Making HEAD request error.", "error", err.Error(), "link", link)
		return nil, err
	}
	//https://stackoverflow.com/questions/68515077/http-client-doesnt-return-response-when-status-is-301-without-a-location-header

	//Обрабатываем перенаправление на пост, меняя link.
	if resp.StatusCode == 301 {
		unparsedLink := resp.Header.Get("Location")
		if unparsedLink == "" {
			err = errors.New("Reddit has returned empty location")
			slog.Error(err.Error())
			return nil, err
		}
		link, err = url.Parse(unparsedLink)
		if err != nil {
			slog.Error("Error while parsing reddit location link.", "errorText", err.Error())
			return nil, err
		}
	}
	//TODO: обработка 403, например.
	link.RawQuery = ""
	link = link.JoinPath("/.json")
	req.Method = "GET"
	req.URL = link
	resp, err = c.Do(req)
	if err != nil {
		slog.Error("Making GET request error.", "error", err.Error(), "link", link)
		return nil, err
	}
	defer resp.Body.Close()

	dec := json.NewDecoder(resp.Body)

	if ct := resp.Header.Get("Content-Type"); ct != "application/json; charset=UTF-8" {
		err = errors.New("Unexpected response from reddit.")
		filename := time.Now().String()
		if ct == "text/html" {
			errorPage, _ := os.Create(filename)
			b, _ := io.ReadAll(resp.Body)
			_, _ = errorPage.Write(b)
		}
		slog.Error(err.Error(), "link", link, "errorPage", filename)
		return nil, err
	}

	for {
		t, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			slog.Error("Ошибка при обработкe JSON", "error", err.Error(), "link", link)
			b := new(strings.Builder)
			_, _ = io.Copy(b, resp.Body)
			slog.Error(b.String())
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

func ComposeffmpegCommand(link string) *exec.Cmd {
	//TODO: Добавить обработку ошибок и работу через именованный pipe?
	cmd := exec.Command("ffmpeg", "-loglevel", "panic", "-i", link, "-movflags", "frag_keyframe+empty_moov", "-f", "mp4", "pipe:1")
	for k, v := range headers {
		cmd.Args = append(cmd.Args, "-headers", fmt.Sprintf("%v: %v", k, v[0]))
	}
	return cmd
}