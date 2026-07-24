import type { ChildProcessWithoutNullStreams } from "node:child_process";
import { spawn } from "node:child_process";
import { join } from "node:path";
import type {
  SpeechRecognitionEvent,
  SpeechRecognitionStartResult,
} from "../shared/protocol";

type SpeechProcess = Pick<
  ChildProcessWithoutNullStreams,
  "stdout" | "stderr" | "stdin" | "kill" | "once"
>;

type SpeechRecognitionDeps = {
  platform: NodeJS.Platform;
  resourcesPath: string;
  helperPath?: string;
  askForMicrophoneAccess: () => Promise<boolean>;
  spawnHelper: (path: string, args: string[]) => SpeechProcess;
};

export class SpeechRecognitionService {
  private process: SpeechProcess | undefined;
  private sessionID = 0;
  private owner: ((event: SpeechRecognitionEvent) => void) | undefined;

  constructor(private readonly deps: SpeechRecognitionDeps) {}

  async start(
    locale: string,
    emit: (event: SpeechRecognitionEvent) => void,
  ): Promise<SpeechRecognitionStartResult> {
    if (this.deps.platform !== "darwin") {
      return { ok: false, error: "platform_unsupported" };
    }
    this.stop();
    emit({ type: "state", state: "requesting_microphone_permission" });
    if (!(await this.deps.askForMicrophoneAccess())) {
      return { ok: false, error: "microphone_permission_denied" };
    }

    const helperPath =
      this.deps.helperPath ||
      join(this.deps.resourcesPath, "bin", "wuu-speech-mac");
    const child = this.deps.spawnHelper(helperPath, ["--locale", locale]);
    const sessionID = String(++this.sessionID);
    this.process = child;
    this.owner = emit;
    let stdoutBuffer = "";
    let stderr = "";
    let protocolErrorReported = false;

    child.stdout.setEncoding("utf8");
    child.stderr.setEncoding("utf8");
    child.stdout.on("data", (chunk: string) => {
      stdoutBuffer += chunk;
      const lines = stdoutBuffer.split("\n");
      stdoutBuffer = lines.pop() ?? "";
      for (const line of lines) {
        const event = parseSpeechEvent(line);
        if (event && this.process === child) {
          if (event.type === "error") {
            protocolErrorReported = true;
          }
          emit(event);
        }
      }
    });
    child.stderr.on("data", (chunk: string) => {
      stderr = `${stderr}${chunk}`.slice(-2000);
    });
    child.once("error", (error) => {
      if (this.process !== child) return;
      this.clear(child);
      emit({
        type: "error",
        code: "helper_unavailable",
        message: error.message,
      });
    });
    child.once("exit", (code) => {
      if (this.process !== child) return;
      this.clear(child);
      if (code && code !== 0 && !protocolErrorReported) {
        emit({
          type: "error",
          code: "recognition_process_failed",
          message: stderr.trim() || `Speech recognition exited with code ${code}.`,
        });
      }
    });
    return { ok: true, session_id: sessionID };
  }

  stop(): void {
    const child = this.process;
    if (!child) return;
    this.clear(child);
    const owner = this.owner;
    this.owner = undefined;
    try {
      child.stdin.write("\n");
      child.stdin.end();
    } catch {
      child.kill();
    }
    owner?.({ type: "state", state: "stopped" });
  }

  private clear(child: SpeechProcess): void {
    if (this.process === child) {
      this.process = undefined;
    }
  }
}

export function createSpeechRecognitionService({
  platform = process.platform,
  resourcesPath = process.resourcesPath,
  helperPath = process.env.WUU_SPEECH_MAC_HELPER,
  askForMicrophoneAccess,
}: {
  platform?: NodeJS.Platform;
  resourcesPath?: string;
  helperPath?: string;
  askForMicrophoneAccess: () => Promise<boolean>;
}): SpeechRecognitionService {
  return new SpeechRecognitionService({
    platform,
    resourcesPath,
    helperPath,
    askForMicrophoneAccess,
    spawnHelper: (path, args) =>
      spawn(path, args, { stdio: ["pipe", "pipe", "pipe"] }),
  });
}

function parseSpeechEvent(line: string): SpeechRecognitionEvent | undefined {
  try {
    const value = JSON.parse(line) as SpeechRecognitionEvent;
    if (
      value.type === "state" ||
      value.type === "result" ||
      value.type === "error"
    ) {
      return value;
    }
  } catch {
    // Ignore non-protocol helper output.
  }
  return undefined;
}
