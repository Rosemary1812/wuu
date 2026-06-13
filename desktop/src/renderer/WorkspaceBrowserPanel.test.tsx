import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { WorkspaceBrowserPanel } from "./WorkspaceBrowserPanel";

type FakeWebviewMethods = {
  loadURL: ReturnType<typeof vi.fn>;
  getURL: ReturnType<typeof vi.fn>;
  getTitle: ReturnType<typeof vi.fn>;
  canGoBack: ReturnType<typeof vi.fn>;
  canGoForward: ReturnType<typeof vi.fn>;
  goBack: ReturnType<typeof vi.fn>;
  goForward: ReturnType<typeof vi.fn>;
  reload: ReturnType<typeof vi.fn>;
  stop: ReturnType<typeof vi.fn>;
};

type FakeWebview = HTMLElement & FakeWebviewMethods;

function makeFakeWebview(): FakeWebview {
  // jsdom does not know the Electron <webview> custom element. We
  // create a plain div and bolt the webview-only API on top so the
  // component's imperative mount path keeps working under jsdom.
  const el = document.createElement("div");
  const methods: FakeWebviewMethods = {
    loadURL: vi.fn(),
    getURL: vi.fn(() => ""),
    getTitle: vi.fn(() => ""),
    canGoBack: vi.fn(() => false),
    canGoForward: vi.fn(() => false),
    goBack: vi.fn(),
    goForward: vi.fn(),
    reload: vi.fn(),
    stop: vi.fn()
  };
  return Object.assign(el, methods);
}

let container: HTMLDivElement;
let root: Root | null = null;
let fakeWebview: FakeWebview;
let originalCreateElement: typeof document.createElement;

beforeAll(() => {
  (globalThis as { Electron?: unknown }).Electron = {
    WebviewTag: class WebviewTag {}
  };
});

beforeEach(() => {
  fakeWebview = makeFakeWebview();
  originalCreateElement = document.createElement.bind(document);
  document.createElement = ((tag: string, options?: ElementCreationOptions) => {
    if (String(tag).toLowerCase() === "webview") {
      return fakeWebview as unknown as HTMLElement;
    }
    return originalCreateElement(tag, options);
  }) as typeof document.createElement;
  container = document.createElement("div");
  document.body.appendChild(container);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  root = null;
  if (container.parentNode) {
    container.remove();
  }
  document.createElement = originalCreateElement;
  vi.restoreAllMocks();
});

function render(props: {
  pendingBrowserURL?: string;
  onBrowserURLConsumed?: () => void;
  onCurrentURLChange?: (url: string) => void;
}) {
  act(() => {
    root = createRoot(container);
    root!.render(
      <WorkspaceBrowserPanel
        activeContext={{ kind: "no_project", cwd: "/repo" }}
        pendingBrowserURL={props.pendingBrowserURL}
        onBrowserURLConsumed={props.onBrowserURLConsumed}
        onCurrentURLChange={props.onCurrentURLChange}
      />
    );
  });
  return {
    input: container.querySelector(".workspace-browser-url-input") as HTMLInputElement | null
  };
}

function rerender(props: {
  pendingBrowserURL?: string;
  onBrowserURLConsumed?: () => void;
  onCurrentURLChange?: (url: string) => void;
}) {
  act(() => {
    root!.render(
      <WorkspaceBrowserPanel
        activeContext={{ kind: "no_project", cwd: "/repo" }}
        pendingBrowserURL={props.pendingBrowserURL}
        onBrowserURLConsumed={props.onBrowserURLConsumed}
        onCurrentURLChange={props.onCurrentURLChange}
      />
    );
  });
}

describe("WorkspaceBrowserPanel pendingBrowserURL", () => {
  it("navigates to the URL the parent requested", () => {
    const consumed = vi.fn();
    render({ pendingBrowserURL: "http://localhost:3000", onBrowserURLConsumed: consumed });
    expect(fakeWebview.loadURL).toHaveBeenCalledWith("http://localhost:3000");
    expect(consumed).toHaveBeenCalledTimes(1);
  });

  it("surfaces the navigated URL in the address bar", () => {
    const { input } = render({});
    rerender({ pendingBrowserURL: "http://localhost:5173" });
    expect(input?.value).toBe("http://localhost:5173");
  });

  it("reports webview navigation URLs to the parent", () => {
    const changed = vi.fn();
    render({ onCurrentURLChange: changed });
    act(() => {
      fakeWebview.dispatchEvent(Object.assign(new Event("did-navigate"), { url: "http://localhost:5173" }));
    });
    expect(changed).toHaveBeenCalledWith("http://localhost:5173");
  });

  it("does not navigate again when the same URL is re-asserted", () => {
    const consumed = vi.fn();
    const { input } = render({
      pendingBrowserURL: "http://localhost:3000",
      onBrowserURLConsumed: consumed
    });
    expect(consumed).toHaveBeenCalledTimes(1);
    // The first mount navigates and immediately consumes the prop. A
    // re-render with the same prop value should not re-issue
    // loadURL, otherwise the agent's repeat reports would churn the
    // webview.
    fakeWebview.loadURL.mockClear();
    rerender({
      pendingBrowserURL: "http://localhost:3000",
      onBrowserURLConsumed: consumed
    });
    expect(fakeWebview.loadURL).not.toHaveBeenCalled();
    void input;
  });
});
