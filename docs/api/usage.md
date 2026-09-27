# API guide

The web client and CLI use ConvoMeow's native API. The [OpenAPI file](openapi-v1.yaml) defines the current routes and payloads.

On first start, the daemon creates `data/control.token`. Send its value as `Authorization: Bearer <token>` on every `/api/v1` request. The token grants access to all accounts. Keep the API on loopback, or place it behind HTTPS and access controls if you change the listen address.

## Accounts and messages

Create an account with `{"label":"sales","provider":"whatsapp","connection_kind":"linked_device"}`. To message a new number, create a conversation with `{"target":{"type":"phone_number","value":"+15551234567"}}`. To start from a synced contact, use `{"target":{"type":"contact","value":"<provider_id>"}}`. Send to the conversation ID with `{"kind":"text","content":{"text":"Hello"}}` and an `Idempotency-Key` header. Reuse the same key if you need to retry that request.

## Sending media

To send a file, post one multipart `file` part to the account's uploads route. The response contains an upload ID. Send `{"kind":"image","content":{"upload_id":"<id>","caption":"Photo"}}` to the conversation's messages route with an `Idempotency-Key` header. Images accept JPEG, PNG, or WebP; videos accept MP4 or 3GPP; audio accepts MP3, MP4, Ogg, AAC, or AMR. Documents accept any valid MIME type. Stickers must be static WebP files. Audio and stickers do not accept captions. Each upload belongs to one account, can be sent once, and expires after 24 hours if unused. You can delete an unused upload with its DELETE route.

Media sends return `202 Accepted` and a queued message. Poll the `Location` URL until its state is `sent`, `failed`, or `outcome_unknown`. Reuse both the upload ID and idempotency key if a send request fails. The CLI prints those values for a retry with `message send-file --key KEY --upload-id ID ACCOUNT PHONE_OR_CONVERSATION_ID KIND`.

## Pagination and IDs

List responses contain `items` and, when more records exist, `next_cursor`. Pass that value as `?cursor=...` to load older records. Messages use provider timestamps and, when available, provider ordering to break timestamp ties. Messages without ordering metadata use a saved local sequence. Use `?account_id=...` to limit the conversation list to one account; the account message list requires it. The CLI accepts account labels and displays conversation IDs.

ConvoMeow assigns conversation and message IDs. WhatsApp chat IDs appear as read-only `provider_chat_id` values; API paths use ConvoMeow IDs.

## Contacts and photos

The contacts route lists synced contacts, including people with no chat, and accepts `q`, `limit`, and `cursor`. Saved names take priority over business and push names; phone numbers may be unavailable. Conversations include a display name and an avatar URL. After an account connects, ConvoMeow fetches contact and group photo previews in the background and stores them in the active media profile. Cached photos remain available while the account is disconnected. ConvoMeow checks saved photos again after 24 hours and missing photos after six hours. It also refreshes photos when WhatsApp reports a change. If WhatsApp hides or removes a photo, the next check removes the saved copy and the avatar route returns `404`. A photo may stay cached while the account is offline. Logging out clears the account's photo cache.

## Send failures and retries

The API saves an outgoing message before asking WhatsApp to send it. A failed request may leave its state as `outcome_unknown`; check that message before sending again. The CLI prints a retry key when a text send request fails. Media uploads can retry before ConvoMeow calls WhatsApp's send operation. A send error after that point does not trigger an automatic resend.


## Replies and receipts

Add `reply_to_message_id` to a text or media send request to quote a saved message from the same conversation. Returned messages include a `reply` preview when available. Use `around_message_id` on the conversation's messages route to load the original message and its neighbors.

Outgoing messages include a `delivery` summary, separate from their send-job `state`. Read per-recipient timestamps through `GET /api/v1/messages/{id}/receipts`. Group counts describe the receipts received so far; they do not establish that everyone has read a message. Missing receipts mean the status is unknown.

To send read receipts, post `{"message_ids":["<message-id>"]}` to `/api/v1/conversations/{id}/read-receipts`. Provide up to 100 incoming message IDs from that conversation. Inspect both `read_message_ids` and `failed` in the response, including on HTTP 200. Opening a chat or fetching messages does not send this command.

## Live updates

Connect to `/api/v1/events` for WebSocket notifications and fetch changed resources through HTTP. Chat presence uses temporary events; opt in with `presence_account_id` to mark the selected account online while viewing it. See the [event protocol](realtime.md) for authentication, cache invalidation, and reconnect behavior.

## History and downloads

ConvoMeow saves live messages and any chat history WhatsApp supplies after pairing or reconnecting. To request earlier messages, post `{"before_message_id":"<saved-message-id>","count":50}` to `/api/v1/conversations/{id}/history-requests`. The server merges returned history into saved messages. A `202` response confirms the request was sent, not that the phone returned history. It attempts to download new images, videos, audio, documents, and stickers in the background. The web client loads images and stickers from imported history as they enter view. Other imported files download on request. A queued file returns `202 Accepted` with `Retry-After`; poll the same URL until it returns the file. The content route supports one `Range: bytes=...` request. `HEAD` checks a stored file without starting a download. Location and shared contact-card messages include only their type.

## Reactions

Use `PUT /api/v1/messages/{id}/reaction` with `{"emoji":"👍"}` to set or replace the connected account's reaction. Use `DELETE` on the same route to remove it. Both return the updated message after provider acknowledgment. The target must be a confirmed saved message in a direct or group conversation.

Message responses include `reactions` summaries with `emoji`, `count`, and `own`. `GET /api/v1/messages/{id}/reactions` lists participants with cursor pagination; `self` identifies the connected account across its devices. These reads do not send reactions or mark messages read.

Commands set the requested state and do not toggle it. There is no automatic retry or durable reaction command queue. A `502 reaction_unconfirmed` response can follow a provider timeout or a local recording failure after acknowledgment. Refresh before retrying; a new request sets the desired state again.

Live events and imported history update the same stored reaction state. Removal timestamps prevent older history from restoring removed reactions. `conversations.changed` notifications invalidate message and participant queries. Existing local messages acquire old reactions only when the provider supplies those reactions again; adding this feature does not request a resync.
