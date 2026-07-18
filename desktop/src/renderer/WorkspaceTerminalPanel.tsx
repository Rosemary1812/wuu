import { FitAddon } from "@xterm/addon-fit";
import { Terminal as XtermTerminal, type ITheme } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { CheckCircle2, Clock3, Square, SquareTerminal, Terminal, XCircle } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type {
  ManagedProcessSummary,
  RuntimeContext,
  TerminalSessionEvent,
  Thread,
} from "../shared/protocol";
import { currentAppliedTheme, observeAppliedTheme, type AppliedTheme } from "./Theme";
import {
  agentRunGroupsForThread,
  selectAgentRun,
  type AgentRunLocator,
  type AgentRunRecord,
} from "./TerminalRuns";
import { WorkspacePanelEmpty } from "./WorkspaceFiles";
import { desktopApiErrorMessage } from "./WorkspaceReviewHelpers";
import { translateCurrent, useI18n } from "./i18n";

const WORKSPACE_TERMINAL_PENDING_EVENT_IDS = 12;
const WORKSPACE_TERMINAL_PENDING_EVENTS_PER_ID = 256;
const WORKSPACE_TERMINAL_PENDING_TEXT_PER_ID = 512 * 1024;

type WorkspaceTerminalState = "starting" | "ready" | "exited" | "error";

function workspaceTerminalTheme(theme: AppliedTheme): ITheme {
  if (theme === "dark") {
    return {
      background: "#1d2024",
      black: "#141618",
      blue: "#58a6ff",
      cursor: "#f2f3f4",
      foreground: "#e4e6e8",
      green: "#4cc38a",
      red: "#f0705f",
      selectionBackground: "#3a4046",
      yellow: "#d9a84e",
    };
  }
  return {
    background: "#ffffff",
    black: "#24292f",
    blue: "#2f98ff",
    cursor: "#202427",
    foreground: "#1f2328",
    green: "#1f9d46",
    red: "#b42318",
    selectionBackground: "#d7e9ff",
    yellow: "#ffc21a",
  };
}

export function appendPendingTerminalEvent(
  events: TerminalSessionEvent[],
  event: TerminalSessionEvent,
): TerminalSessionEvent[] {
  const normalized =
    event.type === "data" &&
    event.text.length > WORKSPACE_TERMINAL_PENDING_TEXT_PER_ID
      ? {
          ...event,
          text: event.text.slice(-WORKSPACE_TERMINAL_PENDING_TEXT_PER_ID),
        }
      : event;
  const next = [...events, normalized];
  let textLength = next.reduce(
    (total, pending) =>
      total + (pending.type === "data" ? pending.text.length : 0),
    0,
  );
  while (
    next.length > WORKSPACE_TERMINAL_PENDING_EVENTS_PER_ID ||
    textLength > WORKSPACE_TERMINAL_PENDING_TEXT_PER_ID
  ) {
    const dataIndex = next.findIndex((pending) => pending.type === "data");
    const removeIndex = dataIndex >= 0 ? dataIndex : 0;
    const [removed] = next.splice(removeIndex, 1);
    if (removed?.type === "data") {
      textLength -= removed.text.length;
    }
  }
  return next;
}

function formatTerminalDuration(ms: number): string {
  const totalSeconds = Math.max(0, Math.floor(ms / 1000));
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  if (hours > 0) {
    return translateCurrent("workspace.terminal.durationHours", { hours, minutes, seconds });
  }
  if (minutes > 0) {
    return translateCurrent("workspace.terminal.durationMinutes", { minutes, seconds });
  }
  return translateCurrent("workspace.terminal.durationSeconds", { seconds });
}

function terminalExitText(event: Extract<TerminalSessionEvent, { type: "exit" }>): string {
  const duration = formatTerminalDuration(event.duration_ms);
  if (event.signal) {
    return translateCurrent("workspace.terminal.stoppedBy", { signal: event.signal, duration });
  }
  return translateCurrent("workspace.terminal.exited", {
    code: event.exit_code ?? translateCurrent("workspace.terminal.unknown"),
    duration,
  });
}

export type WorkspaceTerminalRunRequest = AgentRunLocator & { requestID: number };

export function WorkspaceTerminalPanel({
  activeContext,
  thread,
  requestedRun,
}: {
  activeContext?: RuntimeContext;
  thread?: Thread;
  requestedRun?: WorkspaceTerminalRunRequest;
}): JSX.Element {
  const { t } = useI18n();
  const groups = useMemo(
    () => (thread ? agentRunGroupsForThread(thread) : []),
    [thread],
  );
  const requestedRecord = requestedRun ? selectAgentRun(groups, requestedRun) : undefined;
  const [selectedResourceID, setSelectedResourceID] = useState(
    () => requestedRecord?.toolCallID ?? "user-terminal",
  );
  const [userTerminalOpened, setUserTerminalOpened] = useState(
    () => !requestedRecord,
  );
  const [managedProcesses, setManagedProcesses] = useState<Record<string, ManagedProcessSummary>>({});
  const selectedRun = groups
    .flatMap((group) => group.runs)
    .find((run) => run.toolCallID === selectedResourceID);
  const managedProcessIDs = useMemo(
    () => groups.flatMap((group) => group.runs.flatMap((run) => run.processID ? [run.processID] : [])),
    [groups],
  );
  const managedProcessKey = managedProcessIDs.join("\u0000");
  const handleManagedProcessChange = useCallback((next: ManagedProcessSummary) => {
    setManagedProcesses((current) => ({
      ...current,
      [next.id]: preferManagedProcess(current[next.id], next),
    }));
  }, []);

  useEffect(() => {
    const threadID = thread?.id;
    if (!threadID || managedProcessIDs.length === 0) {
      setManagedProcesses({});
      return undefined;
    }
    let disposed = false;
    let refreshTimer: number | undefined;
    const activeThreadID = threadID;
    const wanted = new Set(managedProcessIDs);

    async function refresh(): Promise<void> {
      try {
        const result = await window.wuu.listManagedProcesses(activeThreadID);
        if (disposed) {
          return;
        }
        const incoming = result.processes.filter((process) => wanted.has(process.id));
        setManagedProcesses((current) => {
          return Object.fromEntries(incoming.map((process) => [
            process.id,
            preferManagedProcess(current[process.id], process),
          ]));
        });
        if (incoming.some(isManagedProcessLive)) {
          refreshTimer = window.setTimeout(() => void refresh(), 1500);
        }
      } catch {
        if (!disposed) {
          refreshTimer = window.setTimeout(() => void refresh(), 3000);
        }
      }
    }

    void refresh();
    return () => {
      disposed = true;
      if (refreshTimer !== undefined) {
        window.clearTimeout(refreshTimer);
      }
    };
  }, [managedProcessKey, thread?.id]);

  useEffect(() => {
    if (!requestedRun) {
      return;
    }
    const next = selectAgentRun(groups, requestedRun);
    if (next) {
      setSelectedResourceID(next.toolCallID);
    }
  }, [groups, requestedRun?.requestID, requestedRun?.threadID, requestedRun?.turnID, requestedRun?.toolCallID]);

  useEffect(() => {
    if (selectedResourceID === "user-terminal" || selectedRun) {
      return;
    }
    setSelectedResourceID("user-terminal");
    setUserTerminalOpened(true);
  }, [selectedResourceID, selectedRun]);

  function openUserTerminal(): void {
    setUserTerminalOpened(true);
    setSelectedResourceID("user-terminal");
  }

  return (
    <div className="workspace-terminal-workspace">
      <nav className="workspace-terminal-navigation" aria-label={t("workspace.terminal.resources")}>
        <button
          className={`workspace-terminal-resource${selectedResourceID === "user-terminal" ? " active" : ""}`}
          type="button"
          onClick={openUserTerminal}
        >
          <SquareTerminal className="icon" />
          <span>{t("workspace.terminal.userTerminal")}</span>
        </button>
        <div className="workspace-terminal-run-list">
          <div className="workspace-terminal-nav-heading">{t("workspace.terminal.agentRuns")}</div>
          {groups.length > 0 ? groups.slice().reverse().map((group) => (
            <section className="workspace-terminal-turn-group" key={group.turnID}>
              <div className="workspace-terminal-turn-heading">
                {t("workspace.terminal.turnLabel", { number: group.turnNumber })}
              </div>
              {group.runs.map((run) => (
                <button
                  className={`workspace-terminal-resource workspace-terminal-run${selectedResourceID === run.toolCallID ? " active" : ""}`}
                  type="button"
                  key={run.toolCallID}
                  title={run.command}
                  onClick={() => setSelectedResourceID(run.toolCallID)}
                >
                  <RunStatusIcon run={run} process={run.processID ? managedProcesses[run.processID] : undefined} />
                  <span>{run.command}</span>
                </button>
              ))}
            </section>
          )) : (
            <div className="workspace-terminal-no-runs">{t("workspace.terminal.noRuns")}</div>
          )}
        </div>
      </nav>
      <div className="workspace-terminal-content">
        {userTerminalOpened ? (
          <UserTerminalPane
            active={selectedResourceID === "user-terminal"}
            activeContext={activeContext}
          />
        ) : null}
        {selectedRun ? (
          <AgentTerminalPane
            key={selectedRun.toolCallID}
            run={selectedRun}
            process={selectedRun.processID ? managedProcesses[selectedRun.processID] : undefined}
            onProcessChange={handleManagedProcessChange}
          />
        ) : null}
      </div>
    </div>
  );
}

function RunStatusIcon({
  run,
  process,
}: {
  run: AgentRunRecord;
  process?: ManagedProcessSummary;
}): JSX.Element {
  if (process ? process.status === "failed" : run.status === "failed") {
    return <XCircle className="icon failed" />;
  }
  if (run.execution === "managed" && (!process || isManagedProcessLive(process))) {
    return <Clock3 className="icon live" />;
  }
  if (process?.status === "stopped" || run.status === "completed") {
    return <CheckCircle2 className="icon completed" />;
  }
  return <Clock3 className="icon" />;
}

function isManagedProcessLive(process: ManagedProcessSummary): boolean {
  return process.status === "starting" || process.status === "running" || process.status === "stopping";
}

function preferManagedProcess(
  current: ManagedProcessSummary | undefined,
  next: ManagedProcessSummary,
): ManagedProcessSummary {
  if (!current) {
    return next;
  }
  if (!isManagedProcessLive(current) && isManagedProcessLive(next)) {
    return current;
  }
  return Date.parse(next.updated_at) < Date.parse(current.updated_at) ? current : next;
}

function AgentTerminalPane({
  run,
  process,
  onProcessChange,
}: {
  run: AgentRunRecord;
  process?: ManagedProcessSummary;
  onProcessChange: (process: ManagedProcessSummary) => void;
}): JSX.Element {
  const { locale, t } = useI18n();
  const containerRef = useRef<HTMLDivElement | null>(null);
  const terminalRef = useRef<XtermTerminal | null>(null);
  const processRef = useRef<ManagedProcessSummary | undefined>(process);
  const onProcessChangeRef = useRef(onProcessChange);
  const [currentProcess, setCurrentProcess] = useState(process);
  const [stopping, setStopping] = useState(false);
  const [terminalError, setTerminalError] = useState<string | undefined>();
  const processID = run.processID;
  const live = run.execution === "managed" && (!currentProcess || isManagedProcessLive(currentProcess));

  useEffect(() => {
    onProcessChangeRef.current = onProcessChange;
  }, [onProcessChange]);

  useEffect(() => {
    if (!process) {
      return;
    }
    const next = preferManagedProcess(processRef.current, process);
    processRef.current = next;
    setCurrentProcess(next);
    const terminal = terminalRef.current;
    if (terminal) {
      terminal.options.disableStdin = !(next.tty && next.input_available && isManagedProcessLive(next));
    }
  }, [process]);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) {
      return undefined;
    }

    let disposed = false;
    let resizeFrame: number | undefined;
    let didInitialResize = false;
    let offset = 0;
    setTerminalError(undefined);

    const terminal = new XtermTerminal({
      allowTransparency: false,
      convertEol: !run.tty,
      cursorBlink: run.execution === "managed" && run.tty,
      disableStdin: true,
      fontFamily: '"SFMono-Regular", Consolas, "Liberation Mono", monospace',
      fontSize: 12,
      lineHeight: 1.45,
      scrollback: 10000,
      theme: workspaceTerminalTheme(currentAppliedTheme()),
    });
    const fitAddon = new FitAddon();
    terminal.loadAddon(fitAddon);
    terminal.open(container);
    terminalRef.current = terminal;
    const stopObservingTheme = observeAppliedTheme((theme) => {
      terminal.options.theme = workspaceTerminalTheme(theme);
    });

    function updateProcess(next: ManagedProcessSummary): void {
      const preferred = preferManagedProcess(processRef.current, next);
      processRef.current = preferred;
      terminal.options.disableStdin = !(
        preferred.tty && preferred.input_available && isManagedProcessLive(preferred)
      );
      setCurrentProcess(preferred);
      onProcessChangeRef.current(preferred);
    }

    function fitAndResize(): void {
      if (disposed) {
        return;
      }
      try {
        fitAddon.fit();
      } catch {
        return;
      }
      const current = processRef.current;
      if (processID && current?.tty && isManagedProcessLive(current)) {
        void window.wuu.resizeManagedProcess(run.threadID, processID, terminal.cols, terminal.rows).catch((error) => {
          if (!disposed && isManagedProcessLive(processRef.current ?? current)) {
            setTerminalError(desktopApiErrorMessage(error, translateCurrent("workspace.terminal.resizeFailed")));
          }
        });
      }
    }

    const dataDisposable = terminal.onData((data) => {
      const current = processRef.current;
      if (!processID || !current?.tty || !current.input_available || !isManagedProcessLive(current)) {
        return;
      }
      void window.wuu.writeManagedProcess(run.threadID, processID, data).catch((error) => {
        if (!disposed) {
          setTerminalError(desktopApiErrorMessage(error, translateCurrent("workspace.terminal.writeFailed")));
        }
      });
    });
    const resizeObserver = new ResizeObserver(() => {
      if (resizeFrame !== undefined) {
        window.cancelAnimationFrame(resizeFrame);
      }
      resizeFrame = window.requestAnimationFrame(fitAndResize);
    });
    resizeObserver.observe(container);
    resizeFrame = window.requestAnimationFrame(fitAndResize);

    if (run.execution === "snapshot" || !processID) {
      if (run.stdout) {
        terminal.write(run.stdout);
      }
      if (run.stderr) {
        terminal.write(run.stderr);
      }
      if (!run.stdout && !run.stderr) {
        terminal.writeln(t("workspace.terminal.noOutput"));
      }
      if (run.truncated) {
        terminal.writeln("");
        terminal.writeln(`[${t("workspace.terminal.retainedOutputTruncated")}]`);
      }
    } else {
      const managedProcessID = processID;
      async function readManagedOutput(): Promise<void> {
        while (!disposed) {
          try {
            const result = await window.wuu.readManagedProcess({
              thread_id: run.threadID,
              process_id: managedProcessID,
              offset_bytes: offset,
              max_bytes: 512 * 1024,
              wait_ms: 10000,
            });
            if (disposed) {
              return;
            }
            setTerminalError(undefined);
            if (result.truncated && result.start_offset > offset) {
              terminal.writeln(`[${translateCurrent("workspace.terminal.earlierOutputTruncated")}]`);
            }
            if (result.output) {
              terminal.write(result.output);
            }
            offset = result.end_offset;
            updateProcess(result.process);
            if (!didInitialResize) {
              didInitialResize = true;
              fitAndResize();
            }
            if (!isManagedProcessLive(result.process)) {
              return;
            }
          } catch (error) {
            if (!disposed) {
              setTerminalError(desktopApiErrorMessage(error, translateCurrent("workspace.terminal.readFailed")));
            }
            await new Promise((resolve) => window.setTimeout(resolve, 1500));
            const current = processRef.current;
            if (disposed || (current && !isManagedProcessLive(current))) {
              return;
            }
          }
        }
      }
      void readManagedOutput();
    }

    return () => {
      disposed = true;
      if (resizeFrame !== undefined) {
        window.cancelAnimationFrame(resizeFrame);
      }
      dataDisposable.dispose();
      resizeObserver.disconnect();
      stopObservingTheme();
      terminal.dispose();
      terminalRef.current = null;
    };
  }, [locale, processID, run.execution, run.stderr, run.stdout, run.threadID, run.toolCallID, run.truncated, run.tty]);

  async function stopProcess(): Promise<void> {
    if (!processID || stopping) {
      return;
    }
    setStopping(true);
    setTerminalError(undefined);
    try {
      const result = await window.wuu.stopManagedProcess(run.threadID, processID);
      processRef.current = result.process;
      setCurrentProcess(result.process);
      onProcessChange(result.process);
    } catch (error) {
      setTerminalError(desktopApiErrorMessage(error, t("workspace.terminal.stopFailed")));
    } finally {
      setStopping(false);
    }
  }

  return (
    <article className="workspace-agent-terminal" data-tool-call-id={run.toolCallID}>
      <header className="workspace-agent-terminal-toolbar">
        <div className="workspace-agent-terminal-command" title={run.command}>{run.command}</div>
        <div className="workspace-agent-terminal-actions">
          <span className={`workspace-agent-run-status ${currentProcess?.status ?? run.status}`}>
            {managedRunStatusLabel(run, currentProcess, stopping)}
          </span>
          {live && processID ? (
            <button type="button" className="workspace-agent-terminal-stop" disabled={stopping} onClick={() => void stopProcess()}>
              <Square size={12} fill="currentColor" />
              {stopping ? t("workspace.terminal.stopping") : t("workspace.terminal.stop")}
            </button>
          ) : null}
        </div>
      </header>
      {terminalError ? <div className="workspace-agent-terminal-error">{terminalError}</div> : null}
      <div className="workspace-terminal-screen" onMouseDown={() => terminalRef.current?.focus()}>
        <div className="workspace-terminal-host" ref={containerRef} />
      </div>
    </article>
  );
}

function runStatusLabel(run: AgentRunRecord): string {
  switch (run.status) {
    case "completed":
      return translateCurrent("workspace.terminal.status.completed");
    case "failed":
      return translateCurrent("workspace.terminal.status.failed");
    case "interrupted":
      return translateCurrent("workspace.terminal.status.interrupted");
    case "incomplete":
      return translateCurrent("workspace.terminal.status.incomplete");
  }
}

function managedRunStatusLabel(
  run: AgentRunRecord,
  process: ManagedProcessSummary | undefined,
  stopping: boolean,
): string {
  if (stopping || process?.status === "stopping") {
    return translateCurrent("workspace.terminal.status.stopping");
  }
  switch (process?.status) {
    case "starting":
      return translateCurrent("workspace.terminal.status.starting");
    case "running":
      return process.input_available
        ? translateCurrent("workspace.terminal.status.interactive")
        : translateCurrent("workspace.terminal.status.running");
    case "stopped":
      return translateCurrent("workspace.terminal.status.stopped");
    case "failed":
      return translateCurrent("workspace.terminal.status.failed");
    default:
      return run.execution === "managed"
        ? translateCurrent("workspace.terminal.status.running")
        : runStatusLabel(run);
  }
}

function UserTerminalPane({
  active,
  activeContext,
}: {
  active: boolean;
  activeContext?: RuntimeContext;
}): JSX.Element {
  const { t } = useI18n();
  const containerRef = useRef<HTMLDivElement | null>(null);
  const terminalRef = useRef<XtermTerminal | null>(null);
  const sessionIDRef = useRef<string | undefined>(undefined);
  const pendingTerminalEventsRef = useRef(new Map<string, TerminalSessionEvent[]>());
  const [terminalState, setTerminalState] = useState<WorkspaceTerminalState>("starting");
  const [restartKey, setRestartKey] = useState(0);
  const workspaceRoot = activeContext?.cwd;

  useEffect(() => {
    if (active) {
      terminalRef.current?.focus();
    }
  }, [active]);

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
      theme: workspaceTerminalTheme(currentAppliedTheme()),
    });
    const fitAddon = new FitAddon();
    terminal.loadAddon(fitAddon);
    terminal.open(container);
    terminal.focus();
    terminalRef.current = terminal;
    const stopObservingTheme = observeAppliedTheme((theme) => {
      terminal.options.theme = workspaceTerminalTheme(theme);
    });

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
      pendingTerminalEventsRef.current.set(
        event.id,
        appendPendingTerminalEvent(events, event),
      );
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
      terminal.writeln(`[${translateCurrent("workspace.terminal.error", { message: event.message })}]`);
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
        const started = await window.wuu.startTerminalSession({
          cols: terminal.cols,
          rows: terminal.rows,
          cwd: workspaceRoot
        });
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
        terminal.writeln(desktopApiErrorMessage(error, translateCurrent("workspace.terminal.startFailed")));
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
      stopObservingTheme();
      dataDisposable.dispose();
      resizeObserver.disconnect();
      pendingTerminalEventsRef.current.clear();
      terminal.dispose();
      terminalRef.current = null;
    };
  }, [restartKey, workspaceRoot]);

  if (!workspaceRoot) {
    return <WorkspacePanelEmpty title={t("workspace.files.noProject")} description={t("workspace.terminal.noProjectDescription")} icon={<Terminal size={24} />} />;
  }

  return (
    <div className="workspace-terminal-panel" hidden={!active}>
      <div className="workspace-terminal-screen" onMouseDown={() => terminalRef.current?.focus()}>
        <div className="workspace-terminal-host" ref={containerRef} />
      </div>
      {terminalState === "exited" || terminalState === "error" ? (
        <button className="workspace-terminal-restart" type="button" onClick={() => setRestartKey((current) => current + 1)}>
          {t("workspace.terminal.restart")}
        </button>
      ) : null}
    </div>
  );
}
