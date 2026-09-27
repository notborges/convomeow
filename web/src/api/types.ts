export interface Page<T> {
  items: T[];
  next_cursor?: string;
  previous_cursor?: string;
}

export interface Account {
  avatar_url?: string;
  id: string;
  label: string;
  provider: string;
  provider_identity?: string;
  state: string;
  last_error?: string;
}

export interface Contact {
  provider_id: string;
  name: string;
  phone?: string;
  masked_phone?: string;
  avatar_url: string;
}

export interface Attachment {
  duration_seconds?: number;
  width?: number;
  height?: number;
  attempt_count?: number;
  id: string;
  kind: string;
  mime_type?: string;
  file_name?: string;
  size?: number;
  availability: string;
}

export interface Reply {
  message_id?: string;
  sender_id?: string;
  kind: string;
  text?: string;
}

export interface Message {
  reply?: Reply;
  id: string;
  account_id: string;
  conversation_id: string;
  direction: "inbound" | "outbound";
  state: string;
  sender_id?: string;
  kind: string;
  content: { text?: string; caption?: string };
  attachments?: Attachment[];
  occurred_at: string;
}

export interface Conversation {
  id: string;
  account_id: string;
  kind: string;
  display_name: string;
  description?: string;
  avatar_url: string;
  contact?: Contact;
  last_message?: Message;
  updated_at: string;
}

export interface LoginAttempt {
  id: string;
  state: string;
  challenge?: { type: string; value: string; expires_at: string };
  error?: string;
}

export interface Problem {
  detail?: string;
  title?: string;
  code?: string;
}
