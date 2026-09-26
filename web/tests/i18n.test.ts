import { describe, expect, test } from "bun:test";
import { ApiError } from "../src/api/client";
import { errorKey } from "../src/i18n/errors";
import en from "../src/i18n/locales/en.json";
import ptBR from "../src/i18n/locales/pt-BR.json";

function entries(value: object, prefix = ""): Record<string, string> {
  return Object.fromEntries(
    Object.entries(value).flatMap(([key, entry]) => {
      const path = prefix ? `${prefix}.${key}` : key;
      return typeof entry === "string"
        ? [[path, entry]]
        : Object.entries(entries(entry, path));
    }),
  );
}

describe("translation contract", () => {
  test("Portuguese includes every English key and preserves interpolation and rich-text slots", () => {
    const english = entries(en);
    const portuguese = entries(ptBR);
    expect(Object.keys(portuguese).sort()).toEqual(Object.keys(english).sort());
    for (const [key, value] of Object.entries(english)) {
      expect(portuguese[key].trim().length, key).toBeGreaterThan(0);
      const slots = (text: string) =>
        [...text.matchAll(/{{\s*([^}]+)\s*}}|<\/?([a-z]+)\s*\/?\s*>/g)]
          .map((match) => match[0])
          .sort();
      expect(slots(portuguese[key]), key).toEqual(slots(value));
    }
  });
  test("provider error codes retain actionable meanings instead of generic HTTP errors", () => {
    expect(
      errorKey(new ApiError("provider detail", 409, "account_not_connected")),
    ).toBe("connection");
    expect(errorKey(new ApiError("provider detail", 507, "media_quota"))).toBe(
      "quota",
    );
    expect(errorKey(new ApiError("unknown detail", 503, "unknown_code"))).toBe(
      "unavailable",
    );
    expect(errorKey(new TypeError("Failed to fetch"))).toBe("network");
  });
});
