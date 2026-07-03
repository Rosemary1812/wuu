/**
 * Tests for `EmptyStateHints`.
 *
 * Contract:
 * - Renders a "配置模型" chip when no provider reports
 *   `api_key_configured === true` or `connection_locked === true`,
 *   and clicking it emits an `openSettings` action.
 * - The settings chip is suppressed when any provider is ready (key
 *   configured OR connection locked, e.g. OAuth).
 * - When the chip would be hidden the strip returns null (caller
 *   must guard with a null check).
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import {
  EmptyStateHints,
  type EmptyStateHintAction,
} from "./EmptyStateHints";
import type { ProviderSummary } from "../shared/protocol";

let container: HTMLDivElement | null = null;
let root: Root | null = null;

function mount(props: Parameters<typeof EmptyStateHints>[0]): void {
  if (container) unmount();
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  act(() => {
    root!.render(<EmptyStateHints {...props} />);
  });
}

function unmount(): void {
  if (root) {
    act(() => {
      root!.unmount();
    });
    root = null;
  }
  if (container) {
    container.remove();
    container = null;
  }
}

function hintLabels(): string[] {
  return Array.from(
    container?.querySelectorAll(".empty-home-hint-chip") ?? [],
  ).map((node) => node.textContent?.trim() ?? "");
}

function clickHint(label: string): void {
  const chip = Array.from(
    container?.querySelectorAll(".empty-home-hint-chip") ?? [],
  ).find((node) => node.textContent?.trim() === label);
  if (!chip) {
    throw new Error(`chip with label ${label} not found`);
  }
  act(() => {
    (chip as HTMLButtonElement).click();
  });
}

const providerWithKey: ProviderSummary = {
  name: "openai",
  type: "openai",
  model: "gpt-4o",
  api_key_configured: true,
};

const providerWithoutKey: ProviderSummary = {
  name: "openai",
  type: "openai",
  model: "gpt-4o",
  api_key_configured: false,
};

const providerOAuth: ProviderSummary = {
  name: "anthropic",
  type: "anthropic",
  model: "claude-3-7-sonnet",
  connection_locked: true,
};

describe("EmptyStateHints", () => {
  afterEach(() => {
    unmount();
  });

  it("renders the settings chip when no provider has a configured key", () => {
    mount({
      providers: [providerWithoutKey],
      onSelect: () => {},
    });
    expect(hintLabels()).toContain("配置模型");
  });

  it("emits openSettings when the settings chip is clicked", () => {
    const onSelect = vi.fn<(action: EmptyStateHintAction) => void>();
    mount({
      providers: [providerWithoutKey],
      onSelect,
    });
    clickHint("配置模型");
    expect(onSelect).toHaveBeenCalledWith({ kind: "openSettings" });
  });

  it("hides the settings chip when a provider has an api_key_configured", () => {
    mount({
      providers: [providerWithKey],
      onSelect: () => {},
    });
    expect(hintLabels()).not.toContain("配置模型");
  });

  it("hides the settings chip when a provider is connection_locked (OAuth)", () => {
    mount({
      providers: [providerOAuth],
      onSelect: () => {},
    });
    expect(hintLabels()).not.toContain("配置模型");
  });

  it("renders nothing when the settings chip would be hidden", () => {
    mount({
      providers: [providerWithKey],
      onSelect: () => {},
    });
    expect(container?.querySelector(".empty-home-hints")).toBeNull();
  });

  it("renders the settings chip when providers is undefined", () => {
    mount({
      providers: undefined,
      onSelect: () => {},
    });
    expect(hintLabels()).toContain("配置模型");
  });

  it("renders the settings chip when the providers list is empty", () => {
    mount({
      providers: [],
      onSelect: () => {},
    });
    expect(hintLabels()).toContain("配置模型");
  });
});
