import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { RichContent } from "./RichContent";

let container: HTMLDivElement;
let root: Root | null = null;
let writeTextMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  writeTextMock = vi.fn().mockResolvedValue(undefined);
  // jsdom does not implement the clipboard API; inject a mock for the
  // success-path tests.
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: { writeText: writeTextMock }
  });
  container = document.createElement("div");
  document.body.appendChild(container);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  root = null;
  container.remove();
});

function render(element: JSX.Element): void {
  act(() => {
    if (!root) {
      root = createRoot(container);
    }
    root.render(element);
  });
}

describe("RichContent code block", () => {
  it("wraps fenced code in a header with the language label and a copy button", () => {
    render(<RichContent text={"```typescript\nconst x = 1;\n```"} />);

    const block = container.querySelector(".rich-code-block");
    expect(block).not.toBeNull();

    const language = block?.querySelector(".rich-code-language");
    expect(language?.textContent).toBe("typescript");

    const copyButton = block?.querySelector(".rich-code-copy");
    expect(copyButton).not.toBeNull();
    // The button should advertise itself as a code copy, not the
    // generic "复制消息" label that the message-level copy uses.
    expect(copyButton?.getAttribute("aria-label")).toBe("复制代码");
  });

  it("omits the language label when the fenced code has no language", () => {
    render(<RichContent text={"```\nnaked code\n```"} />);

    const block = container.querySelector(".rich-code-block");
    expect(block).not.toBeNull();
    expect(block?.querySelector(".rich-code-language")).toBeNull();
    expect(block?.querySelector(".rich-code-copy")).not.toBeNull();
  });

  it("clicking the copy button writes the code text to the clipboard", async () => {
    render(<RichContent text={"```js\nconsole.log('hi');\n```"} />);

    const copyButton = container.querySelector(".rich-code-copy") as HTMLButtonElement | null;
    expect(copyButton).not.toBeNull();

    await act(async () => {
      copyButton?.click();
    });

    expect(writeTextMock).toHaveBeenCalledTimes(1);
    expect(writeTextMock).toHaveBeenCalledWith("console.log('hi');");
  });
});
