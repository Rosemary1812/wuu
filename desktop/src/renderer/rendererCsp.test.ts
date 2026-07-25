import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const rendererHtml = readFileSync(resolve(__dirname, "../../index.html"), "utf-8");

describe("renderer content security policy", () => {
  it("allows PDF.js to fetch documents through the renderable file scheme", () => {
    expect(rendererHtml).toMatch(/connect-src\s+'self'\s+wuu-file:/);
  });
});
