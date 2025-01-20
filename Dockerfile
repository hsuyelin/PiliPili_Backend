FROM mirrors.dilidili.work/golang:1.23.5-alpine3.21

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

CMD ["go", "run", "main.go", "config.yaml"]