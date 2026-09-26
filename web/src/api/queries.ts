export const keys = {
  accounts: ["accounts"] as const,
  conversations: (accountID: string) => ["conversations", accountID] as const,
  conversation: (id: string) => ["conversation", id] as const,
  contacts: (accountID: string, search: string) =>
    ["contacts", accountID, search] as const,
  messages: (id: string) => ["messages", id] as const,
  attachment: (id: string) => ["attachment", id] as const,
};
