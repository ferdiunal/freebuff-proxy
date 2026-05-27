// Package httpapi, OpenAI uyumlu HTTP uç noktalarını Fiber v3 uygulaması olarak sunar.
//
// ## Kullanım örneği
//
// ```go
// app := httpapi.NewApp(httpapi.Options{Model: "deepseek-v3.1-terminus"})
// err := app.Listen("127.0.0.1:1455")
// ```
package httpapi

import "github.com/gofiber/fiber/v3"

// Options, HTTP API sunucusunun dış bağımlılıklarını ve çalışma ayarlarını taşır.
//
// ## Kullanım örneği
//
// ```go
//
//	app := httpapi.NewApp(httpapi.Options{
//		Model:       "deepseek-v3.1-terminus",
//		ProxyAPIKey: "local-secret",
//		Chat:        chatService,
//	})
//
// ```
type Options struct {
	Model       string
	ProxyAPIKey string
	Chat        ChatService
}

// NewApp, sağlık, model listeleme ve sohbet tamamlama rotalarını Fiber v3 ile kurar.
//
// ## Kullanım örneği
//
// ```go
// app := httpapi.NewApp(httpapi.Options{Model: "deepseek-v3.1-terminus"})
// err := app.Listen("127.0.0.1:1455")
// ```
func NewApp(opts Options) *fiber.App {
	app := fiber.New(fiber.Config{AppName: "freebuff-proxy"})
	handlers := newHandlers(opts.Model, opts.Chat)

	if opts.ProxyAPIKey != "" {
		app.Use(authMiddleware(opts.ProxyAPIKey))
	}

	app.Get("/healthz", handlers.Health)
	app.Get("/v1/models", handlers.Models)
	app.Post("/v1/chat/completions", handlers.ChatCompletions)
	app.Post("/v1/messages", handlers.AnthropicMessages)
	app.Post("/v1/messages/count_tokens", handlers.AnthropicCountTokens)

	return app
}
