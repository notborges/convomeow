import { ApiError } from "../api/client";
import type en from "./locales/en.json";

export type ErrorKey = keyof typeof en.errors;
const codes: Record<string, ErrorKey> = {
  unsupported_operation: "unsupported",
  account_not_connected: "connection",
  media_quota: "quota",
  media_busy: "busy",
  media_storage_unavailable: "storage",
  unsupported_media_type: "unsupported",
  media_too_large: "tooLarge",
  request_too_large: "tooLarge",
};
export function errorKey(error: unknown): ErrorKey {
  if (!(error instanceof ApiError))
    return error instanceof TypeError ? "network" : "request";
  if (error.code && Object.hasOwn(codes, error.code)) return codes[error.code];
  switch (error.status) {
    case 400:
    case 422:
      return "invalid";
    case 401:
      return "unauthorized";
    case 403:
      return "forbidden";
    case 404:
    case 410:
      return "notFound";
    case 409:
      return "conflict";
    case 413:
      return "tooLarge";
    case 415:
      return "unsupported";
    case 429:
      return "rateLimit";
    default:
      return error.status >= 500 ? "unavailable" : "request";
  }
}
