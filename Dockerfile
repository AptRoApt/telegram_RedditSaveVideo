FROM golang:bullseye
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
RUN apt-get update && apt-get upgrade && apt-get install -y ffmpeg
COPY main.go ./
COPY internal/redditVideoDownloader/redditVideoDownloader.go ./internal/redditVideoDownloader/
RUN CGO_ENABLED=0 GOOS=linux go build -o ./reddit_save_video
CMD ["./reddit_save_video"]