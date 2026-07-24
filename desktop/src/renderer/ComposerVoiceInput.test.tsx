import { act, useState } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type {
  SpeechRecognitionEvent,
  WuuDesktopApi,
} from "../shared/protocol";
import { ComposerVoiceInput } from "./ComposerVoiceInput";

let container: HTMLDivElement;
let root: Root | null = null;
let speechHandler: ((event: SpeechRecognitionEvent) => void) | undefined;

function installApi(overrides: Partial<WuuDesktopApi> = {}): void {
  const api: Partial<WuuDesktopApi> = {
    platform: "darwin",
    startSpeechRecognition: vi.fn().mockResolvedValue({
      ok: true,
      session_id: "speech-1",
    }),
    stopSpeechRecognition: vi.fn().mockResolvedValue({ ok: true }),
    onSpeechRecognitionEvent: vi.fn((handler) => {
      speechHandler = handler;
      return () => {
        speechHandler = undefined;
      };
    }),
    polishText: vi.fn().mockResolvedValue({ text: "polished" }),
    ...overrides,
  };
  (window as unknown as { wuu: WuuDesktopApi }).wuu =
    api as WuuDesktopApi;
}

function renderVoiceInput(initialPrompt = "", polishAvailable = true): void {
  function Harness(): JSX.Element {
    const [prompt, setPrompt] = useState(initialPrompt);
    return (
      <>
        <output data-testid="prompt">{prompt}</output>
        <ComposerVoiceInput
          prompt={prompt}
          setPrompt={setPrompt}
          disabled={false}
          locale="zh-CN"
          polishAvailable={polishAvailable}
        />
      </>
    );
  }
  act(() => {
    root = createRoot(container);
    root.render(<Harness />);
  });
}

beforeEach(() => {
  window.localStorage.clear();
  speechHandler = undefined;
  container = document.createElement("div");
  document.body.appendChild(container);
});

afterEach(() => {
  act(() => root?.unmount());
  root = null;
  delete (window as unknown as { wuu?: WuuDesktopApi }).wuu;
  container.remove();
});

describe("ComposerVoiceInput", () => {
  it("keeps free ASR output as raw text when BYOK polish is off", async () => {
    const polishText = vi.fn();
    installApi({ polishText });
    renderVoiceInput("前文");

    await act(async () => {
      container
        .querySelector<HTMLButtonElement>(".composer-voice-button")
        ?.click();
    });
    act(() => {
      speechHandler?.({
        type: "result",
        text: "这是原始转写",
        is_final: true,
      });
    });

    expect(container.querySelector("output")?.textContent).toBe(
      "前文\n这是原始转写",
    );
    expect(polishText).not.toHaveBeenCalled();
  });

  it("uses the configured BYOK model only when polish is enabled", async () => {
    const polishText = vi.fn().mockResolvedValue({ text: "这是润色文本。" });
    installApi({ polishText });
    renderVoiceInput();

    act(() => {
      container
        .querySelector<HTMLButtonElement>(".composer-polish-toggle")
        ?.click();
    });
    await act(async () => {
      container
        .querySelector<HTMLButtonElement>(".composer-voice-button")
        ?.click();
    });
    await act(async () => {
      speechHandler?.({
        type: "result",
        text: "这是 原始 文本",
        is_final: true,
      });
      await Promise.resolve();
    });

    expect(polishText).toHaveBeenCalledWith("这是 原始 文本");
    expect(container.querySelector("output")?.textContent).toBe(
      "这是润色文本。",
    );
  });

  it("shows an understandable microphone permission failure", async () => {
    installApi({
      startSpeechRecognition: vi.fn().mockResolvedValue({
        ok: false,
        error: "microphone_permission_denied",
      }),
    });
    renderVoiceInput();

    await act(async () => {
      container
        .querySelector<HTMLButtonElement>(".composer-voice-button")
        ?.click();
    });

    expect(container.querySelector('[role="alert"]')?.textContent).toContain(
      "系统设置",
    );
    expect(container.querySelector('[role="alert"]')?.textContent).toContain(
      "麦克风",
    );
  });

  it("keeps raw transcription when BYOK polish fails", async () => {
    installApi({
      polishText: vi.fn().mockRejectedValue(new Error("provider failed")),
    });
    renderVoiceInput();
    act(() => {
      container
        .querySelector<HTMLButtonElement>(".composer-polish-toggle")
        ?.click();
    });
    await act(async () => {
      container
        .querySelector<HTMLButtonElement>(".composer-voice-button")
        ?.click();
    });
    await act(async () => {
      speechHandler?.({
        type: "result",
        text: "保留原始转写",
        is_final: true,
      });
      await Promise.resolve();
    });

    expect(container.querySelector("output")?.textContent).toBe(
      "保留原始转写",
    );
    expect(container.querySelector('[role="alert"]')?.textContent).toContain(
      "已保留原始转写",
    );
  });
});
