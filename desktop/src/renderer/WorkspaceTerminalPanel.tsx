import { FitAddon } from "@xterm/addon-fit";
import { Terminal as XtermTerminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { Terminal } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import type { RuntimeContext, TerminalSessionEvent } from "../shared/protocol";
import { WorkspacePanelEmpty } from "./WorkspaceFiles";
import { desktopApiErrorMessage } from "./WorkspaceReviewHelpers";

const WORKSPACE_TERMINAL_PENDING_EVENT_IDS = 12;

type WorkspaceTerminalState = "starting" | "ready" | "exited" | "error";

function formatTerminalDuration(ms: number): string {
  const totalSeconds = Math.max(0, Math.floor(ms / 1000));
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  if (hours > 0) {
    return `${hours}h ${minutes}m ${seconds}s`;
  }
  if (minutes > 0) {
    return `${minutes}m ${seconds}s`;
  }
  return `${seconds}s`;
}

function terminalExitText(event: Extract<TerminalSessionEvent, { type: "exit" }>): string {
  const duration = formatTerminalDuration(event.duration_ms);
  if (event.signal) {
    return `stopped by ${event.signal} after ${duration}`;
  }
  return `exit ${event.exit_code ?? "unknown"} after ${duration}`;
}

export function WorkspaceTerminalPanel({ activeContext }: { activeContext?: RuntimeContext }): JSX.Element {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const terminalRef = useRef<XtermTerminal | null>(null);
  const sessionIDRef = useRef<string | undefined>(undefined);
  const pendingTerminalEventsRef = useRef(new Map<string, TerminalSessionEvent[]>());
  const [terminalState, setTerminalState] = useState<WorkspaceTerminalState>("starting");
  const [restartKey, setRestartKey] = useState(0);
  const workspaceRoot = activeContext?.cwd;

  useEffect(() => {
    const container = containerRef.current;
    if (!workspaceRoot || !container) {
      return undefined;
    }

    let disposed = false;
    let resizeFrame: number | undefined;
    setTerminalState("starting");
    pendingTerminalEventsRef.current.clear();

    const terminal = new XtermTerminal({
      allowTransparency: false,
      convertEol: false,
      cursorBlink: true,
      fontFamily: '"SFMono-Regular", Consolas, "Liberation Mono", monospace',
      fontSize: 12,
      lineHeight: 1.45,
      scrollback: 5000,
      theme: {
        background: "#ffffff",
        black: "#24292f",
        blue: "#2f98ff",
        cursor: "#202427",
        foreground: "#1f2328",
        green: "#1f9d46",
        red: "#b42318",
        selectionBackground: "#d7e9ff",
        yellow: "#ffc21a"
      }
    });
    const fitAddon = new FitAddon();
    terminal.loadAddon(fitAddon);
    terminal.open(container);
    terminal.focus();
    terminalRef.current = terminal;

    function fitAndResize(): void {
      if (disposed) {
        return;
      }
      try {
        fitAddon.fit();
      } catch {
        return;
      }
      const id = sessionIDRef.current;
      if (id) {
        void window.wuu.resizeTerminalSession(id, terminal.cols, terminal.rows);
      }
    }

    const dataDisposable = terminal.onData((data) => {
      const id = sessionIDRef.current;
      if (id) {
        void window.wuu.writeTerminalSession(id, data);
      }
    });
    const resizeObserver = new ResizeObserver(() => {
      if (resizeFrame !== undefined) {
        window.cancelAnimationFrame(resizeFrame);
      }
      resizeFrame = window.requestAnimationFrame(fitAndResize);
    });
    resizeObserver.observe(container);
    resizeFrame = window.requestAnimationFrame(fitAndResize);

    function bufferTerminalEvent(event: TerminalSessionEvent): void {
      const events = pendingTerminalEventsRef.current.get(event.id) ?? [];
      pendingTerminalEventsRef.current.set(event.id, [...events, event]);
      while (pendingTerminalEventsRef.current.size > WORKSPACE_TERMINAL_PENDING_EVENT_IDS) {
        const firstID = pendingTerminalEventsRef.current.keys().next().value;
        if (!firstID) {
          break;
        }
        pendingTerminalEventsRef.current.delete(firstID);
      }
    }

    function handleTerminalEvent(event: TerminalSessionEvent): void {
      if (event.type === "data") {
        terminal.write(event.text);
        return;
      }
      if (event.type === "exit") {
        terminal.writeln("");
        terminal.writeln(`[${terminalExitText(event)}]`);
        setTerminalState("exited");
        sessionIDRef.current = undefined;
        return;
      }
      terminal.writeln("");
      terminal.writeln(`[terminal error: ${event.message}]`);
      setTerminalState("error");
      sessionIDRef.current = undefined;
    }

    function flushPendingTerminalEvents(id: string): void {
      const events = pendingTerminalEventsRef.current.get(id);
      if (!events) {
        return;
      }
      pendingTerminalEventsRef.current.delete(id);
      for (const event of events) {
        handleTerminalEvent(event);
      }
    }

    const unsubscribeTerminal = window.wuu.onTerminalEvent((event) => {
      if (event.id !== sessionIDRef.current) {
        bufferTerminalEvent(event);
        return;
      }
      handleTerminalEvent(event);
    });

    async function startSession(): Promise<void> {
      try {
        fitAndResize();
        const started = await window.wuu.startTerminalSession({ cols: terminal.cols, rows: terminal.rows });
        if (disposed) {
          void window.wuu.stopTerminalSession(started.id);
          return;
        }
        sessionIDRef.current = started.id;
        setTerminalState("ready");
        flushPendingTerminalEvents(started.id);
        fitAndResize();
        terminal.focus();
      } catch (error) {
        terminal.writeln(desktopApiErrorMessage(error, "终端启动失败"));
        setTerminalState("error");
      }
    }

    void startSession();

    return () => {
      disposed = true;
      if (resizeFrame !== undefined) {
        window.cancelAnimationFrame(resizeFrame);
      }
      const sessionID = sessionIDRef.current;
      sessionIDRef.current = undefined;
      if (sessionID) {
        void window.wuu.stopTerminalSession(sessionID);
      }
      unsubscribeTerminal();
      dataDisposable.dispose();
      resizeObserver.disconnect();
      pendingTerminalEventsRef.current.clear();
      terminal.dispose();
      terminalRef.current = null;
    };
  }, [restartKey, workspaceRoot]);

  if (!workspaceRoot) {
    return <WorkspacePanelEmpty title="没有项目" description="先选择一个项目。" icon={<Terminal size={24} />} />;
  }

  return (
    <div className="workspace-terminal-panel">
      <div className="workspace-terminal-screen" onMouseDown={() => terminalRef.current?.focus()}>
        <div className="workspace-terminal-host" ref={containerRef} />
      </div>
      {terminalState === "exited" || terminalState === "error" ? (
        <button className="workspace-terminal-restart" type="button" onClick={() => setRestartKey((current) => current + 1)}>
          重新启动
        </button>
      ) : null}
    </div>
  );
}
