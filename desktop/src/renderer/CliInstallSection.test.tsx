import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { CliInstallSection } from "./CliInstallSection";
import type { CliInstallResult, CliInstallStatus, WuuDesktopApi } from "../shared/protocol";

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
  delete (globalThis as { wuu?: WuuDesktopApi }).wuu;
  delete (window as { wuu?: WuuDesktopApi }).wuu;
});

function baseStatus(overrides: Partial<CliInstallStatus> = {}): CliInstallStatus {
  return {
    platform_supported: true,
    platform: "darwin",
    source_path: "/Applications/wuu.app/Contents/Resources/bin/wuu",
    install_dir: "/home/u/.local/bin",
    install_path: "/home/u/.local/bin/wuu",
    installed: false,
    linked_to_source: false,
    link_dangling: false,
    foreign_install: false,
    on_path: true,
    auto_install_enabled: true,
    last_auto_install: null,
    ...overrides,
  };
}

function installStub(stub: Partial<WuuDesktopApi>): void {
  (globalThis as { wuu?: WuuDesktopApi }).wuu = stub as WuuDesktopApi;
  (window as { wuu?: WuuDesktopApi }).wuu = stub as WuuDesktopApi;
}

async function renderSection(): Promise<void> {
  await act(async () => {
    root = createRoot(container);
    root.render(<CliInstallSection />);
  });
  // Flush the mount-time status fetch.
  await act(async () => {
    await Promise.resolve();
  });
}

async function clickButtonByText(text: string): Promise<void> {
  const button = [...container.querySelectorAll("button")].find(
    (candidate) => candidate.textContent?.trim() === text,
  );
  if (!button) {
    throw new Error(`button not found: ${text}`);
  }
  await act(async () => {
    button.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await Promise.resolve();
  });
  await act(async () => {
    await Promise.resolve();
  });
}

describe("CliInstallSection", () => {
  it("renders the install path and an install button when supported", async () => {
    installStub({
      getCliInstallStatus: vi.fn().mockResolvedValue(baseStatus()),
      installCli: vi.fn(),
    });
    await renderSection();
    expect(container.textContent).toContain("/home/u/.local/bin/wuu");
    expect(container.querySelector("[data-testid='settings-cli-install']")).not.toBeNull();
    // On PATH → no hint.
    expect(container.querySelector("[data-testid='cli-install-path-hint']")).toBeNull();
  });

  it("shows a PATH hint when ~/.local/bin is not on PATH", async () => {
    installStub({
      getCliInstallStatus: vi.fn().mockResolvedValue(baseStatus({ on_path: false })),
      installCli: vi.fn(),
    });
    await renderSection();
    expect(container.querySelector("[data-testid='cli-install-path-hint']")).not.toBeNull();
    expect(container.textContent).toContain("不在 PATH 中");
  });

  it("shows an unsupported message on Windows", async () => {
    installStub({
      getCliInstallStatus: vi
        .fn()
        .mockResolvedValue(baseStatus({ platform_supported: false, platform: "win32" })),
      installCli: vi.fn(),
    });
    await renderSection();
    expect(container.textContent).toContain("暂不支持");
    expect(
      [...container.querySelectorAll("button")].some((b) => b.textContent?.includes("安装")),
    ).toBe(false);
  });

  it("installs and shows success feedback", async () => {
    const installCli = vi.fn().mockResolvedValue({
      ok: true,
      install_path: "/home/u/.local/bin/wuu",
      on_path: true,
    } satisfies CliInstallResult);
    installStub({
      getCliInstallStatus: vi.fn().mockResolvedValue(baseStatus()),
      installCli,
    });
    await renderSection();
    await clickButtonByText("安装");
    expect(installCli).toHaveBeenCalledWith(false);
    expect(container.textContent).toContain("已安装到 ~/.local/bin/wuu");
  });

  it("prompts to overwrite and installs with overwrite=true", async () => {
    const installCli = vi
      .fn()
      .mockResolvedValueOnce({
        ok: false,
        needs_overwrite: true,
        install_path: "/home/u/.local/bin/wuu",
        on_path: true,
      } satisfies CliInstallResult)
      .mockResolvedValueOnce({
        ok: true,
        install_path: "/home/u/.local/bin/wuu",
        on_path: true,
      } satisfies CliInstallResult);
    installStub({
      getCliInstallStatus: vi.fn().mockResolvedValue(baseStatus()),
      installCli,
    });
    await renderSection();

    await clickButtonByText("安装");
    expect(container.querySelector("[data-testid='cli-install-overwrite']")).not.toBeNull();

    await clickButtonByText("覆盖");
    expect(installCli).toHaveBeenNthCalledWith(2, true);
    expect(container.textContent).toContain("已安装");
  });

  it("renders the auto-install toggle on and persists turning it off", async () => {
    const setCliAutoInstallEnabled = vi
      .fn()
      .mockResolvedValue({ ok: true, enabled: false });
    installStub({
      getCliInstallStatus: vi.fn().mockResolvedValue(baseStatus()),
      installCli: vi.fn(),
      setCliAutoInstallEnabled,
    });
    await renderSection();

    const toggle = container.querySelector("[data-testid='cli-auto-install-switch']");
    expect(toggle).not.toBeNull();
    expect(toggle?.getAttribute("aria-checked")).toBe("true");

    await act(async () => {
      toggle?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      await Promise.resolve();
    });
    expect(setCliAutoInstallEnabled).toHaveBeenCalledWith(false);
    expect(toggle?.getAttribute("aria-checked")).toBe("false");
  });

  it("reflects a persisted disabled auto-install setting", async () => {
    installStub({
      getCliInstallStatus: vi
        .fn()
        .mockResolvedValue(baseStatus({ auto_install_enabled: false })),
      installCli: vi.fn(),
      setCliAutoInstallEnabled: vi.fn(),
    });
    await renderSection();
    const toggle = container.querySelector("[data-testid='cli-auto-install-switch']");
    expect(toggle?.getAttribute("aria-checked")).toBe("false");
  });

  it("shows a not-taken-over note for a foreign wuu install without prompting", async () => {
    installStub({
      getCliInstallStatus: vi.fn().mockResolvedValue(
        baseStatus({ installed: true, foreign_install: true }),
      ),
      installCli: vi.fn(),
      setCliAutoInstallEnabled: vi.fn(),
    });
    await renderSection();

    expect(container.querySelector("[data-testid='cli-install-foreign']")).not.toBeNull();
    expect(container.textContent).toContain("桌面版未接管");
    // No overwrite prompt is auto-opened; the decision stays with the
    // manual install button.
    expect(container.querySelector("[data-testid='cli-install-overwrite']")).toBeNull();
    expect(
      [...container.querySelectorAll("button")].some((b) => b.textContent?.trim() === "安装"),
    ).toBe(true);
  });

  it("shows a one-time note when startup auto-install just ran", async () => {
    installStub({
      getCliInstallStatus: vi.fn().mockResolvedValue(
        baseStatus({
          installed: true,
          linked_to_source: true,
          last_auto_install: {
            outcome: "installed",
            install_path: "/home/u/.local/bin/wuu",
            source_path: "/Applications/wuu.app/Contents/Resources/bin/wuu",
            on_path: true,
          },
        }),
      ),
      installCli: vi.fn(),
      setCliAutoInstallEnabled: vi.fn(),
    });
    await renderSection();
    expect(container.querySelector("[data-testid='cli-install-auto-note']")).not.toBeNull();
    expect(container.textContent).toContain("启动时已自动安装");
  });

  it("does not show the auto-install note when nothing was installed", async () => {
    installStub({
      getCliInstallStatus: vi.fn().mockResolvedValue(
        baseStatus({
          installed: true,
          linked_to_source: true,
          last_auto_install: {
            outcome: "already-linked",
            install_path: "/home/u/.local/bin/wuu",
            source_path: "/Applications/wuu.app/Contents/Resources/bin/wuu",
            on_path: true,
          },
        }),
      ),
      installCli: vi.fn(),
      setCliAutoInstallEnabled: vi.fn(),
    });
    await renderSection();
    expect(container.querySelector("[data-testid='cli-install-auto-note']")).toBeNull();
  });
});
