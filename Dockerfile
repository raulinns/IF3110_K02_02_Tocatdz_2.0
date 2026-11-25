# Image server dan worker: SPA di-build lalu ditanam ke binary Go.
FROM node:22-alpine AS web
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN mkdir -p /app/src/internal/spa/dist && npm run build:embed

FROM golang:1.27-alpine AS build
WORKDIR /app/src
COPY src/go.mod src/go.sum ./
RUN go mod download
COPY src/ ./
COPY --from=web /app/src/internal/spa/dist ./internal/spa/dist
RUN CGO_ENABLED=0 go build -trimpath -o /out/server ./cmd/server \
	&& CGO_ENABLED=0 go build -trimpath -o /out/worker ./cmd/worker \
	&& CGO_ENABLED=0 go build -trimpath -o /out/seed ./cmd/seed \
	&& CGO_ENABLED=0 go build -trimpath -o /out/keygen ./cmd/keygen

FROM alpine:3.22
RUN adduser -D -u 10001 app
COPY --from=build /out/ /usr/local/bin/
USER app
EXPOSE 8080
CMD ["server"]
