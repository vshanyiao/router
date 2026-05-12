FROM golang:1.25-alpine

WORKDIR /app

# Pin air to a known-working version. `@latest` would track the moving tip
# and broke once when air bumped its Go requirement.
RUN go install github.com/air-verse/air@v1.65.1

COPY go.mod go.sum* ./
RUN go mod download

COPY . .

EXPOSE 8080

CMD ["air"]
