import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { existsSync } from "node:fs";
import { join, resolve } from "node:path";
import type { Rectangle } from "electron";

export type CUANativePiPEvent = {
  event: "ready" | "user_close" | "user_input" | "capture_status" | "geometry";
  x?: number;
  y?: number;
  width?: number;
  height?: number;
  status?: "healthy" | "idle" | "blank" | "suspended" | "stopped" | "error";
  message?: string;
};

export class CUALineDecoder {
  private buffer = "";

  push(chunk: Buffer | string): CUANativePiPEvent[] {
    this.buffer += chunk.toString();
    const lines = this.buffer.split("\n");
    this.buffer = lines.pop() ?? "";
    return lines
      .map((line) => line.trim())
      .filter(Boolean)
      .map((line) => JSON.parse(line) as CUANativePiPEvent);
  }
}

type Interaction = {
  kind: "click" | "drag" | "scroll" | "type" | "observe";
  x: number;
  y: number;
  to_x?: number;
  to_y?: number;
  revision: number;
};

export class CUANativePiP {
  private child: ChildProcessWithoutNullStreams | undefined;

  constructor(
    private readonly helper: string,
    private readonly threadID: string,
    private readonly target: string,
    private readonly processID: number | undefined,
    private readonly windowID: number | undefined,
    private readonly initialBounds: Rectangle,
    private readonly onEvent: (event: CUANativePiPEvent) => void,
    private readonly onError: (message: string) => void,
  ) {}

  start(): void {
    if (this.child) return;
    const decoder = new CUALineDecoder();
    const { x, y, width, height } = this.initialBounds;
    const child = spawn(this.helper, [
      "--native-pip",
      this.threadID,
      this.target,
      String(x),
      String(y),
      String(width),
      String(height),
      String(this.processID ?? 0),
      String(this.windowID ?? 0),
      String(process.pid),
    ], { stdio: ["pipe", "pipe", "pipe"] });
    this.child = child;
    child.stdout.on("data", (chunk: Buffer) => {
      try {
        for (const event of decoder.push(chunk)) this.onEvent(event);
      } catch (error) {
        this.onError(error instanceof Error ? error.message : String(error));
        this.stop();
      }
    });
    let stderr = "";
    child.stderr.setEncoding("utf8");
    child.stderr.on("data", (chunk: string) => { stderr = (stderr + chunk).slice(-4096); });
    child.on("error", (error) => this.onError(error.message));
    child.on("exit", (code, signal) => {
      if (this.child !== child) return;
      // Reaching here means the helper died without us asking: a deliberate
      // stop() clears this.child first, so it is filtered out above. Report every
      // unexpected exit — including a clean code 0 (e.g. stdin EOF) — so the
      // coordinator drops the dead entry and can recover, instead of holding a
      // reference to a vanished window forever.
      this.child = undefined;
      this.onError(stderr.trim() || `native PiP exited (${signal ?? `code ${code ?? 0}`})`);
    });
  }

  setVisible(visible: boolean): void {
    this.send({ type: "visible", visible });
  }

  animateInteraction(interaction?: Interaction): void {
    if (!interaction || interaction.kind === "observe") return;
    this.send({ type: "interaction", ...interaction });
  }

  stop(): void {
    const child = this.child;
    this.child = undefined;
    if (!child) return;
    if (!child.stdin.destroyed) child.stdin.write(`${JSON.stringify({ type: "close" })}\n`);
    child.kill("SIGTERM");
  }

  isLive(): boolean { return this.child !== undefined; }

  private send(command: object): void {
    if (!this.child || this.child.stdin.destroyed) return;
    this.child.stdin.write(`${JSON.stringify(command)}\n`);
  }
}

export function resolveCUAFrameHelper(): string | undefined {
  if (process.platform !== "darwin") return undefined;
  const resourcesPath = (process as { resourcesPath?: string }).resourcesPath;
  const roots = [process.env.WUU_SOURCE_ROOT, process.cwd(), resolve(process.cwd(), "..")]
    .filter((value): value is string => Boolean(value));
  const candidates = [
    process.env.WUU_CUA_MAC_HELPER,
    resourcesPath ? join(resourcesPath, "bin", "wuu-cua-mac") : undefined,
    ...roots.map((root) => join(root, "desktop", "build", "bin", "wuu-cua-mac")),
  ].filter((value): value is string => Boolean(value));
  return candidates.find(existsSync);
}
