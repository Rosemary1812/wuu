import { linkSync, mkdtempSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { CUALineDecoder, cuaFrameHelperCandidates, isSameExecutableFile } from "./cuaFrameStreams";

describe("CUALineDecoder", () => {
  it("decodes fragmented native PiP events", () => {
    const decoder = new CUALineDecoder();
    expect(decoder.push('{"event":"rea')).toEqual([]);
    expect(decoder.push('dy","width":320,"height":240}\n{"event":"user_input"}\n')).toEqual([
      { event: "ready", width: 320, height: 240 },
      { event: "user_input" },
    ]);
  });

  it("keeps an incomplete final line for the next chunk", () => {
    const decoder = new CUALineDecoder();
    expect(decoder.push('{"event":"capture_status","status":"idle"}')).toEqual([]);
    expect(decoder.push("\n")).toEqual([{ event: "capture_status", status: "idle" }]);
  });

  it("decodes user-resized PiP geometry", () => {
    const decoder = new CUALineDecoder();
    expect(decoder.push('{"event":"geometry","x":20,"y":30,"width":520,"height":300}\n')).toEqual([
      { event: "geometry", x: 20, y: 30, width: 520, height: 300 },
    ]);
  });
});

describe("cuaFrameHelperCandidates", () => {
  it("keeps the PiP on a physical sibling path instead of the MCP executable", () => {
    expect(cuaFrameHelperCandidates(
      { WUU_CUA_MAC_HELPER: "/app/bin/wuu-cua-mac" },
      "/app",
      ["/repo"],
    )).toEqual([
      "/app/bin/wuu-cua-mac-pip",
      "/repo/desktop/build/bin/wuu-cua-mac-pip",
    ]);
  });

  it("rejects an override that points PiP back at the MCP executable", () => {
    expect(cuaFrameHelperCandidates(
      {
        WUU_CUA_MAC_HELPER: "/app/bin/wuu-cua-mac",
        WUU_CUA_MAC_PIP_HELPER: "/app/bin/wuu-cua-mac",
      },
      undefined,
      [],
    )).toEqual(["/app/bin/wuu-cua-mac-pip"]);
  });

  it("detects aliases that resolve to the same executable file", () => {
    const root = mkdtempSync(join(tmpdir(), "wuu-cua-helper-"));
    try {
      const mcp = join(root, "wuu-cua-mac");
      const symlink = join(root, "wuu-cua-mac-pip-symlink");
      const hardlink = join(root, "wuu-cua-mac-pip-hardlink");
      const copy = join(root, "wuu-cua-mac-pip-copy");
      writeFileSync(mcp, "signed helper bytes");
      symlinkSync(mcp, symlink);
      linkSync(mcp, hardlink);
      writeFileSync(copy, "signed helper bytes");

      expect(isSameExecutableFile(symlink, mcp)).toBe(true);
      expect(isSameExecutableFile(hardlink, mcp)).toBe(true);
      expect(isSameExecutableFile(copy, mcp)).toBe(false);
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });
});
