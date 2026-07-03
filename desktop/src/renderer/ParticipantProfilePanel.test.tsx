import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type {
  ParticipantProfile,
  ProviderModelSummary,
  ProviderSummary,
} from "../shared/protocol";
import { ParticipantProfilePanel } from "./ParticipantProfilePanel";

let container: HTMLDivElement;
let root: Root | null = null;

beforeEach(() => {
  container = document.createElement("div");
  document.body.appendChild(container);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  root = null;
  container.remove();
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
  // getter so the input reads back the files we want.
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
}

function mount(props: {
  participant?: ParticipantProfile;
  mode: "new" | "edit";
  providers?: ProviderSummary[];
  onSave?: (params: Parameters<typeof ParticipantProfilePanel>[0]["onSave"] extends (p: infer P) => void ? P : never) => void;
  onRetire?: (id: string) => void;
}): void {
  act(() => {
    root = createRoot(container);
    root.render(
      <ParticipantProfilePanel
        mode={props.mode}
        participant={props.participant}
        providers={props.providers}
        onClose={() => {}}
        onSave={props.onSave ?? (() => {})}
        onFeedback={() => {}}
        onReset={() => {}}
        onRetire={props.onRetire ?? (() => {})}
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
    avatar: "N",
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

describe("ParticipantProfilePanel — identity and model", () => {
  it("backfills identity fields from the participant when editing", () => {
    mount({
      mode: "edit",
      participant: participantFixture({
        name: "Mira",
        tagline: "Catches typos",
        role: "reviewer",
        memory: "Remember: read first.",
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
    const memory = container.querySelector<HTMLTextAreaElement>(
      "textarea[data-field='memory']",
    );

    expect(nameInput?.value).toBe("Mira");
    expect(taglineInput?.value).toBe("Catches typos");
    expect(roleSelect?.value).toBe("reviewer");
    expect(modelSelect?.value).toBe("anthropic:claude-sonnet-4-7");
    expect(memory?.value).toBe("Remember: read first.");

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
});

describe("ParticipantProfilePanel — retire two-step", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  it("does not fire onRetire on first click; switches copy to a danger '确认退役' label", () => {
    const onRetire = vi.fn();
    mount({
      mode: "edit",
      participant: participantFixture(),
      providers: providersFixture(),
      onRetire,
    });

    const retireButton = Array.from(container.querySelectorAll("button")).find(
      (button) => button.textContent?.trim() === "退役",
    );
    expect(retireButton).toBeDefined();
    act(() => {
      retireButton!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    expect(onRetire).not.toHaveBeenCalled();
    const confirm = Array.from(container.querySelectorAll("button")).find(
      (button) => button.textContent?.trim() === "确认退役",
    );
    expect(confirm).toBeDefined();
  });

  it("fires onRetire with the participant id on the second click", () => {
    const onRetire = vi.fn();
    mount({
      mode: "edit",
      participant: participantFixture({ id: "agent-42" }),
      providers: providersFixture(),
      onRetire,
    });

    const retireButton = Array.from(container.querySelectorAll("button")).find(
      (button) => button.textContent?.trim() === "退役",
    )!;
    act(() => {
      retireButton.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    const confirm = Array.from(container.querySelectorAll("button")).find(
      (button) => button.textContent?.trim() === "确认退役",
    )!;
    act(() => {
      confirm.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    expect(onRetire).toHaveBeenCalledTimes(1);
    expect(onRetire).toHaveBeenCalledWith("agent-42");
  });

  it("reverts the confirm label to 退役 after the 5s timeout", () => {
    const onRetire = vi.fn();
    mount({
      mode: "edit",
      participant: participantFixture(),
      providers: providersFixture(),
      onRetire,
    });

    const retireButton = Array.from(container.querySelectorAll("button")).find(
      (button) => button.textContent?.trim() === "退役",
    )!;
    act(() => {
      retireButton.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    expect(
      Array.from(container.querySelectorAll("button")).some(
        (button) => button.textContent?.trim() === "确认退役",
      ),
    ).toBe(true);

    act(() => {
      vi.advanceTimersByTime(5_000);
    });

    expect(
      Array.from(container.querySelectorAll("button")).some(
        (button) => button.textContent?.trim() === "退役",
      ),
    ).toBe(true);
    expect(
      Array.from(container.querySelectorAll("button")).some(
        (button) => button.textContent?.trim() === "确认退役",
      ),
    ).toBe(false);
    expect(onRetire).not.toHaveBeenCalled();
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
