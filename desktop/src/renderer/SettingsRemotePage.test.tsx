/**
 * Tests for `SettingsRemotePage`.
 *
 * Contract: relay input drafts from status and saves via onSaveRelay; the
 * remote-access switch reflects hostRunning and is gated on a configured
 * relay; the pairing card shows the QR (SVG) + copyable URI while a pairing
 * window is open, otherwise a button gated on the host running; paired
 * devices list fingerprint/name with a revoke action; empty and error
 * states render.
 */
import { afterEach, describe, expect, it } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";

import { SettingsRemotePage, type RemoteStatusView, type SettingsRemotePageProps } from "./SettingsRemotePage";

let container: HTMLDivElement | null = null;
let root: Root | null = null;

function mount(props: SettingsRemotePageProps): void {
  if (container) unmount();
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  act(() => {
    root!.render(<SettingsRemotePage {...props} />);
  });
}

function unmount(): void {
  if (root) {
    act(() => root!.unmount());
    root = null;
  }
  container?.remove();
  container = null;
}

afterEach(unmount);

const baseStatus: RemoteStatusView = {
  fingerprint: "3a5ec62add99",
  host_name: "studio",
  relay_url: "ws://127.0.0.1:8787/v1/connect",
  store: "/home/user/.wuu/remote.json",
  devices: []
};

function baseProps(overrides: Partial<SettingsRemotePageProps> = {}): SettingsRemotePageProps {
  return {
    status: baseStatus,
    statusError: "",
    hostRunning: false,
    pairUri: null,
    busy: false,
    onSaveRelay: () => {},
    onToggleHost: () => {},
    onOpenPairing: () => {},
    onRemoveDevice: () => {},
    ...overrides
  };
}

async function flushAsync(): Promise<void> {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

describe("SettingsRemotePage relay + switch", () => {
  it("drafts the relay from status and saves trimmed edits", () => {
    const saved: string[] = [];
    mount(baseProps({ onSaveRelay: (url) => saved.push(url) }));
    const input = container!.querySelector<HTMLInputElement>(".settings-input")!;
    expect(input.value).toBe("ws://127.0.0.1:8787/v1/connect");

    act(() => {
      const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
      setter.call(input, "  ws://relay.example.com/v1/connect  ");
      input.dispatchEvent(new Event("input", { bubbles: true }));
    });
    const form = container!.querySelector("form")!;
    act(() => {
      form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    });
    expect(saved).toEqual(["ws://relay.example.com/v1/connect"]);
  });

  it("gates the remote-access switch on a configured relay", () => {
    mount(baseProps({ status: { ...baseStatus, relay_url: undefined } }));
    const toggle = container!.querySelector<HTMLButtonElement>(".settings-switch")!;
    expect(toggle.disabled).toBe(true);
    expect(container!.textContent).toContain("先配置中继地址");
  });

  it("toggles the host through onToggleHost", () => {
    const toggles: boolean[] = [];
    mount(baseProps({ hostRunning: true, onToggleHost: (enabled) => toggles.push(enabled) }));
    const toggle = container!.querySelector<HTMLButtonElement>(".settings-switch")!;
    expect(toggle.getAttribute("aria-checked")).toBe("true");
    act(() => toggle.click());
    expect(toggles).toEqual([false]);
  });
});

describe("SettingsRemotePage pairing", () => {
  it("gates the pairing button on a running host", () => {
    let opened = 0;
    mount(baseProps({ onOpenPairing: () => opened++ }));
    const button = [...container!.querySelectorAll<HTMLButtonElement>("button")].find(
      (b) => b.textContent === "显示配对二维码"
    )!;
    expect(button.disabled).toBe(true);

    mount(baseProps({ hostRunning: true, onOpenPairing: () => opened++ }));
    const enabled = [...container!.querySelectorAll<HTMLButtonElement>("button")].find(
      (b) => b.textContent === "显示配对二维码"
    )!;
    expect(enabled.disabled).toBe(false);
    act(() => enabled.click());
    expect(opened).toBe(1);
  });

  it("renders the QR svg and the copyable URI while pairing", async () => {
    const uri = "wuu://pair?h=HOST&k=KEY&p=PID&r=ws%3A%2F%2Frelay&v=1";
    mount(baseProps({ hostRunning: true, pairUri: uri }));
    expect(container!.querySelector('[data-testid="remote-pair-uri"]')!.textContent).toBe(uri);
    await flushAsync();
    const qr = container!.querySelector('[data-testid="remote-pair-qr"]')!;
    expect(qr.innerHTML).toContain("<svg");
  });
});

describe("SettingsRemotePage devices", () => {
  it("shows the empty state", () => {
    mount(baseProps());
    expect(container!.textContent).toContain("尚未配对任何手机");
  });

  it("lists devices and revokes through onRemoveDevice", () => {
    const removed: string[] = [];
    mount(
      baseProps({
        status: {
          ...baseStatus,
          devices: [
            { pub: "PUB1", fingerprint: "2579e6ff1255", name: "我的手机", added_at: "2026-07-07T00:00:00Z" },
            { pub: "PUB2", fingerprint: "c8465106f505", name: "", added_at: "not-a-date" }
          ]
        },
        onRemoveDevice: (device) => removed.push(device.pub)
      })
    );
    expect(container!.textContent).toContain("我的手机");
    // Format the fixture instant the same way the page does so the expected
    // date follows the runtime timezone instead of hard-coding one zone's day.
    const pairedAt = new Intl.DateTimeFormat("zh-CN", {
      year: "numeric",
      month: "short",
      day: "numeric"
    }).format(new Date("2026-07-07T00:00:00Z"));
    expect(container!.textContent).toContain(`2579e6ff1255 · 配对于 ${pairedAt}`);
    expect(container!.textContent).toContain("未命名设备");
    expect(container!.textContent).toContain("not-a-date");
    const revoke = [...container!.querySelectorAll<HTMLButtonElement>("button")].filter(
      (b) => b.textContent === "吊销"
    );
    expect(revoke).toHaveLength(2);
    act(() => revoke[0].click());
    expect(removed).toEqual(["PUB1"]);
  });

  it("surfaces status errors", () => {
    mount(baseProps({ status: null, statusError: "wuu remote status failed" }));
    expect(container!.querySelector(".settings-error")!.textContent).toBe("wuu remote status failed");
  });
});
