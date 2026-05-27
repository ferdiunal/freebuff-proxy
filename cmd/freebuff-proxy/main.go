package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ferdiunal/freebuff-proxy/internal/app"
	"github.com/ferdiunal/freebuff-proxy/internal/config"
	"github.com/ferdiunal/freebuff-proxy/internal/credentials"
	"github.com/ferdiunal/freebuff-proxy/internal/oauth"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	_ = ctx

	if len(args) == 0 {
		args = []string{"serve"}
	}

	switch args[0] {
	case "serve":
		return serve(ctx, args[1:])
	case "login":
		return login(ctx, args[1:])
	case "logout":
		return logout(ctx, args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

// serve, yapılandırmayı yükler ve proxy Fiber uygulamasını başlatır.
//
// ## Kullanım örneği
//
// ```bash
// freebuff-proxy serve
// FREEBUFF_PROXY_ADDR=0.0.0.0:8080 freebuff-proxy serve
// ```
func serve(ctx context.Context, args []string) error {
	_ = ctx
	_ = args

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	fiberApp, err := app.NewApp(cfg)
	if err != nil {
		return err
	}

	return fiberApp.Listen(cfg.Addr)
}

// login, OAuth doğrulama URL'ini yazdırır ve başarılı olunca credential dosyasını kaydeder.
//
// ## Kullanım örneği
//
// ```bash
// freebuff-proxy login
// FREEBUFF_API_BASE_URL=https://www.codebuff.com freebuff-proxy login
// ```
func login(ctx context.Context, args []string) error {
	_ = args

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	flow := oauth.Flow{
		BaseURL: cfg.APIBaseURL,
		Store:   credentials.FileStore{Path: cfg.CredentialsPath},
	}
	code, err := flow.RequestLoginCode(ctx)
	if err != nil {
		return err
	}

	fmt.Fprintln(os.Stdout, "Freebuff giriş bağlantısı:")
	fmt.Fprintln(os.Stdout, code.LoginURL)
	fmt.Fprintln(os.Stdout, "Tarayıcıda oturumu tamamladıktan sonra bekleniyor...")

	cred, err := flow.PollLoginStatus(ctx, code)
	if err != nil {
		return err
	}

	if cred.Name != "" && cred.Email != "" {
		fmt.Fprintf(os.Stdout, "Giriş başarılı: %s (%s)\n", cred.Name, cred.Email)
		return nil
	}
	fmt.Fprintln(os.Stdout, "Giriş başarılı.")
	return nil
}

// logout, kayıtlı metadata ile API logout çağrısı yapar ve yerel credential dosyasını temizler.
//
// ## Kullanım örneği
//
// ```bash
// freebuff-proxy logout
// FREEBUFF_CREDENTIALS_PATH=/tmp/credentials.json freebuff-proxy logout
// ```
func logout(ctx context.Context, args []string) error {
	_ = args

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	flow := oauth.Flow{
		BaseURL: cfg.APIBaseURL,
		Store:   credentials.FileStore{Path: cfg.CredentialsPath},
	}
	if err := flow.Logout(ctx); err != nil {
		return err
	}

	fmt.Fprintln(os.Stdout, "Çıkış yapıldı. Yerel kimlik durumu temizlendi.")
	return nil
}
