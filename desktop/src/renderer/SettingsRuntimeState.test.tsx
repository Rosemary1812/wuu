import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import type {
  CodexPetsSnapshot,
  SettingsUsageRange,
  SettingsUsageResponse,
  WuuDesktopApi,
} from "../shared/protocol";
import {
  useSettingsRuntimeState,
  type SettingsRuntimeState,
} from "./SettingsRuntimeState";

let mountedRoots: Root[] = [];

afterEach(() => {
  act(() => {
    for (const root of mountedRoots) root.unmount();
  });
  mountedRoots = [];
  document.body.innerHTML = "";
  delete (window as unknown as { wuu?: unknown }).wuu;
});

function snapshot(enabled: boolean): CodexPetsSnapshot {
  return {
    home: "/tmp/pets",
    enabled,
    selected_id: enabled ? "alpha" : "",
    errors: [],
    pets: enabled
      ? [
          {
            id: "alpha",
            display_name: "Alpha Pet",
            description: "",
            manifest_path: "/tmp/pets/alpha/pet.json",
            spritesheet_path: "/tmp/pets/alpha/spritesheet.webp",
            spritesheet_url: "wuu-file://local/alpha",
          },
        ]
      : [],
  };
}

function usage(range: SettingsUsageRange): SettingsUsageResponse {
  return {
    range,
    total_sessions: range === "7d" ? 7 : 12,
    generated_at: "2026-07-09T00:00:00Z",
    metrics: {
      prompt_tokens: 0,
      context_tokens: 0,
      input_tokens: 0,
      output_tokens: 0,
      cache_read_tokens: 0,
      cache_creation_tokens: 0,
      cache_hit_rate: 0,
      turns: 0,
      agents: 0,
      date_range: ["2026-07-01", "2026-07-09"],
      active_days: 0,
    },
    model_breakdowns: [],
    days: [],
  };
}

function installWuuStub(overrides: Partial<WuuDesktopApi>): void {
  (window as unknown as { wuu: WuuDesktopApi }).wuu = {
    ...overrides,
  } as WuuDesktopApi;
}

async function flushEffects(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}

async function renderSettingsRuntimeState(settingsOpen: boolean): Promise<{
  get: () => SettingsRuntimeState;
  rerender: (nextSettingsOpen: boolean) => Promise<void>;
}> {
  let latest: SettingsRuntimeState | undefined;

  function Probe({ open }: { open: boolean }) {
    latest = useSettingsRuntimeState({ settingsOpen: open });
    return null;
  }

  const container = document.createElement("div");
  document.body.appendChild(container);
  const root = createRoot(container);
  mountedRoots.push(root);

  async function rerender(open: boolean): Promise<void> {
    await act(async () => {
      root.render(createElement(Probe, { open }));
      await flushEffects();
    });
  }

  await rerender(settingsOpen);

  return {
    get: () => {
      if (!latest) {
        throw new Error("settings runtime state was not rendered");
      }
      return latest;
    },
    rerender,
  };
}

describe("useSettingsRuntimeState", () => {
  it("loads Codex Pets on mount and stores updated settings snapshots", async () => {
    const initialSnapshot = snapshot(false);
    const updatedSnapshot = snapshot(true);
    const updateCodexPetSettings = vi.fn().mockResolvedValue(updatedSnapshot);
    installWuuStub({
      listCodexPets: vi.fn().mockResolvedValue(initialSnapshot),
      updateCodexPetSettings,
    });

    const hook = await renderSettingsRuntimeState(false);

    expect(hook.get().codexPets).toEqual(initialSnapshot);
    expect(hook.get().codexPetsLoading).toBe(false);
    expect(hook.get().codexPetsError).toBe("");

    await act(async () => {
      await hook.get().updateCodexPets({ enabled: true });
      await flushEffects();
    });

    expect(updateCodexPetSettings).toHaveBeenCalledWith({ enabled: true });
    expect(hook.get().codexPets).toEqual(updatedSnapshot);
  });

  it("loads usage only while settings are open and clears it on close", async () => {
    const getSettingsUsage = vi
      .fn()
      .mockImplementation(async (range: SettingsUsageRange) => usage(range));
    installWuuStub({
      listCodexPets: vi.fn().mockResolvedValue(snapshot(false)),
      updateCodexPetSettings: vi.fn().mockResolvedValue(snapshot(false)),
      getSettingsUsage,
    });

    const hook = await renderSettingsRuntimeState(true);

    expect(getSettingsUsage).toHaveBeenCalledWith("all");
    expect(hook.get().settingsUsage?.range).toBe("all");

    await act(async () => {
      hook.get().setUsageRange("7d");
      await flushEffects();
    });

    expect(getSettingsUsage).toHaveBeenCalledWith("7d");
    expect(hook.get().settingsUsage?.range).toBe("7d");

    await hook.rerender(false);

    expect(hook.get().settingsUsage).toBeUndefined();
  });

  it("surfaces the existing stale-preload error when Codex Pets APIs are missing", async () => {
    installWuuStub({});

    const hook = await renderSettingsRuntimeState(false);

    expect(hook.get().codexPetsLoading).toBe(false);
    expect(hook.get().codexPetsError).toBe(
      "当前桌面进程不支持 Codex Pets，请重启应用",
    );

    let refreshError: unknown;
    await act(async () => {
      try {
        await hook.get().refreshCodexPets();
      } catch (error) {
        refreshError = error;
      }
    });
    expect(refreshError).toBeInstanceOf(Error);
    expect((refreshError as Error).message).toBe(
      "当前桌面进程不支持 Codex Pets，请重启应用",
    );
  });
});
