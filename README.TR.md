# freebuff-proxy

`freebuff-proxy`, Freebuff/Codebuff oturum altyapısını yerel bir OpenAI ve Anthropic uyumlu HTTP yüzeyi arkasına alan küçük bir Go servisidir. Amaç; CLI ile Freebuff hesabına giriş yapmak, `credentials.json` içindeki kimlik bilgisini güvenli biçimde kullanmak, oturumu hazır tutmak ve istemcilere `/v1/models`, `/v1/chat/completions`, `/v1/messages` ve `/v1/messages/count_tokens` biçiminde uyumlu endpointler sunmaktır.

Chat endpointleri OpenAI veya Anthropic biçimli istek kabul eder, Freebuff oturumunu hazırlar, Codebuff CLI gibi önce `/api/v1/agent-runs` üzerinde agent run başlatır, `codebuff_metadata` ile doğrulanmış upstream chat rotasına istek gönderir ve yanıtı istemci sözleşmesine göre `chat.completion`, OpenAI SSE chunk veya Anthropic Messages/SSE formatına dönüştürür.

## Kurulum

Gereksinimler:

- Go 1.25 veya üzeri
- Freebuff/Codebuff hesabı
- Docker ile container imajı üretilecekse Docker CLI ve çalışan Docker daemon

HTTP sunucu katmanı Fiber v3 ile çalışır. Yapılandırma yüklenirken proje kökündeki `.env` dosyası `github.com/joho/godotenv` ile otomatik okunur; process env değerleri `.env` içindeki değerlerden önceliklidir.

Kaynak koddan doğrulama ve derleme:

```bash
go test ./...
go vet ./...
go build -o bin/freebuff-proxy ./cmd/freebuff-proxy
```

Makefile hedefleri:

```bash
make test
make vet
make build
make run
make run-bin
make doctor
make smoke-openai
make smoke-anthropic
make docker
```

`make build`, ikili dosyayı `bin/freebuff-proxy` olarak üretir. `make run-bin`, önce bu binary'yi üretip ardından `./bin/freebuff-proxy serve` çalıştırır. `make doctor`, port `1455` üzerinde çalışan süreci ve stale root binary izlerini gösterir. `make smoke-openai` ve `make smoke-anthropic`, çalışan yerel proxy'ye örnek istek gönderir. Doğrudan kurulum istenirse proje kökünde şu komut kullanılabilir:

```bash
go install ./cmd/freebuff-proxy
```

## Ortam değişkenleri

| Değişken | Varsayılan | Açıklama |
| --- | --- | --- |
| `FREEBUFF_PROXY_ADDR` | `127.0.0.1:1455` | HTTP proxy dinleme adresi. Container içinde dışarı port açmak için genellikle `0.0.0.0:1455` kullanılır. |
| `FREEBUFF_API_BASE_URL` | `https://www.codebuff.com` | Freebuff/Codebuff API taban adresi. Login, logout ve session endpointleri bu adrese göre çağrılır. |
| `FREEBUFF_MODEL` | `deepseek/deepseek-v4-pro` | `/v1/models` çıktısında listelenen ve örnek isteklerde kullanılan model adı. |
| `FREEBUFF_PROXY_API_KEY` | boş | Boş değilse proxy, tüm endpointler için `Authorization: Bearer <değer>` veya `x-api-key: <değer>` kontrolü yapar. OpenAI uyumlu istemcilerde `api_key`, Anthropic/Claude Code tarafında `ANTHROPIC_AUTH_TOKEN` veya `ANTHROPIC_API_KEY` olarak kullanılabilir. |
| `FREEBUFF_CREDENTIALS_PATH` | `$HOME/.config/manicode/credentials.json` | Login sonrası yazılan ve serve sırasında okunan kimlik dosyası yolu. |

Örnek `.env` dosyası:

```dotenv
FREEBUFF_PROXY_ADDR=127.0.0.1:1455
FREEBUFF_API_BASE_URL=https://www.codebuff.com
FREEBUFF_MODEL=deepseek/deepseek-v4-pro
FREEBUFF_PROXY_API_KEY=local-proxy-key
FREEBUFF_CREDENTIALS_PATH=$HOME/.config/manicode/credentials.json
```

`.env` dosyasında gerçek production secret değerlerini paylaşmayın; CI veya container ortamında mümkünse secret manager ya da runtime env kullanın.

## Sıfırdan hızlı başlangıç

Bu akış temiz bir checkout'ta yerel proxy'yi ayağa kaldırır, Freebuff hesabıyla login yapar ve OpenAI/Anthropic uyumlu endpointleri doğrular.

1. Binary'yi derleyin:

   ```bash
   make build
   ```

2. Yerel ayarları export edin. Geliştirme sırasında aynı değerleri `.env` dosyasına da yazabilirsiniz:

   ```bash
   export FREEBUFF_PROXY_ADDR=127.0.0.1:1455
   export FREEBUFF_API_BASE_URL=https://www.codebuff.com
   export FREEBUFF_MODEL=deepseek/deepseek-v4-pro
   export FREEBUFF_PROXY_API_KEY=local-proxy-key
   export FREEBUFF_CREDENTIALS_PATH="$HOME/.config/manicode/credentials.json"
   ```

3. Freebuff/Codebuff hesabıyla giriş yapın:

   ```bash
   ./bin/freebuff-proxy login
   ```

   Komut bir giriş bağlantısı yazdırır. Bağlantıyı tarayıcıda açıp hesabınızla oturum açın; CLI doğrulama tamamlanınca `FREEBUFF_CREDENTIALS_PATH` konumuna credential yazar.

4. Proxy'yi çalıştırın:

   ```bash
   ./bin/freebuff-proxy serve
   ```

   Ayrı bir terminalde hızlı doğrulama çalıştırın:

   ```bash
   make smoke-openai
   make smoke-anthropic
   ```

5. İşiniz bitince çıkış yapın:

   ```bash
   ./bin/freebuff-proxy logout
   ```

   Logout upstream oturumu kapatır ve yerel credential dosyasını temizler. Sadece yerel proxy sürecini kapatmak istiyorsanız `Ctrl-C` yeterlidir.

## Login flow

1. CLI login başlatılır:

   ```bash
   freebuff-proxy login
   ```

2. Komut, Freebuff giriş bağlantısını terminale yazdırır.
3. Kullanıcı bağlantıyı tarayıcıda açıp hesabıyla oturum açar.
4. CLI, doğrulama durumu tamamlanana kadar status endpointini poll eder.
5. Başarılı girişte varsayılan olarak `$HOME/.config/manicode/credentials.json` dosyasına kimlik bilgisi yazılır.
6. Dosya atomik olarak oluşturulur ve `0600` izinleriyle korunur.

Özel credential yolu kullanmak için:

```bash
FREEBUFF_CREDENTIALS_PATH=/secure/path/credentials.json freebuff-proxy login
```

Çıkış yapmak için:

```bash
freebuff-proxy logout
```

Logout, kayıtlı metadata ile upstream logout çağrısı yapar ve başarılıysa yerel credential dosyasını temizler.

## Serve command

Yerel servis başlatma:

```bash
make build
./bin/freebuff-proxy serve
```

veya tek Makefile hedefiyle:

```bash
make run-bin
```

Geliştirme sırasında kaynak koddan doğrudan çalıştırmak için:

```bash
make run
```

Varsayılan adres `127.0.0.1:1455` olduğu için servis sadece yerel makineden erişilebilir. Farklı adres veya proxy API key ile çalıştırmak için:

```bash
FREEBUFF_PROXY_ADDR=0.0.0.0:1455 \
FREEBUFF_PROXY_API_KEY=local-proxy-key \
./bin/freebuff-proxy serve
```

Repo kökünde eski `./freebuff-proxy` binary'si varsa `./freebuff-proxy serve` güncel `bin/freebuff-proxy` yerine stale artifact çalıştırabilir. Bu durumda `upstream_chat_route_not_verified` gibi eski hata kodları görülebilir. Teşhis için:

```bash
make doctor
strings ./freebuff-proxy | grep -F upstream_chat_route_not_verified
strings ./bin/freebuff-proxy | grep -F upstream_chat_route_not_verified
```

Sağlık ve model endpointleri:

```bash
curl http://127.0.0.1:1455/healthz
curl -H 'Authorization: Bearer local-proxy-key' http://127.0.0.1:1455/v1/models
```

Chat endpointi OpenAI biçimli istek kabul eder, Freebuff oturumunu hazırlar, Codebuff CLI akışındaki gibi agent run başlatır ve chat isteğine `run_id`, `client_id`, `cost_mode` ile `freebuff_instance_id` metadata alanlarını ekleyerek upstream rotaya iletir. `deepseek-v4-pro` ve `deepseek-v4-flash` gibi kısa model alias'ları upstream isteğinde kanonik `deepseek/...` adına çevrilir:

```bash
curl -X POST http://127.0.0.1:1455/v1/chat/completions \
  -H 'Authorization: Bearer local-proxy-key' \
  -H 'Content-Type: application/json' \
  -d '{"model":"deepseek/deepseek-v4-pro","messages":[{"role":"user","content":"Merhaba"}]}'
```

Başarılı çağrı `object: "chat.completion"` ve `choices[0].message.content` alanında assistant yanıtı döndürür:

```json
{
  "object": "chat.completion",
  "model": "deepseek/deepseek-v4-pro",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "Merhaba!"
      },
      "finish_reason": "stop"
    }
  ]
}
```

Stream çağrısı için:

```bash
curl -N -X POST http://127.0.0.1:1455/v1/chat/completions \
  -H 'Authorization: Bearer local-proxy-key' \
  -H 'Content-Type: application/json' \
  -d '{"model":"deepseek/deepseek-v4-pro","stream":true,"messages":[{"role":"user","content":"Merhaba"}]}'
```

Stream çağrısı `Content-Type: text/event-stream` ile `data: {...}` chunk'ları ve en sonda `data: [DONE]` döndürür:

```text
data: {"object":"chat.completion.chunk","model":"deepseek/deepseek-v4-pro","choices":[{"index":0,"delta":{"role":"assistant","content":"Mer"}}]}

data: {"object":"chat.completion.chunk","model":"deepseek/deepseek-v4-pro","choices":[{"index":0,"delta":{"content":"haba!"}}]}

data: [DONE]
```

Makefile smoke hedefleri çalışan proxy'ye hızlı doğrulama isteği atar:

```bash
make smoke-openai
make smoke-anthropic

PROXY_URL=http://127.0.0.1:1455 \
PROXY_API_KEY=local-proxy-key \
SMOKE_MODEL=deepseek/deepseek-v4-pro \
make smoke-anthropic
```

## OpenAI-compatible client örneği

Python OpenAI istemcisi ile proxy adresini `base_url` olarak verin. `FREEBUFF_PROXY_API_KEY` ayarlıysa aynı değeri `api_key` olarak kullanın; ayarlı değilse istemcinin zorunlu alanını doldurmak için yerel, anlamsız bir değer verilebilir.

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://127.0.0.1:1455/v1",
    api_key="local-proxy-key",
)

response = client.chat.completions.create(
    model="deepseek/deepseek-v4-pro",
    messages=[{"role": "user", "content": "Merhaba"}],
)

print(response.choices[0].message.content)
```

Bu çağrı, yapılandırılmış Freebuff oturumu aktifse OpenAI uyumlu başarı yanıtı döndürür; upstream veya oturum hataları OpenAI uyumlu hata zarfına dönüştürülür.

## Codex ve OpenAI-compatible kullanım

OpenAI Chat Completions uyumlu istemciler için `base_url` değerini `http://127.0.0.1:1455/v1`, API key değerini de `FREEBUFF_PROXY_API_KEY` olarak verin.

```bash
export OPENAI_BASE_URL=http://127.0.0.1:1455/v1
export OPENAI_API_KEY=local-proxy-key
```

Sonra Chat Completions kullanan istemciler `model=deepseek/deepseek-v4-pro` ile proxy üzerinden çalışabilir.

OpenAI Codex CLI için durum farklıdır: güncel Codex CLI akışları OpenAI Responses API (`/v1/responses`) kullanabilir. Bu proxy şu anda `/v1/responses` endpointi sunmadığı için Codex CLI'yi doğrudan tam uyumlu hedef olarak belgelemiyoruz. Codex tarafında Chat Completions uyumlu veya custom provider destekleyen bir sürüm/konfigürasyon kullanıyorsanız yukarıdaki `OPENAI_BASE_URL` ve `OPENAI_API_KEY` değerleriyle deneyebilirsiniz; Responses API isteyen sürümler için ayrıca `/v1/responses` desteği eklenmelidir.

## Anthropic ve Claude Code örneği

Anthropic Messages uyumlu yüzeyler:

- `POST /v1/messages`
- `POST /v1/messages/count_tokens`
- `GET /v1/models`

`/v1/messages`, string veya `type: "text"` content block içeren Anthropic mesajlarını OpenAI uyumlu chat isteğine çevirir. Yanıt non-stream çağrıda Anthropic `message` nesnesi, stream çağrıda `message_start`, `content_block_delta`, `message_delta` ve `message_stop` SSE eventleri olarak döner. Tool tanımları OpenAI function tool biçimine best-effort çevrilir; mevcut upstream chat servisi metin yanıtı döndürdüğü için Anthropic `tool_use` response block üretimi bu sürümde yoktur.

Basit Anthropic curl:

```bash
curl -X POST http://127.0.0.1:1455/v1/messages \
  -H 'x-api-key: local-proxy-key' \
  -H 'anthropic-version: 2023-06-01' \
  -H 'Content-Type: application/json' \
  -d '{"model":"deepseek/deepseek-v4-pro","max_tokens":256,"messages":[{"role":"user","content":"Merhaba"}]}'
```

Stream örneği:

```bash
curl -N -X POST http://127.0.0.1:1455/v1/messages \
  -H 'x-api-key: local-proxy-key' \
  -H 'anthropic-version: 2023-06-01' \
  -H 'Content-Type: application/json' \
  -d '{"model":"deepseek/deepseek-v4-pro","max_tokens":256,"stream":true,"messages":[{"role":"user","content":"Merhaba"}]}'
```

Claude Code için yerel gateway ayarı:

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:1455
export ANTHROPIC_AUTH_TOKEN=local-proxy-key
export ANTHROPIC_MODEL=deepseek/deepseek-v4-pro
export ANTHROPIC_SMALL_FAST_MODEL=deepseek/deepseek-v4-flash
export CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1
```

Ardından Claude Code'u aynı terminal oturumunda başlatın:

```bash
claude
```

Proxy API key boş bırakıldıysa `ANTHROPIC_AUTH_TOKEN` gerekli değildir; yine de Claude Code tarafında boş olmayan yerel bir değer vermek istemci davranışını daha öngörülebilir kılar.

## Release binary indirme

Kaynak kod `ferdiunal/freebuff-go` deposunda tutulur. `v*` tag'i push edildiğinde bu kaynak depodaki GitHub Actions workflow'u binary arşivlerini üretir ve `ferdiunal/freebuff-proxy` GitHub Releases altında yayınlar.

Örnek indirme:

```bash
gh release download v0.1.0 \
  --repo ferdiunal/freebuff-proxy \
  --pattern "*darwin-arm64*" \
  --dir dist

tar -xzf dist/freebuff-proxy-darwin-arm64.tar.gz -C dist
./dist/freebuff-proxy-darwin-arm64 serve
```

Release asset'leri yalnızca derlenmiş `freebuff-proxy` binary'sini içerir; credential dosyaları release paketlerine dahil edilmez.

## Docker kullanımı

İmaj oluşturma:

```bash
docker build -t freebuff-proxy:local .
```

veya:

```bash
make docker
```

Container çalıştırma örneği:

```bash
docker run --rm \
  -p 1455:1455 \
  -e FREEBUFF_PROXY_ADDR=0.0.0.0:1455 \
  -e FREEBUFF_PROXY_API_KEY=local-proxy-key \
  -v "$HOME/.config/manicode:/home/freebuff/.config/manicode:ro" \
  freebuff-proxy:local serve
```

Credential dosyasını container içinde üretmek isterseniz ilgili volume yazılabilir olmalıdır. Üretim kullanımında credential dosyasını salt okunur mount etmek tercih edilir.

## Release otomasyonu

`freebuff-go` deposunda `FREEBUFF_PROXY_RELEASE_TOKEN` adlı repository secret tanımlı olmalıdır. Bu token yalnızca `ferdiunal/freebuff-proxy` deposunda release oluşturma ve asset yükleme yetkisi için kullanılmalıdır.

Yeni sürüm yayınlamak için kaynak repoda tag push edilir:

```bash
git tag v0.1.0
git push origin v0.1.0
```

Workflow ilgili `v*` tag'ini `ferdiunal/freebuff-go` deposundan checkout eder, test/vet çalıştırır ve binary arşivlerini dağıtım reposundaki aynı tag release'ine yükler.

## Güvenlik notları

- `credentials.json` gizli tutulmalıdır; bu dosya Freebuff oturum token'ı değerini içerir.
- `credentials.json` repo kökünde tutulmamalı ve git'e commit edilmemelidir; varsayılan güvenli yol `$HOME/.config/manicode/credentials.json` değeridir.
- `credentials.json` yanlışlıkla public repository'ye pushlandıysa token rotate/revoke edilmeli ve dosya git history'den de temizlenmelidir.
- Credential dosyası `0600` izinleriyle saklanmalıdır. Uygulama kendi yazdığı dosyada bu izni uygular; dışarıdan mount edilen dosyalarda izni ayrıca kontrol edin.
- oturum token'ı terminale, loglara, hata raporlarına veya CI çıktılarına yazdırılmamalıdır.
- GitHub Actions release workflow'u Freebuff credential dosyası veya upstream token gerektirmez; CI'da yalnız `FREEBUFF_PROXY_RELEASE_TOKEN` dağıtım reposuna release yazmak için kullanılmalıdır.
- Dış ağa açılan kurulumlarda `FREEBUFF_PROXY_API_KEY` kullanın ve istemcilerden `Authorization: Bearer <proxy-api-key>` göndermesini isteyin.
- Proxy API key, upstream Freebuff token yerine geçmez; yalnızca yerel proxy yüzeyini korur.
- Container çalıştırırken credential volume'unu mümkünse salt okunur bağlayın ve sadece gerekli kullanıcıya erişim verin.
