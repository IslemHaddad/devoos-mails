# Build manuel/local. La CI utilise ko (pas de daemon Docker), mais ce
# Dockerfile reste utile pour `docker build` en dehors du pipeline.
FROM golang:1.22 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /devops-mails .

FROM gcr.io/distroless/static:nonroot
COPY --from=build /devops-mails /devops-mails
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/devops-mails", "--addr=:8080", "--config=/data/smtp-config.json"]
