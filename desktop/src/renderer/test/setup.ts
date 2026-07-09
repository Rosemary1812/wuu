(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

if (typeof globalThis.matchMedia !== "function") {
  Object.defineProperty(globalThis, "matchMedia", {
    writable: true,
    value: (query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }),
  });
}

if (typeof document.queryCommandSupported !== "function") {
  Object.defineProperty(document, "queryCommandSupported", {
    configurable: true,
    value: () => false,
  });
}

if (typeof globalThis.CSS !== "object" || globalThis.CSS === null) {
  Object.defineProperty(globalThis, "CSS", {
    configurable: true,
    value: {},
  });
}

if (typeof globalThis.CSS.escape !== "function") {
  Object.defineProperty(globalThis.CSS, "escape", {
    configurable: true,
    value: (value: string) => String(value).replace(/[^a-zA-Z0-9_-]/g, "\\$&"),
  });
}

const clipboard = (navigator as unknown as { clipboard?: Record<string, unknown> })
  .clipboard ?? {};
if (typeof clipboard.write !== "function") {
  clipboard.write = (items: Array<{ items?: Record<string, unknown> }>) => {
    for (const item of items ?? []) {
      for (const value of Object.values(item.items ?? {})) {
        void Promise.resolve(value).catch(() => undefined);
      }
    }
    return Promise.resolve();
  };
}
if (typeof clipboard.writeText !== "function") {
  clipboard.writeText = () => Promise.resolve();
}
if (typeof clipboard.readText !== "function") {
  clipboard.readText = () => Promise.resolve("");
}
Object.defineProperty(navigator, "clipboard", {
  configurable: true,
  value: clipboard,
});

if (typeof globalThis.ClipboardItem !== "function") {
  Object.defineProperty(globalThis, "ClipboardItem", {
    configurable: true,
    value: class ClipboardItem {
      readonly items: Record<string, unknown>;

      constructor(items: Record<string, unknown>) {
        this.items = items;
      }
    },
  });
}
