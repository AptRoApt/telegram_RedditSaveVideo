FROM golang:bullseye
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
RUN apt-get update && apt-get upgrade && apt-get install -y ffmpeg
COPY main.go ./
COPY internal/redditVideoDownloader/redditVideoDownloader.go ./internal/redditVideoDownloader/
RUN CGO_ENABLED=0 GOOS=linux go build -o ./reddit_save_video
ENV RSV_TOKEN=${RSV_TOKEN}
ENV My_Test_Bot_ID=${My_Test_Bot_ID}
ENV My_Test_Bot_Secret=${My_Test_Bot_Secret}
CMD ["./reddit_save_video"]