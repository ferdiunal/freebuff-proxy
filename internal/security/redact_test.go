package security

import "testing"

// Bu testler, token maskeleme kurallarının kısa ve uzun girdilerde korunduğunu doğrular.
//
// ## Kullanım örneği
//
// ```bash
// go test ./internal/security
// go test ./internal/security -run TestRedactToken
// ```
func TestRedactToken(t *testing.T) {
	testCases := []struct {
		name  string
		token string
		want  string
	}{
		{
			name:  "empty token",
			token: "",
			want:  "",
		},
		{
			name:  "short token",
			token: "1234567890",
			want:  "****",
		},
		{
			name:  "long token",
			token: "42d7350000000000000000000000a223",
			want:  "42d735…a223",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactToken(tc.token)
			if got != tc.want {
				t.Fatalf("RedactToken(%q) = %q, beklenen %q", tc.token, got, tc.want)
			}
		})
	}
}
