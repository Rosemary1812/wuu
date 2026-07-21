import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { WuuDesktopApi } from "../shared/protocol";
import { I18nProvider } from "./i18n";
import { ThemePreferenceControl } from "./ThemePreferenceSection";

describe("ThemePreferenceControl", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    window.wuu = {
      initialLanguagePreference: "zh-CN",
      initialSystemLocale: "zh-CN",
      initialThemePreference: "system",
    } as unknown as WuuDesktopApi;
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
  });

  it("uses aligned light and dark layers for the system preview", async () => {
    await act(async () => {
      root.render(
        <I18nProvider>
          <ThemePreferenceControl />
        </I18nProvider>,
      );
    });

    const systemPreview = container.querySelector('[data-testid="settings-theme-system"] .settings-theme-preview');
    const lightPreview = container.querySelector('[data-testid="settings-theme-light"] .settings-theme-preview');

    expect(systemPreview?.querySelectorAll(".settings-theme-preview-window")).toHaveLength(2);
    expect(systemPreview?.querySelector(".settings-theme-preview-window-light")).not.toBeNull();
    expect(systemPreview?.querySelector(".settings-theme-preview-window-dark")).not.toBeNull();
    expect(lightPreview?.querySelectorAll(".settings-theme-preview-window")).toHaveLength(1);
  });
});
