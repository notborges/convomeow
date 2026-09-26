# Live updates

Connect to `/api/v1/events` with WebSocket. Browser clients use the existing session cookie and must connect from the same origin. API clients can send the normal bearer authorization header. Credentials do not belong in the URL.

The server subscribes the connection before sending:

```json
{"type":"ready"}
```

On every `ready` frame, refresh active HTTP queries and mark inactive cached data stale. This covers changes missed while disconnected. Notifications are not persisted or replayed.

## Notifications

| Type | Fields | Refresh |
| --- | --- | --- |
| `accounts.changed` | `account_id` | Account state, login attempts and account contacts |
| `conversations.changed` | `account_id`, optional `conversation_id` | Conversation list and affected conversation/messages |
| `attachment.changed` | `account_id`, `attachment_id` | Attachment metadata |
| `contacts.changed` | `account_id` | Contacts and conversation names |
| `avatars.changed` | `account_id` | Account, contact and conversation pictures |

An omitted `conversation_id` means a change may affect any conversation in the account, such as a history import or alias merge. Refresh loaded queries for that account; fetching unseen history is unnecessary.

Notifications follow saved resource changes or completed account/pairing state changes. They contain ConvoMeow IDs, not provider payloads, message contents, QR values or media bytes. For example:

```json
{"type":"attachment.changed","account_id":"account-id","attachment_id":"attachment-id"}
```

Coalesce notifications before fetching. If a notification arrives during an older HTTP request, fetch again after that request finishes. Use the HTTP responses for message ordering and pagination.

## Connection lifecycle

The connection only sends notifications; commands and file transfers use HTTP. Client application messages close the connection. The server sends a ping every 20 seconds, with a five-second response deadline. Browser authorization is rechecked before each notification and heartbeat.

Each connection has a bounded queue. Queue overflow closes the connection with code `1013`; reconnect and refresh after `ready`. Service shutdown closes the stream. Reconnect with bounded backoff and jitter after network failures. Stop reconnecting when the browser session is no longer valid, and close the stream on sign-out.
