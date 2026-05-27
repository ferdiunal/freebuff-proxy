package security

// RedactToken, günlüklerde ve hata mesajlarında kullanılacak token görünümünü kısaltır.
//
// ## Kullanım örneği
//
// ```go
// masked := security.RedactToken("42d7350000000000000000000000a223")
// fmt.Println(masked)
// // Output: 42d735…a223
// ```
func RedactToken(token string) string {
	if token == "" {
		return ""
	}

	if len(token) <= 10 {
		return "****"
	}

	return token[:6] + "…" + token[len(token)-4:]
}
