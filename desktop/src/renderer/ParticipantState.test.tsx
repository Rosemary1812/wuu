import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import type {
  ParticipantProfile,
  ParticipantSaveParams,
  WuuDesktopApi,
} from "../shared/protocol";
import {
  useParticipantState,
  type ParticipantStateController,
} from "./ParticipantState";

let mountedRoots: Root[] = [];

afterEach(() => {
  act(() => {
    for (const root of mountedRoots) root.unmount();
  });
  mountedRoots = [];
  document.body.innerHTML = "";
  delete (window as unknown as { wuu?: unknown }).wuu;
  vi.restoreAllMocks();
});

async function flushEffects(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
  await Promise.resolve();
  await Promise.resolve();
  await Promise.resolve();
  await new Promise((resolve) => window.setTimeout(resolve, 0));
}

function installWuuStub(overrides: Partial<WuuDesktopApi>): void {
  (window as unknown as { wuu: WuuDesktopApi }).wuu = {
    ...overrides,
  } as WuuDesktopApi;
}

function participant(
  overrides: Partial<ParticipantProfile> = {},
): ParticipantProfile {
  return {
    id: "participant-1",
    name: "Alex",
    role: "Engineer",
    tagline: "Builds",
    model: "gpt-test",
    memory: "Notes",
    ...overrides,
  } as ParticipantProfile;
}

async function renderParticipantState({
  initialized = true,
}: {
  initialized?: boolean;
} = {}): Promise<{
  get: () => ParticipantStateController;
  setInitialized: (next: boolean) => void;
  setStatus: ReturnType<typeof vi.fn>;
  onOpenParticipantProfile: ReturnType<typeof vi.fn>;
}> {
  let latest: ParticipantStateController | undefined;
  let currentInitialized = initialized;
  const setStatus = vi.fn();
  const onOpenParticipantProfile = vi.fn();

  function Probe(): null {
    latest = useParticipantState({
      initialized: currentInitialized,
      setStatus,
      onOpenParticipantProfile,
      archivedNoticeMs: 10,
    });
    return null;
  }

  const container = document.createElement("div");
  document.body.appendChild(container);
  const root = createRoot(container);
  mountedRoots.push(root);

  await act(async () => {
    root.render(createElement(Probe));
    await flushEffects();
  });

  return {
    get: () => {
      if (!latest) {
        throw new Error("participant state was not rendered");
      }
      return latest;
    },
    setInitialized: (next) => {
      currentInitialized = next;
    },
    setStatus,
    onOpenParticipantProfile,
  };
}

describe("useParticipantState", () => {
  it("refreshes participants and patches the open profile panel with fresh data", async () => {
    installWuuStub({
      listParticipants: vi.fn().mockResolvedValue({
        participants: [participant({ name: "Fresh Alex" })],
      }),
    });
    const hook = await renderParticipantState();

    act(() => {
      hook.get().setParticipantPanel({
        mode: "edit",
        participant: participant({ name: "Stale Alex" }),
        loading: false,
      });
    });
    await act(async () => {
      await hook.get().refreshParticipants();
      await flushEffects();
    });

    expect(hook.get().participants.map((item) => item.name)).toEqual([
      "Fresh Alex",
    ]);
    expect(hook.get().participantPanel?.participant?.name).toBe("Fresh Alex");
  });

  it("opens the profile panel after running the host side-panel close callback", async () => {
    const hook = await renderParticipantState();

    act(() => {
      hook.get().openParticipantProfile(participant());
    });

    expect(hook.onOpenParticipantProfile).toHaveBeenCalledTimes(1);
    expect(hook.get().participantPanel).toMatchObject({
      mode: "edit",
      participant: { id: "participant-1" },
      loading: false,
    });
  });

  it("imports participant templates by updating matching names in place", async () => {
    const saved = participant({ id: "participant-1", role: "Lead" });
    const saveParticipant = vi
      .fn()
      .mockImplementation(async (params: ParticipantSaveParams) => ({
        participant: participant({
          id: params.id ?? "new-participant",
          name: params.name,
          role: params.role,
        }),
      }));
    installWuuStub({ saveParticipant });
    const hook = await renderParticipantState();
    act(() => {
      hook.get().setParticipants([participant({ id: saved.id, name: "Alex" })]);
    });
    await flushEffects();

    const file = {
      text: async () =>
        JSON.stringify({
          version: 1,
          participants: [
            {
              name: "alex",
              role: "Lead",
              tagline: "Runs delivery",
              model: "gpt-test",
              memory: "Updated",
            },
          ],
        }),
    } as File;
    await act(async () => {
      await hook.get().importParticipantTemplate(file);
      await flushEffects();
    });

    expect(saveParticipant).toHaveBeenCalledWith(
      expect.objectContaining({ id: "participant-1", name: "alex" }),
    );
    expect(hook.get().participants).toEqual([
      expect.objectContaining({ id: "participant-1", role: "Lead" }),
    ]);
    expect(hook.setStatus).toHaveBeenCalledWith("已导入 1 个 Agent");
  });
});
