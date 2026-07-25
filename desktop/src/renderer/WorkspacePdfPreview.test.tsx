import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const pdfMocks = vi.hoisted(() => {
  const globalWorkerOptions: { workerSrc?: string } = {};
  const loadingTask = {
    promise: Promise.resolve({ numPages: 3 }),
    destroy: vi.fn(() => Promise.resolve()),
  };
  const viewer = {
    currentScale: 1.25,
    currentScaleValue: "",
    setDocument: vi.fn(),
    cleanup: vi.fn(),
    decreaseScale: vi.fn(),
    increaseScale: vi.fn(),
  };
  return {
    eventBus: undefined as
      | {
          dispatch: (name: string, value: unknown) => void;
        }
      | undefined,
    getDocument: vi.fn(() => loadingTask),
    globalWorkerOptions,
    loadingTask,
    viewer,
  };
});

vi.mock("pdfjs-dist", () => ({
  GlobalWorkerOptions: pdfMocks.globalWorkerOptions,
  getDocument: pdfMocks.getDocument,
}));

vi.mock("pdfjs-dist/build/pdf.worker.min.mjs?url", () => ({
  default: "pdf-worker.mjs",
}));

vi.mock("pdfjs-dist/web/pdf_viewer.css?inline", () => ({
  default: ".pdfViewer {}",
}));

vi.mock("./styles/workspace-pdf-preview.css?inline", () => ({
  default: ".workspace-pdf-shell {}",
}));

vi.mock("pdfjs-dist/web/pdf_viewer.mjs", () => ({
  EventBus: class {
    private handlers = new Map<string, Set<(value: unknown) => void>>();

    constructor() {
      pdfMocks.eventBus = this;
    }

    on(name: string, handler: (value: unknown) => void): void {
      const handlers = this.handlers.get(name) ?? new Set();
      handlers.add(handler);
      this.handlers.set(name, handlers);
    }

    off(name: string, handler: (value: unknown) => void): void {
      this.handlers.get(name)?.delete(handler);
    }

    dispatch(name: string, value: unknown): void {
      for (const handler of this.handlers.get(name) ?? []) handler(value);
    }
  },
  PDFViewer: class {
    constructor() {
      return pdfMocks.viewer;
    }
  },
}));

vi.mock("./i18n", () => ({
  useI18n: () => ({ t: (key: string) => key }),
}));

import { WorkspacePdfPreview } from "./WorkspacePdfPreview";

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  pdfMocks.getDocument.mockClear();
  pdfMocks.loadingTask.destroy.mockClear();
  pdfMocks.viewer.setDocument.mockClear();
  pdfMocks.viewer.cleanup.mockClear();
  pdfMocks.viewer.decreaseScale.mockClear();
  pdfMocks.viewer.increaseScale.mockClear();
  pdfMocks.viewer.currentScaleValue = "";
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

describe("WorkspacePdfPreview", () => {
  it("loads through PDF.js and exposes lazy viewer zoom controls in its shadow root", async () => {
    await act(async () => {
      root.render(
        <WorkspacePdfPreview url="wuu-file://local/document" title="document.pdf" />,
      );
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(pdfMocks.globalWorkerOptions.workerSrc).toBe("pdf-worker.mjs");
    expect(pdfMocks.getDocument).toHaveBeenCalledWith(
      expect.objectContaining({
        url: "wuu-file://local/document",
        isEvalSupported: false,
        rangeChunkSize: 256 * 1024,
      }),
    );
    expect(pdfMocks.viewer.setDocument).toHaveBeenCalledWith({ numPages: 3 });

    await act(async () => {
      pdfMocks.eventBus?.dispatch("pagesinit", {});
      pdfMocks.eventBus?.dispatch("scalechanging", { scale: 1.5 });
    });

    const host = container.querySelector<HTMLElement>("[data-workspace-pdf-preview]");
    const shadowRoot = host?.shadowRoot;
    expect(shadowRoot?.textContent).toContain("1 / 3");
    expect(shadowRoot?.textContent).toContain("150%");
    expect(pdfMocks.viewer.currentScaleValue).toBe("page-width");

    const zoomIn = shadowRoot?.querySelector<HTMLButtonElement>(
      'button[aria-label="imagePreview.zoomIn"]',
    );
    act(() => zoomIn?.click());
    expect(pdfMocks.viewer.increaseScale).toHaveBeenCalledOnce();
  });

  it("destroys PDF.js resources when the preview closes", async () => {
    await act(async () => {
      root.render(
        <WorkspacePdfPreview url="wuu-file://local/document" title="document.pdf" />,
      );
      await Promise.resolve();
    });

    act(() => root.unmount());
    root = createRoot(container);

    expect(pdfMocks.viewer.cleanup).toHaveBeenCalledOnce();
    expect(pdfMocks.loadingTask.destroy).toHaveBeenCalledOnce();
  });
});
