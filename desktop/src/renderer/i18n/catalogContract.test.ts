import { describe, expect, it } from "vitest";
import { APP_LOCALES, type AppLocale } from "../../shared/protocol";
import { assertCatalogContract } from "./catalogContract";
import { enUS } from "./resources/en-US";
import { zhCN, type TranslationKey } from "./resources/zh-CN";

const catalogs = {
  "zh-CN": zhCN,
  "en-US": enUS,
} satisfies Record<AppLocale, Record<TranslationKey, string>>;

describe("renderer translation catalogs", () => {
  it("registers every supported app locale", () => {
    expect(Object.keys(catalogs)).toEqual([...APP_LOCALES]);
  });

  it("keeps keys, order, values, and placeholders aligned", () => {
    expect(() => assertCatalogContract(catalogs)).not.toThrow();
  });
});
