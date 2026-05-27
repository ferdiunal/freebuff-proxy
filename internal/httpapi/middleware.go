// Bu dosya, proxy API anahtarı denetimini OpenAI uyumlu hata zarfıyla uygular.
//
// ## Kullanım örneği
//
// ```go
// app.Use(authMiddleware("local-secret"))
// ```
package httpapi

import (
	"net/http"

	"github.com/gofiber/fiber/v3"
)

const bearerPrefix = "Bearer "

func authMiddleware(proxyAPIKey string) fiber.Handler {
	return func(c fiber.Ctx) error {
		if c.Get(fiber.HeaderAuthorization) != bearerPrefix+proxyAPIKey && c.Get("x-api-key") != proxyAPIKey {
			return writeOpenAIError(c, http.StatusUnauthorized, "invalid_api_key", "Geçerli bir Bearer API anahtarı gerekli")
		}

		return c.Next()
	}
}
