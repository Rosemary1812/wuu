import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync } from "node:fs";
import { mkdir, rename, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

export type CUAFrameMetadata = {
  event: string;
  revision?: number;
  width?: number;
  height?: number;
  mime_type?: string;
  message?: string;
};

export class CUAFrameDecoder {
  private buffer = Buffer.alloc(0);

  push(chunk: Buffer): Array<{ metadata: CUAFrameMetadata; payload: Buffer }> {
    this.buffer = Buffer.concat([this.buffer, chunk]);
    const frames: Array<{ metadata: CUAFrameMetadata; payload: Buffer }> = [];
    while (this.buffer.length >= 8) {
      const metadataLength = this.buffer.readUInt32BE(0);
      const payloadLength = this.buffer.readUInt32BE(4);
      if (metadataLength > 1024 * 1024 || payloadLength > 16 * 1024 * 1024) {
        throw new Error("invalid CUA frame envelope size");
      }
      const total = 8 + metadataLength + payloadLength;
      if (this.buffer.length < total) break;
      const metadata = JSON.parse(this.buffer.subarray(8, 8 + metadataLength).toString("utf8")) as CUAFrameMetadata;
      const payload = Buffer.from(this.buffer.subarray(8 + metadataLength, total));
      this.buffer = this.buffer.subarray(total);
      frames.push({ metadata, payload });
    }
    return frames;
  }
}

type FrameCallback = (path: string, metadata: CUAFrameMetadata) => void;
type ErrorCallback = (message: string) => void;

export class CUAFrameStream {
  private child: ChildProcessWithoutNullStreams | undefined;
  private latest: { payload: Buffer; metadata: CUAFrameMetadata } | undefined;
  private writing = false;
  private readonly framePath: string;

  constructor(
    private readonly helper: string,
    private readonly activityID: string,
    private readonly target: string,
    private readonly onFrame: FrameCallback,
    private readonly onError: ErrorCallback,
    private readonly onUserInput: () => void = () => undefined,
  ) {
    const safeID = createHash("sha256").update(activityID).digest("hex").slice(0, 24);
    this.framePath = join(tmpdir(), "wuu-cua-live", `${safeID}.jpg`);
  }

  start(): void {
    if (this.child) return;
    const decoder = new CUAFrameDecoder();
    const child = spawn(this.helper, ["--stream", this.target], { stdio: ["pipe", "pipe", "pipe"] });
    child.stdin.end();
    this.child = child;
    child.stdout.on("data", (chunk: Buffer) => {
      try {
        for (const frame of decoder.push(chunk)) {
          if (frame.metadata.event === "frame" && frame.payload.length > 0) {
            this.latest = frame;
            void this.flushLatest();
          } else if (frame.metadata.event === "error") {
            this.onError(frame.metadata.message ?? "live capture stopped");
          } else if (frame.metadata.event === "user_input") {
            this.onUserInput();
          }
        }
      } catch (error) {
        this.onError(error instanceof Error ? error.message : String(error));
        this.stop();
      }
    });
    let stderr = "";
    child.stderr.setEncoding("utf8");
    child.stderr.on("data", (chunk: string) => { stderr = (stderr + chunk).slice(-4096); });
    child.on("error", (error) => this.onError(error.message));
    child.on("exit", (code) => {
      if (this.child !== child) return;
      this.child = undefined;
      if (code && code !== 0) this.onError(stderr.trim() || `live capture exited with code ${code}`);
    });
  }

  stop(): void {
    const child = this.child;
    this.child = undefined;
    if (child && !child.killed) child.kill("SIGTERM");
    this.latest = undefined;
  }

  isLive(): boolean {
    return this.child !== undefined;
  }

  private async flushLatest(): Promise<void> {
    if (this.writing) return;
    this.writing = true;
    try {
      await mkdir(join(tmpdir(), "wuu-cua-live"), { recursive: true });
      while (this.latest) {
        const frame = this.latest;
        this.latest = undefined;
        const temporary = `${this.framePath}.${process.pid}.tmp`;
        await writeFile(temporary, frame.payload, { mode: 0o600 });
        await rename(temporary, this.framePath);
        this.onFrame(this.framePath, frame.metadata);
      }
    } catch (error) {
      this.onError(error instanceof Error ? error.message : String(error));
    } finally {
      this.writing = false;
      if (this.latest) void this.flushLatest();
    }
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
