# ConvoMeow

ConvoMeow runs WhatsApp accounts on one server through [whatsmeow](https://github.com/tulir/whatsmeow). Its CLI uses a local HTTP API to manage accounts, send text, and read saved messages.

## Build and run

You need Go 1.27 or newer and a C compiler for SQLite (`CGO_ENABLED=1`). Pairing requires a WhatsApp number.

```sh
go build -o convomeow ./cmd/convomeow
./convomeow serve
```

In another terminal:

```sh
./convomeow account add sales
./convomeow account login sales
./convomeow account list
./convomeow message send sales +15551234567 'Hello'
./convomeow chat list sales
./convomeow chat messages sales '<conversation-id>'
./convomeow message list sales
```

The daemon listens on `127.0.0.1:8787` and writes to `./data`. Put `--data-dir DIR` before the command to change its data directory. You can set these variables:

| Variable | Purpose |
| --- | --- |
| `CONVOMEOW_DATA_DIR` | Data directory for the daemon and CLI |
| `CONVOMEOW_LISTEN` | Daemon listen address |
| `CONVOMEOW_URL` | Daemon URL used by the CLI |

The CLI displays a QR code during pairing. Scan it from WhatsApp's **Linked devices** screen. The daemon loads paired sessions from SQLite when it starts. Run one daemon per data directory; the process lock prevents a second daemon from opening the same files.

## Native API

The CLI uses ConvoMeow's native API. The [OpenAPI file](docs/api/openapi-v1.yaml) defines the current routes and payloads.

On first start, the daemon creates `data/control.token`. Send its value as `Authorization: Bearer <token>` on every `/api/v1` request. The token grants access to all accounts. Keep the API on loopback, or place it behind HTTPS and access controls if you change the listen address.

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/healthz`, `/api/versions` | Process health and API versions; no token required |
| `POST`, `GET` | `/api/v1/accounts` | Create and list accounts |
| `GET` | `/api/v1/accounts/{id}` | Account connection state |
| `POST` | `/api/v1/accounts/{id}/login-attempts` | Start pairing |
| `GET` | `/api/v1/accounts/{id}/login-attempts/{attempt_id}` | Read the QR challenge or pairing state |
| `POST` | `/api/v1/accounts/{id}/conversations` | Find or create a conversation for a phone number |
| `GET` | `/api/v1/conversations`, `/api/v1/conversations/{id}` | List and read conversations |
| `GET`, `POST` | `/api/v1/conversations/{id}/messages` | Read a thread or send text |
| `GET` | `/api/v1/messages`, `/api/v1/messages/{id}` | Read an account's messages or one message |

Create an account with `{"label":"sales","provider":"whatsapp","connection_kind":"linked_device"}`. To message a new number, create a conversation with `{"target":{"type":"phone_number","value":"+15551234567"}}`. Send to its ID with `{"kind":"text","content":{"text":"Hello"}}` and an `Idempotency-Key` header. Reuse the same key if you need to retry that request.

List responses contain `items` and, when more records exist, `next_cursor`. Pass that value as `?cursor=...` to load older records. Use `?account_id=...` to limit the conversation list to one account; the account message list requires it. The CLI accepts account labels and displays conversation IDs.

ConvoMeow assigns conversation and message IDs. WhatsApp chat IDs appear as read-only `provider_chat_id` values; API paths use ConvoMeow IDs.

The API saves an outgoing message before asking WhatsApp to send it. A failed request may leave its state as `outcome_unknown`; check that message before sending again. The CLI prints a retry key when a send request fails.

## Data and limits

`data/app.sqlite` holds accounts and saved messages. `data/whatsmeow.sqlite` holds WhatsApp sessions. ConvoMeow creates the data directory and control token with owner-only permissions.

The connector saves messages it receives after startup. For images, videos, audio, documents, stickers, locations, and contacts, it stores the type and any caption. It does not download attachments or import older chats.

Pairing, messaging, and reconnection still need testing with a real WhatsApp number.

The connector uses an unofficial WhatsApp client. Review [WhatsApp's terms](https://www.whatsapp.com/legal/terms-of-service) before using an account.

## Code layout

- `cmd/convomeow`: CLI entry point and daemon lifecycle
- `internal/core`: account and message types, plus the connector contract
- `internal/app`: account sessions, pairing, and message handling
- `internal/providers/whatsapp`: whatsmeow adapter
- `internal/store/sqlite`: application database and schema
- `internal/api/native/v1`: authenticated HTTP API and v1 response types
- `internal/cli`: API client and QR display

## Development

AI tools assist with code and documentation.

Use `make fmt` to format Go files and `make fmt-check` to check them. Run `go build ./...` and `go vet ./...` before contributing a change.

ConvoMeow's code is licensed under [Apache-2.0](LICENSE). Whatsmeow remains a separate MPL-2.0 dependency.
