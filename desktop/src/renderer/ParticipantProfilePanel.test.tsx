import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type {
  MemoryReadResult,
  ParticipantProfile,
  ProviderModelSummary,
  ProviderSummary,
  WuuDesktopApi,
} from "../shared/protocol";
import { ParticipantProfilePanel } from "./ParticipantProfilePanel";

let container: HTMLDivElement;
let root: Root | null = null;
const pendingInputCleanups: Array<() => void> = [];

// 编辑模式挂载即拉取 memory/read（只读记忆摘要），所以每个测试都需要
// window.wuu 桩；镜像 MemoryPanel.test.tsx 的安装/清理方式。
function memoryReadFixture(
  overrides: Partial<MemoryReadResult> = {},
): MemoryReadResult {
  return {
    index_md: "- [协作教训](lessons.md) — 先读后写",
    files: [
      {
        name: "lessons.md",
        description: "协作教训",
        type: "lesson",
        mtime: "2026-07-03T08:00:00Z",
      },
    ],
    ...overrides,
  };
}

function installMemoryStub(
  readMemoryRaw = vi.fn().mockResolvedValue(memoryReadFixture()),
): ReturnType<typeof vi.fn> {
  const stub = { readMemoryRaw } as unknown as WuuDesktopApi;
  (globalThis as { wuu?: WuuDesktopApi }).wuu = stub;
  (window as unknown as { wuu: WuuDesktopApi }).wuu = stub;
  return readMemoryRaw;
}

beforeEach(() => {
  container = document.createElement("div");
  document.body.appendChild(container);
  installMemoryStub();
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  root = null;
  container.remove();
  while (pendingInputCleanups.length > 0) {
    const cleanup = pendingInputCleanups.pop();
    cleanup?.();
  }
  delete (globalThis as { wuu?: WuuDesktopApi }).wuu;
  delete (window as { wuu?: WuuDesktopApi }).wuu;
  vi.useRealTimers();
  vi.restoreAllMocks();
});

// jsdom does not lay out real heights. Stub getBoundingClientRect so React's
// effects do not crash on layout queries (the panel uses overflow / flex
// sizing that touches layout during effect runs).
Element.prototype.getBoundingClientRect = function (): DOMRect {
  return {
    x: 0,
    y: 0,
    top: 0,
    left: 0,
    right: 0,
    bottom: 0,
    width: 0,
    height: 0,
    toJSON() {
      return this;
    },
  } as DOMRect;
};

type FileReaderMock = {
  readAsDataURL: (blob: Blob) => void;
  result: string | ArrayBuffer | null;
  onload: ((event: ProgressEvent<FileReader>) => void) | null;
  onerror: ((event: ProgressEvent<FileReader>) => void) | null;
};

function stubFileReader(result: string): () => void {
  const original = window.FileReader;
  class MockReader {
    public result: string | ArrayBuffer | null = null;
    public onload: FileReaderMock["onload"] = null;
    public onerror: FileReaderMock["onerror"] = null;
    public readAsDataURL(blob: Blob): void {
      void blob;
      this.result = result;
      queueMicrotask(() => {
        const event = { target: this } as unknown as ProgressEvent<FileReader>;
        this.onload?.(event);
      });
    }
  }
  Object.defineProperty(window, "FileReader", {
    configurable: true,
    writable: true,
    value: MockReader,
  });
  return () => {
    Object.defineProperty(window, "FileReader", {
      configurable: true,
      writable: true,
      value: original,
    });
  };
}

function setInputFiles(input: HTMLInputElement, files: File[]): void {
  // jsdom refuses ad-hoc objects for `files`. We replace the prototype
  // getter so the input reads back the files we want. The original getter
  // is captured and restored via `pendingInputCleanups` so the prototype
  // does not leak between tests.
  const originalDescriptor = Object.getOwnPropertyDescriptor(
    HTMLInputElement.prototype,
    "files",
  );
  Object.defineProperty(HTMLInputElement.prototype, "files", {
    configurable: true,
    get(this: HTMLInputElement): FileList {
      // Marker on the element tells us what files this instance should report.
      const marker = (this as unknown as { __mockFiles?: File[] }).__mockFiles;
      if (marker) {
        return {
          0: marker[0],
          length: marker.length,
          item: (index: number) => marker[index],
        } as unknown as FileList;
      }
      return originalDescriptor?.get?.call(this) ?? ([] as unknown as FileList);
    },
    set() {
      // swallow — assigned files are stored on the element via __mockFiles
    },
  });
  (input as unknown as { __mockFiles: File[] }).__mockFiles = files;
  pendingInputCleanups.push(() => {
    if (originalDescriptor) {
      Object.defineProperty(HTMLInputElement.prototype, "files", originalDescriptor);
    } else {
      delete (HTMLInputElement.prototype as { files?: PropertyDescriptor }).files;
    }
  });
}

function mount(props: {
  participant?: ParticipantProfile;
  mode: "new" | "edit";
  providers?: ProviderSummary[];
  archived?: boolean;
  feedbackReply?: string;
  onSave?: (params: Parameters<typeof ParticipantProfilePanel>[0]["onSave"] extends (p: infer P) => void ? P : never) => void;
  onOpenMemoryPanel?: (id: string) => void;
  onRetire?: (id: string) => void;
  forkedFromName?: string;
}): void {
  if (root === null) {
    root = createRoot(container);
  }
  act(() => {
    root!.render(
      <ParticipantProfilePanel
        mode={props.mode}
        participant={props.participant}
        providers={props.providers}
        archived={props.archived}
        feedbackReply={props.feedbackReply}
        onClose={() => {}}
        onSave={props.onSave ?? (() => {})}
        onFeedback={() => {}}
        onOpenMemoryPanel={props.onOpenMemoryPanel ?? (() => {})}
        onRetire={props.onRetire ?? (() => {})}
        forkedFromName={props.forkedFromName}
      />,
    );
  });
}

function participantFixture(overrides: Partial<ParticipantProfile> = {}): ParticipantProfile {
  return {
    id: "p-1",
    kind: "named",
    name: "Noel",
    role: "reviewer",
    avatar_image: undefined,
    tagline: "Find regressions",
    model: "anthropic:claude-sonnet-4-7",
    memory: "Long memory line\nSecond line",
    track_record: [],
    ...overrides,
  };
}

function modelFixture(id: string, displayName?: string): ProviderModelSummary {
  return {
    id,
    display_name: displayName ?? id,
  };
}

function providersFixture(): ProviderSummary[] {
  return [
    {
      name: "anthropic",
      type: "anthropic",
      model: "claude-sonnet-4-7",
      models: [modelFixture("claude-sonnet-4-7", "Claude Sonnet 4.7")],
    },
    {
      name: "openai",
      type: "openai",
      model: "gpt-5",
      models: [modelFixture("gpt-5"), modelFixture("gpt-4o")],
    },
  ];
}

function changeInput(input: HTMLInputElement | HTMLTextAreaElement, value: string): void {
  const setter = Object.getOwnPropertyDescriptor(
    Object.getPrototypeOf(input),
    "value",
  )?.set;
  setter?.call(input, value);
  act(() => {
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
}

function changeSelect(select: HTMLSelectElement, value: string): void {
  const setter = Object.getOwnPropertyDescriptor(
    HTMLSelectElement.prototype,
    "value",
  )?.set;
  setter?.call(select, value);
  act(() => {
    select.dispatchEvent(new Event("change", { bubbles: true }));
  });
}

describe("ParticipantProfilePanel — fork badge", () => {
  it("shows \"X 的分身\" for a forked participant using the resolved母体 name", () => {
    mount({
      mode: "edit",
      participant: participantFixture({ forked_from_id: "p-mother" }),
      forkedFromName: "小青",
    });
    const badge = container.querySelector(".participant-profile-fork-badge");
    expect(badge?.textContent).toBe("小青 的分身");
  });

  it("omits the fork badge for an ordinary participant", () => {
    mount({ mode: "edit", participant: participantFixture() });
    expect(
      container.querySelector(".participant-profile-fork-badge"),
    ).toBeNull();
  });
});

describe("ParticipantProfilePanel — identity and model", () => {
  it("backfills identity fields from the participant when editing", () => {
    mount({
      mode: "edit",
      participant: participantFixture({
        name: "Mira",
        tagline: "Catches typos",
        role: "reviewer",
      }),
      providers: providersFixture(),
    });

    const nameInput = container.querySelector<HTMLInputElement>(
      "input[data-field='name']",
    );
    const taglineInput = container.querySelector<HTMLInputElement>(
      "input[data-field='tagline']",
    );
    const roleSelect = container.querySelector<HTMLSelectElement>(
      "select[data-field='role']",
    );
    const modelSelect = container.querySelector<HTMLSelectElement>(
      "select[data-field='model']",
    );

    expect(nameInput?.value).toBe("Mira");
    expect(taglineInput?.value).toBe("Catches typos");
    expect(roleSelect?.value).toBe("reviewer");
    expect(modelSelect?.value).toBe("anthropic:claude-sonnet-4-7");
    // 旧 Memory 编辑框已随扁平记忆退役，不再渲染。
    expect(
      container.querySelector("textarea[data-field='memory']"),
    ).toBeNull();

    // The model <select> must have a "follow global" first option and an
    // <optgroup> per provider.
    const firstOption = modelSelect?.options.item(0);
    expect(firstOption?.value).toBe("");
    expect(firstOption?.text).toBe("跟随全局");
    const optgroups = Array.from(
      modelSelect?.querySelectorAll("optgroup") ?? [],
    );
    expect(optgroups).toHaveLength(2);
    expect(optgroups[0]?.label).toBe("anthropic");
    expect(
      Array.from(optgroups[0]?.querySelectorAll("option") ?? []).map(
        (option) => (option as HTMLOptionElement).value,
      ),
    ).toEqual(["anthropic:claude-sonnet-4-7"]);
  });

  it("serializes model selection as `provider:model` and sends avatar_image on save", async () => {
    const onSave = vi.fn();
    mount({
      mode: "edit",
      participant: participantFixture({ name: "Mira" }),
      providers: providersFixture(),
      onSave,
    });

    changeSelect(
      container.querySelector<HTMLSelectElement>("select[data-field='model']")!,
      "openai:gpt-4o",
    );
    changeInput(
      container.querySelector<HTMLInputElement>("input[data-field='name']")!,
      "  Mira O  ",
    );

    // Simulate a chosen avatar file by directly toggling the avatar state
    // through the file input change handler. We create a small PNG data URL
    // and a 600KB binary blob to cover both normal and oversize cases.
    const fileInput = container.querySelector<HTMLInputElement>(
      "input[type='file']",
    )!;
    const smallFile = new File([new Uint8Array(1024)], "avatar.png", {
      type: "image/png",
    });
    Object.defineProperty(smallFile, "size", { value: 4 * 1024 });
    const expectedDataUrl =
      "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9U3K5RoAAAAASUVORK5CYII=";
    stubFileReader(expectedDataUrl);
    setInputFiles(fileInput, [smallFile]);
    await act(async () => {
      fileInput.dispatchEvent(new Event("change", { bubbles: true }));
      // Reader promise resolution is queued; flush microtasks.
      await Promise.resolve();
      await Promise.resolve();
    });

    const saveButton = Array.from(container.querySelectorAll("button")).find(
      (button) => button.textContent?.trim() === "保存",
    )!;
    await act(async () => {
      saveButton.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    expect(onSave).toHaveBeenCalledTimes(1);
    const params = onSave.mock.calls[0]?.[0] as {
      name: string;
      model: string;
      avatar_image?: string;
    };
    expect(params.name).toBe("Mira O");
    expect(params.model).toBe("openai:gpt-4o");
    expect(params.avatar_image).toMatch(/^data:image\/png/);
  });

  it("sends an empty model string when the user picks 'follow global'", async () => {
    const onSave = vi.fn();
    mount({
      mode: "edit",
      participant: participantFixture({ name: "Mira" }),
      providers: providersFixture(),
      onSave,
    });

    changeSelect(
      container.querySelector<HTMLSelectElement>("select[data-field='model']")!,
      "",
    );

    const saveButton = Array.from(container.querySelectorAll("button")).find(
      (button) => button.textContent?.trim() === "保存",
    )!;
    await act(async () => {
      saveButton.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    expect(onSave).toHaveBeenCalledTimes(1);
    const params = onSave.mock.calls[0]?.[0] as { model: string };
    expect(params.model).toBe("");
  });

  it("renders an orphan model fallback option and preserves the pin across save", async () => {
    const onSave = vi.fn();
    mount({
      mode: "edit",
      participant: participantFixture({
        name: "Mira",
        model: "missing:gone",
      }),
      providers: providersFixture(),
      onSave,
    });

    const modelSelect = container.querySelector<HTMLSelectElement>(
      "select[data-field='model']",
    )!;
    // The orphan pin must remain the selected value rather than silently
    // collapsing to "follow global".
    expect(modelSelect.value).toBe("missing:gone");
    const fallbackOption = Array.from(modelSelect.options).find(
      (option) => option.value === "missing:gone",
    );
    expect(fallbackOption).toBeDefined();
    expect(fallbackOption?.disabled).toBe(false);
    expect(fallbackOption?.text).toContain("missing:gone");
    expect(fallbackOption?.text).toContain("不可用");

    const saveButton = Array.from(container.querySelectorAll("button")).find(
      (button) => button.textContent?.trim() === "保存",
    )!;
    await act(async () => {
      saveButton.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    expect(onSave).toHaveBeenCalledTimes(1);
    const params = onSave.mock.calls[0]?.[0] as { model: string };
    // Saving without explicitly retargeting the model must keep the orphan
    // pin, not silently downgrade to "follow global".
    expect(params.model).toBe("missing:gone");
  });

  it("does not resend avatar_image on a second save when the avatar did not change", async () => {
    const onSave = vi.fn();
    const initialParticipant = participantFixture({
      name: "Mira",
      avatar_image: "data:image/png;base64,STORED",
    });
    mount({
      mode: "edit",
      participant: initialParticipant,
      providers: providersFixture(),
      onSave,
    });

    const fileInput = container.querySelector<HTMLInputElement>(
      "input[type='file']",
    )!;
    const smallFile = new File([new Uint8Array(1024)], "avatar.png", {
      type: "image/png",
    });
    Object.defineProperty(smallFile, "size", { value: 4 * 1024 });
    const firstDataUrl =
      "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9U3K5RoAAAAASUVORK5CYII=";
    stubFileReader(firstDataUrl);
    setInputFiles(fileInput, [smallFile]);
    await act(async () => {
      fileInput.dispatchEvent(new Event("change", { bubbles: true }));
      await Promise.resolve();
      await Promise.resolve();
    });

    const saveButton = Array.from(container.querySelectorAll("button")).find(
      (button) => button.textContent?.trim() === "保存",
    )!;
    await act(async () => {
      saveButton.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    // The parent now mirrors back the saved participant as a fresh object
    // reference with the same id. The panel's reset effect is keyed on
    // [participant?.id, mode] so it does NOT re-initialize the form, but
    // the parent did rebuild the panel state object.
    const refreshed = {
      ...initialParticipant,
      avatar_image: firstDataUrl,
    };
    mount({
      mode: "edit",
      participant: refreshed,
      providers: providersFixture(),
      onSave,
    });

    const saveButton2 = Array.from(container.querySelectorAll("button")).find(
      (button) => button.textContent?.trim() === "保存",
    )!;
    await act(async () => {
      saveButton2.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    expect(onSave).toHaveBeenCalledTimes(2);
    const firstParams = onSave.mock.calls[0]?.[0] as {
      avatar_image?: string;
      clear_avatar_image?: boolean;
    };
    const secondParams = onSave.mock.calls[1]?.[0] as {
      avatar_image?: string;
      clear_avatar_image?: boolean;
    };
    expect(firstParams.avatar_image).toMatch(/^data:image\/png/);
    expect(secondParams.avatar_image).toBeUndefined();
    expect(secondParams.clear_avatar_image).toBeUndefined();
  });
});

describe("ParticipantProfilePanel — read-only memory summary", () => {
  it("renders the memory/read index for the participant notebook", async () => {
    const readMemoryRaw = installMemoryStub();
    mount({
      mode: "edit",
      participant: participantFixture({ id: "agent-7" }),
      providers: providersFixture(),
    });
    await act(async () => {
      await Promise.resolve();
    });

    expect(readMemoryRaw).toHaveBeenCalledWith({
      scope: "participant",
      participant_id: "agent-7",
    });
    expect(
      container.querySelector("[data-testid='participant-memory-summary']"),
    ).not.toBeNull();
    expect(container.textContent).toContain("协作教训");
    expect(container.textContent).toContain("在记忆面板中管理");
  });

  it("shows a one-line empty state when the index is empty", async () => {
    installMemoryStub(
      vi.fn().mockResolvedValue(memoryReadFixture({ index_md: "", files: [] })),
    );
    mount({
      mode: "edit",
      participant: participantFixture(),
      providers: providersFixture(),
    });
    await act(async () => {
      await Promise.resolve();
    });

    expect(container.textContent).toContain("还没有关于 Ta 的记忆。");
  });

  it("degrades to the not-ready line when the backend lacks memory/read", async () => {
    installMemoryStub(
      vi.fn().mockRejectedValue(new Error('unknown method "memory/read"')),
    );
    mount({
      mode: "edit",
      participant: participantFixture(),
      providers: providersFixture(),
    });
    await act(async () => {
      await Promise.resolve();
    });

    expect(container.textContent).toContain("记忆服务尚未就绪。");
  });

  it("fires onOpenMemoryPanel with the participant id", async () => {
    const onOpenMemoryPanel = vi.fn();
    mount({
      mode: "edit",
      participant: participantFixture({ id: "agent-7" }),
      providers: providersFixture(),
      onOpenMemoryPanel,
    });
    await act(async () => {
      await Promise.resolve();
    });

    const manageButton = Array.from(container.querySelectorAll("button")).find(
      (button) => button.textContent?.trim() === "在记忆面板中管理",
    );
    expect(manageButton).toBeDefined();
    act(() => {
      manageButton!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    expect(onOpenMemoryPanel).toHaveBeenCalledWith("agent-7");
  });

  it("shows the feedback reply line when provided", async () => {
    mount({
      mode: "edit",
      participant: participantFixture(),
      providers: providersFixture(),
      feedbackReply: "已记入身份笔记本。",
    });
    await act(async () => {
      await Promise.resolve();
    });

    expect(container.textContent).toContain("已记入身份笔记本。");
  });
});

describe("ParticipantProfilePanel — archive confirm dialog", () => {
  function archiveButton(): HTMLButtonElement | undefined {
    return Array.from(container.querySelectorAll("button")).find(
      (button) => button.textContent?.trim() === "归档此同事",
    );
  }

  function dialogButton(label: string): HTMLButtonElement | undefined {
    return Array.from(container.querySelectorAll("button")).find(
      (button) => button.textContent?.trim() === label,
    );
  }

  it("opens the confirm dialog without firing onRetire", () => {
    const onRetire = vi.fn();
    mount({
      mode: "edit",
      participant: participantFixture(),
      providers: providersFixture(),
      onRetire,
    });

    const trigger = archiveButton();
    expect(trigger).toBeDefined();
    act(() => {
      trigger!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    expect(onRetire).not.toHaveBeenCalled();
    expect(container.textContent).toContain(
      "Ta 的记忆和对话将完整归档，私聊变为只读；之后可以随时复职。",
    );
    expect(dialogButton("归档")).toBeDefined();
    expect(dialogButton("再想想")).toBeDefined();
  });

  it("fires onRetire with the participant id on 归档 and closes the dialog", () => {
    const onRetire = vi.fn();
    mount({
      mode: "edit",
      participant: participantFixture({ id: "agent-42" }),
      providers: providersFixture(),
      onRetire,
    });

    act(() => {
      archiveButton()!.dispatchEvent(
        new MouseEvent("click", { bubbles: true }),
      );
    });
    act(() => {
      dialogButton("归档")!.dispatchEvent(
        new MouseEvent("click", { bubbles: true }),
      );
    });

    expect(onRetire).toHaveBeenCalledTimes(1);
    expect(onRetire).toHaveBeenCalledWith("agent-42");
    expect(dialogButton("再想想")).toBeUndefined();
  });

  it("closes the dialog without firing onRetire on 再想想", () => {
    const onRetire = vi.fn();
    mount({
      mode: "edit",
      participant: participantFixture(),
      providers: providersFixture(),
      onRetire,
    });

    act(() => {
      archiveButton()!.dispatchEvent(
        new MouseEvent("click", { bubbles: true }),
      );
    });
    act(() => {
      dialogButton("再想想")!.dispatchEvent(
        new MouseEvent("click", { bubbles: true }),
      );
    });

    expect(onRetire).not.toHaveBeenCalled();
    expect(dialogButton("归档")).toBeUndefined();
  });

  it("shows the 已归档 receipt when archived is set", () => {
    mount({
      mode: "edit",
      participant: participantFixture(),
      providers: providersFixture(),
      archived: true,
    });

    expect(container.textContent).toContain("已归档");
    expect(archiveButton()).toBeUndefined();
  });
});

describe("ParticipantProfilePanel — avatar size precheck", () => {
  it("shows an inline error and skips save when the avatar exceeds 512KB", async () => {
    const onSave = vi.fn();
    mount({
      mode: "edit",
      participant: participantFixture({ name: "Mira" }),
      providers: providersFixture(),
      onSave,
    });

    const fileInput = container.querySelector<HTMLInputElement>(
      "input[type='file']",
    )!;
    const huge = new File([new Uint8Array(8)], "big.png", {
      type: "image/png",
    });
    Object.defineProperty(huge, "size", { value: 600 * 1024 });
    setInputFiles(fileInput, [huge]);
    await act(async () => {
      fileInput.dispatchEvent(new Event("change", { bubbles: true }));
    });

    // Inline error copy should be present, distinct from the avatar preview.
    expect(container.textContent).toContain("512KB");

    const saveButton = Array.from(container.querySelectorAll("button")).find(
      (button) => button.textContent?.trim() === "保存",
    )!;
    saveButton.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    expect(onSave).not.toHaveBeenCalled();
  });
});
