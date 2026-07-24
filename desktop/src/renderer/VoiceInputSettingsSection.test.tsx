import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type {
  VoiceInputSettings,
  WuuDesktopApi,
} from "../shared/protocol";
import { VoiceInputSettingsSection } from "./VoiceInputSettingsSection";

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => root.unmount());
  delete (window as unknown as { wuu?: WuuDesktopApi }).wuu;
  container.remove();
});

describe("VoiceInputSettingsSection", () => {
  it("separates free ASR from BYOK polish and persists defaults", async () => {
    const initial: VoiceInputSettings = {
      polish_enabled: false,
      language: "system",
    };
    const updateVoiceInputSettings = vi
      .fn()
      .mockImplementation(async (settings: VoiceInputSettings) => settings);
    const openVoicePrivacySettings = vi.fn().mockResolvedValue({ ok: true });
    window.wuu = {
      platform: "darwin",
      initialVoiceInputSettings: initial,
      getVoiceInputSettings: vi.fn().mockResolvedValue({
        settings: initial,
        microphone_permission: "denied",
        speech_permission: "not_determined",
      }),
      updateVoiceInputSettings,
      onVoiceInputSettingsChange: vi.fn(() => () => undefined),
      openVoicePrivacySettings,
    } as unknown as WuuDesktopApi;

    await act(async () => {
      root.render(<VoiceInputSettingsSection polishAvailable />);
    });

    expect(container.textContent).toContain("设备端语音识别免费");
    expect(container.textContent).toContain("BYOK 仅用于可选的文字润色");
    expect(container.textContent).toContain("已拒绝");
    expect(container.textContent).toContain("尚未请求");

    await act(async () => {
      container
        .querySelector<HTMLButtonElement>('[data-testid="settings-voice-polish"]')
        ?.click();
    });
    expect(updateVoiceInputSettings).toHaveBeenCalledWith({
      polish_enabled: true,
      language: "system",
    });

    await act(async () => {
      container
        .querySelector<HTMLButtonElement>(
          '[data-testid="settings-voice-language-en-US"]',
        )
        ?.click();
    });
    expect(updateVoiceInputSettings).toHaveBeenLastCalledWith({
      polish_enabled: true,
      language: "en-US",
    });

    await act(async () => {
      Array.from(
        container.querySelectorAll<HTMLButtonElement>(".settings-button"),
      )
        .find((button) => button.textContent?.includes("系统设置"))
        ?.click();
    });
    expect(openVoicePrivacySettings).toHaveBeenCalledWith("microphone");
  });

  it("shows the macOS-only state on other platforms", () => {
    window.wuu = {
      platform: "win32",
      initialVoiceInputSettings: {
        polish_enabled: false,
        language: "system",
      },
    } as unknown as WuuDesktopApi;

    act(() => {
      root.render(<VoiceInputSettingsSection polishAvailable />);
    });

    expect(container.textContent).toContain("仅 macOS");
    expect(
      container.querySelector('[data-testid="settings-voice-polish"]'),
    ).toBeNull();
  });
});
