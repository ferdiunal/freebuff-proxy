// Package app, yapılandırmadan çalıştırılabilir Fiber uygulamasını kurar.
//
// ## Kullanım örneği
//
// ```go
// cfg, err := config.Load()
// if err != nil { return err }
// fiberApp, err := app.NewApp(cfg)
// if err != nil { return err }
// _ = fiberApp.Listen(cfg.Addr)
// ```
package app

import (
	"github.com/gofiber/fiber/v3"

	"github.com/ferdiunal/freebuff-proxy/internal/config"
	"github.com/ferdiunal/freebuff-proxy/internal/credentials"
	"github.com/ferdiunal/freebuff-proxy/internal/freebuff"
	"github.com/ferdiunal/freebuff-proxy/internal/httpapi"
	"github.com/ferdiunal/freebuff-proxy/internal/session"
)

const defaultInstanceID = "freebuff-proxy"

// NewApp, uygulama bağımlılıklarını doğrular ve HTTP API Fiber uygulamasını döndürür.
//
// ## Kullanım örneği
//
// ```go
// fiberApp, err := app.NewApp(cfg)
//
//	if err != nil {
//		return err
//	}
//
// err = fiberApp.Listen(cfg.Addr)
// ```
func NewApp(cfg config.Config) (*fiber.App, error) {
	store := credentials.FileStore{Path: cfg.CredentialsPath}
	client, err := freebuff.NewClient(cfg.APIBaseURL, nil)
	if err != nil {
		return nil, err
	}
	manager := session.NewManager(store, client, defaultInstanceID)
	chat := httpapi.FreebuffChatService{
		Store:    store,
		Sessions: manager,
		Upstream: client,
	}

	return httpapi.NewApp(httpapi.Options{
		Model:       cfg.Model,
		ProxyAPIKey: cfg.ProxyAPIKey,
		Chat:        chat,
	}), nil
}
