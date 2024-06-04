package redditVideoDownloader

import (
	"context"
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

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

const (
	userAgentHeader = "User-Agent"
	tokenURL        = "https://www.reddit.com/api/v1/access_token"
)

var redditRefRegex = regexp.MustCompile("https://www.reddit.com/r/[^/]*/(s|comments)/.*")

type redditTransport struct {
	UserAgent string
	transport *http.Transport
}

func (rt redditTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Add(userAgentHeader, rt.UserAgent)
	return http.DefaultTransport.RoundTrip(req)
}

type RedditVideoDownloader struct {
	client    *http.Client
	cfg       *clientcredentials.Config
	regex     *regexp.Regexp
	userAgent string
}

func (rvd *RedditVideoDownloader) initializeClient() {
	if rvd == nil {
		slog.Error("why is it nil?!")
		return
	}
	rvd.client = &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},

		Transport: &oauth2.Transport{Source: rvd.cfg.TokenSource(context.Background()), Base: redditTransport{UserAgent: rvd.userAgent}},

		Timeout: 30 * time.Second,
	}
}

func NewRedditVideoDownloader(userAgent string, clientID string, clientSecret string) *RedditVideoDownloader {
	rvd := &RedditVideoDownloader{
		cfg: &clientcredentials.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			TokenURL:     tokenURL,
		},
		userAgent: userAgent,
	}
	rvd.initializeClient()
	return rvd
}

func (rvd *RedditVideoDownloader) IsRedditPost(link *url.URL) bool {
	return redditRefRegex.MatchString(link.String())
}

func jsonfyLink(link *url.URL) *url.URL {
	link.RawQuery = ""
	link = link.JoinPath("/.json")
	link.Host = "oauth.reddit.com"
	return link
}

func getDownloadLink(r io.Reader) (*url.URL, error) {
	dec := json.NewDecoder(r)
	for {
		t, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			slog.Error("Ошибка при обработкe JSON", "error", err.Error())
			b := new(strings.Builder)
			_, _ = io.Copy(b, r)
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

// поменять link на string
func (rvd *RedditVideoDownloader) GetDownloadLink(link *url.URL) (*url.URL, error) {
	//Запрашиваем заголовки, чтобы получить ссылку-перенаправление
	resp, err := rvd.client.Head(link.String())
	if err != nil {
		slog.Error("Error while doing head request.")
		return nil, err
	}
	//Обрабатываем перенаправление на пост, меняя link.
	if resp.StatusCode == 301 {
		unparsedLink := resp.Header.Get("Location")
		if unparsedLink == "" {
			err = errors.New("reddit has returned empty location")
			slog.Error(err.Error())
			return nil, err
		}
		link, err = url.Parse(unparsedLink)
		if err != nil {
			slog.Error("Error while parsing redditVideoDownloader location link.", "errorText", err.Error())
			return nil, err
		}
	}
	//TODO: обработка 403, например.
	link = jsonfyLink(link)
	resp, err = rvd.client.Get(link.String())
	if err != nil {
		slog.Error("Making GET request error.", "error", err.Error(), "link", link)
		return nil, err
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "application/json; charset=UTF-8" || resp.StatusCode != 200 {
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

	return getDownloadLink(resp.Body)
}

func (rvd *RedditVideoDownloader) composeffmpegCommand(link string, filename string) *exec.Cmd {
	//TODO: Добавить обработку ошибок и работу через именованный pipe?
	cmd := exec.Command("ffmpeg",
		"-loglevel", "panic",
		"-i", link,
		"-movflags", "+faststart",
		"-c:v", "libx264",
		"-max_muxing_queue_size", "9999",
		"-maxrate", "4.5M",
		"-crf", "23",
		"-preset", "faster",
		"-flags", "+global_header",
		"-pix_fmt", "yuv420p",
		"-profile:v", "baseline",
		"-c:a", "aac",
		"-ac", "2",
		filename)

	token, err := rvd.cfg.Token(context.Background())
	if err != nil {
		slog.Error("error", err.Error())
		return nil
	}
	cmd.Args = append(cmd.Args, "-headers", fmt.Sprintf("\"Authorization: %v %v\"", token.TokenType, token.AccessToken))
	return cmd
}

func (rvd *RedditVideoDownloader) GetVideo(link *url.URL) (string, error) {
	filename := fmt.Sprintf("%v.mp4", time.Now().Unix())
	cmd := rvd.composeffmpegCommand(link.String(), filename)
	if cmd == nil {
		return "", fmt.Errorf("token не получен.")
	}
	return filename, cmd.Run()
}
