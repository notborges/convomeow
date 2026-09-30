import type {
  Account,
  Attachment,
  Contact,
  Conversation,
  LoginAttempt,
  Message,
  MessageReaction,
  MessageReceipt,
  MessageRevision,
  Page,
  Problem,
} from "./types";

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly code?: string,
  ) {
    super(message);
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, { credentials: "same-origin", ...init });
  if (!response.ok) {
    const problem = (await response.json().catch(() => ({}))) as Problem;
    if (response.status === 401 && path.startsWith("/api/")) {
      window.dispatchEvent(new Event("convomeow:unauthorized"));
    }
    throw new ApiError(
      problem.detail || problem.title || `Request failed (${response.status})`,
      response.status,
      problem.code,
    );
  }
  if (response.status === 204) return undefined as T;
  return response.json() as Promise<T>;
}

function json(body: unknown): RequestInit {
  return {
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  };
}

function pageURL(
  path: string,
  params: Record<string, string | undefined>,
): string {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value) query.set(key, value);
  }
  return `${path}?${query}`;
}

export const api = {
  notificationConfig: (signal?: AbortSignal) =>
    request<{ enabled: boolean; public_key?: string }>(
      "/api/v1/notifications/config",
      { signal },
    ),
  registerNotifications: (
    subscription: PushSubscription,
    locale: string,
    preview: boolean,
  ) =>
    request<{ id: string }>("/api/v1/notifications/subscriptions", {
      method: "POST",
      signal: AbortSignal.timeout(10000),
      ...json({
        endpoint: subscription.endpoint,
        keys: subscription.toJSON().keys,
        locale,
        preview,
      }),
    }),
  deleteNotifications: (id: string) =>
    request<void>(
      `/api/v1/notifications/subscriptions/${encodeURIComponent(id)}`,
      { method: "DELETE", signal: AbortSignal.timeout(10000) },
    ),
  presence: (
    conversationID: string,
    activity: import("./presence").ChatActivity,
    clientID: string,
  ) =>
    request<void>(`/api/v1/conversations/${conversationID}/presence`, {
      method: "POST",
      ...json({ activity, client_id: clientID }),
      signal: AbortSignal.timeout(5000),
      keepalive: activity === "paused",
    }),
  session: (signal?: AbortSignal) =>
    request<{ authenticated: boolean }>("/app/session", { signal }),
  login: (token: string) =>
    request<void>("/app/session", { method: "POST", ...json({ token }) }),
  logout: () => request<void>("/app/session", { method: "DELETE" }),
  accounts: () => request<Page<Account>>("/api/v1/accounts"),
  createAccount: (label: string) =>
    request<Account>("/api/v1/accounts", {
      method: "POST",
      ...json({
        label,
        provider: "whatsapp",
        connection_kind: "linked_device",
      }),
    }),
  startLogin: (accountID: string) =>
    request<LoginAttempt>(
      `/api/v1/accounts/${encodeURIComponent(accountID)}/login-attempts`,
      { method: "POST" },
    ),
  loginAttempt: (accountID: string, attemptID: string) =>
    request<LoginAttempt>(
      `/api/v1/accounts/${encodeURIComponent(accountID)}/login-attempts/${encodeURIComponent(attemptID)}`,
    ),
  conversations: (accountID: string, cursor?: string) =>
    request<Page<Conversation>>(
      pageURL("/api/v1/conversations", {
        account_id: accountID,
        cursor,
        limit: "50",
      }),
    ),
  conversation: (id: string) =>
    request<Conversation>(`/api/v1/conversations/${encodeURIComponent(id)}`),
  contacts: (accountID: string, q: string, cursor?: string) =>
    request<Page<Contact>>(
      pageURL(`/api/v1/accounts/${encodeURIComponent(accountID)}/contacts`, {
        q,
        cursor,
        limit: "50",
      }),
    ),
  createConversation: (
    accountID: string,
    type: "contact" | "phone_number",
    value: string,
  ) =>
    request<Conversation>(
      `/api/v1/accounts/${encodeURIComponent(accountID)}/conversations`,
      {
        method: "POST",
        ...json({ target: { type, value } }),
      },
    ),
  messages: (
    conversationID: string,
    cursor?: string,
    position?: { around?: string; after?: string },
    signal?: AbortSignal,
  ) =>
    request<Page<Message>>(
      pageURL(
        `/api/v1/conversations/${encodeURIComponent(conversationID)}/messages`,
        {
          cursor,
          limit: "50",
          around_message_id: position?.around,
          after_cursor: position?.after,
        },
      ),
      { signal },
    ),
  reactions: (id: string, cursor?: string) =>
    request<Page<MessageReaction>>(
      pageURL(`/api/v1/messages/${encodeURIComponent(id)}/reactions`, {
        cursor,
      }),
    ),
  messageRevisions: (id: string, cursor?: string) =>
    request<Page<MessageRevision>>(
      pageURL(`/api/v1/messages/${encodeURIComponent(id)}/revisions`, {
        cursor,
      }),
    ),
  editMessage: (id: string, text: string) =>
    request<Message>(`/api/v1/messages/${encodeURIComponent(id)}`, {
      method: "PATCH",
      ...json({ text }),
    }),
  revokeMessage: (id: string) =>
    request<Message>(`/api/v1/messages/${encodeURIComponent(id)}/revoke`, {
      method: "POST",
    }),
  setReaction: (id: string, emoji: string) =>
    request<Message>(
      `/api/v1/messages/${encodeURIComponent(id)}/reaction`,
      emoji ? { method: "PUT", ...json({ emoji }) } : { method: "DELETE" },
    ),
  receipts: (id: string, cursor?: string) =>
    request<Page<MessageReceipt>>(
      pageURL(`/api/v1/messages/${encodeURIComponent(id)}/receipts`, {
        cursor,
        limit: "50",
      }),
    ),
  message: (id: string) =>
    request<Message>(`/api/v1/messages/${encodeURIComponent(id)}`),
  sendText: (
    conversationID: string,
    text: string,
    key: string,
    replyID?: string,
  ) =>
    request<Message>(
      `/api/v1/conversations/${encodeURIComponent(conversationID)}/messages`,
      {
        method: "POST",
        ...json({
          kind: "text",
          content: { text },
          reply_to_message_id: replyID,
        }),
        headers: { "Content-Type": "application/json", "Idempotency-Key": key },
      },
    ),
  upload: async (accountID: string, file: File): Promise<{ id: string }> => {
    const body = new FormData();
    body.append("file", file);
    return request(
      `/api/v1/accounts/${encodeURIComponent(accountID)}/uploads`,
      { method: "POST", body },
    );
  },
  sendMedia: (
    conversationID: string,
    kind: string,
    uploadID: string,
    caption: string,
    key: string,
    replyID?: string,
  ) =>
    request<Message>(
      `/api/v1/conversations/${encodeURIComponent(conversationID)}/messages`,
      {
        method: "POST",
        ...json({
          kind,
          content: { upload_id: uploadID, caption },
          reply_to_message_id: replyID,
        }),
        headers: { "Content-Type": "application/json", "Idempotency-Key": key },
      },
    ),
  attachment: (id: string, signal?: AbortSignal) =>
    request<Attachment>(`/api/v1/attachments/${encodeURIComponent(id)}`, {
      signal,
    }),
  requestAttachment: (id: string, signal?: AbortSignal) =>
    fetch(`/api/v1/attachments/${encodeURIComponent(id)}/content`, {
      credentials: "same-origin",
      signal,
    }).then(async (response) => {
      if (
        response.status !== 200 &&
        response.status !== 202 &&
        response.status !== 206
      ) {
        const problem = (await response.json().catch(() => ({}))) as Problem;
        if (response.status === 401)
          window.dispatchEvent(new Event("convomeow:unauthorized"));
        throw new ApiError(
          problem.detail ||
            problem.title ||
            `Attachment request failed (${response.status})`,
          response.status,
          problem.code,
        );
      }
      return response.body?.cancel();
    }),
};
