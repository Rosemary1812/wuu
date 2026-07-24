import { EventEmitter } from "node:events";
import { describe, expect, it, vi } from "vitest";
import { SpeechRecognitionService } from "./speechRecognition";

function fakeProcess() {
  const process = new EventEmitter() as EventEmitter & {
    stdout: EventEmitter & { setEncoding: ReturnType<typeof vi.fn> };
    stderr: EventEmitter & { setEncoding: ReturnType<typeof vi.fn> };
    stdin: { write: ReturnType<typeof vi.fn>; end: ReturnType<typeof vi.fn> };
    kill: ReturnType<typeof vi.fn>;
  };
  process.stdout = Object.assign(new EventEmitter(), { setEncoding: vi.fn() });
  process.stderr = Object.assign(new EventEmitter(), { setEncoding: vi.fn() });
  process.stdin = { write: vi.fn(), end: vi.fn() };
  process.kill = vi.fn();
  return process;
}

describe("SpeechRecognitionService", () => {
  it("rejects unsupported platforms without asking for permission", async () => {
    const ask = vi.fn();
    const service = new SpeechRecognitionService({
      platform: "win32",
      resourcesPath: "/resources",
      askForMicrophoneAccess: ask,
      spawnHelper: vi.fn(),
    });

    await expect(service.start("en-US", vi.fn())).resolves.toEqual({
      ok: false,
      error: "platform_unsupported",
    });
    expect(ask).not.toHaveBeenCalled();
  });

  it("reports denied microphone permission without launching the helper", async () => {
    const spawnHelper = vi.fn();
    const events: unknown[] = [];
    const service = new SpeechRecognitionService({
      platform: "darwin",
      resourcesPath: "/resources",
      askForMicrophoneAccess: async () => false,
      spawnHelper,
    });

    await expect(service.start("zh-CN", (event) => events.push(event))).resolves.toEqual({
      ok: false,
      error: "microphone_permission_denied",
    });
    expect(events).toEqual([
      { type: "state", state: "requesting_microphone_permission" },
    ]);
    expect(spawnHelper).not.toHaveBeenCalled();
  });

  it("forwards helper events and stops the active process", async () => {
    const child = fakeProcess();
    const events: unknown[] = [];
    const service = new SpeechRecognitionService({
      platform: "darwin",
      resourcesPath: "/resources",
      askForMicrophoneAccess: async () => true,
      spawnHelper: vi.fn(() => child as never),
    });

    await expect(service.start("zh-CN", (event) => events.push(event))).resolves.toEqual({
      ok: true,
      session_id: "1",
    });
    child.stdout.emit(
      "data",
      '{"type":"state","state":"listening"}\n{"type":"result","text":"你好","is_final":false}\n',
    );
    service.stop();

    expect(events).toContainEqual({ type: "state", state: "listening" });
    expect(events).toContainEqual({
      type: "result",
      text: "你好",
      is_final: false,
    });
    expect(child.stdin.write).toHaveBeenCalledWith("\n");
    expect(child.stdin.end).toHaveBeenCalled();
  });

  it("does not replace a structured helper failure with a generic exit error", async () => {
    const child = fakeProcess();
    const events: unknown[] = [];
    const service = new SpeechRecognitionService({
      platform: "darwin",
      resourcesPath: "/resources",
      askForMicrophoneAccess: async () => true,
      spawnHelper: vi.fn(() => child as never),
    });
    await service.start("zh-CN", (event) => events.push(event));

    child.stdout.emit(
      "data",
      '{"type":"error","code":"speech_permission_denied","message":"denied"}\n',
    );
    child.emit("exit", 3);

    expect(events).toContainEqual({
      type: "error",
      code: "speech_permission_denied",
      message: "denied",
    });
    expect(events).not.toContainEqual(
      expect.objectContaining({ code: "recognition_process_failed" }),
    );
  });
});
