import { useEffect, useState } from "react";
import type { RuntimeContext, ServerEvent } from "../shared/protocol";

const BUSY_LIFECYCLE_METHODS = new Set([
  "turn/started",
  "turn/completed",
  "turn/error",
  "thread/started",
  "thread/resumed",
]);

function invalidatesGitActionBusy(event: ServerEvent): boolean {
  if (event.kind === "server-exit") {
    return true;
  }
  return (
    event.kind === "notification" &&
    BUSY_LIFECYCLE_METHODS.has(event.message.method)
  );
}

export function useGitActionBusy(
  context: RuntimeContext | undefined,
  runningThreadKey: string,
): boolean {
  const [snapshot, setSnapshot] = useState<{
    lookupKey: string;
    busy: boolean;
  }>(() => ({ lookupKey: "", busy: true }));
  const contextKey = context
    ? [context.kind, context.kind === "project" ? context.project_id : "", context.cwd].join(
        "\0",
      )
    : "";
  const lookupKey = `${contextKey}\0${runningThreadKey}`;

  useEffect(() => {
    if (!contextKey) {
      return;
    }
    let cancelled = false;
    let requestID = 0;
    const refresh = (): void => {
      const currentRequestID = ++requestID;
      const query = window.wuu.gitActionBusy;
      if (typeof query !== "function") {
        setSnapshot({ lookupKey, busy: true });
        return;
      }
      void query()
        .then((busy) => {
          if (!cancelled && currentRequestID === requestID) {
            setSnapshot({ lookupKey, busy });
          }
        })
        .catch(() => {
          if (!cancelled && currentRequestID === requestID) {
            setSnapshot({ lookupKey, busy: true });
          }
        });
    };

    refresh();
    const off = window.wuu.onServerEvent((event) => {
      if (!invalidatesGitActionBusy(event)) {
        return;
      }
      setSnapshot({ lookupKey: "", busy: true });
      refresh();
    });
    return () => {
      cancelled = true;
      off();
    };
  }, [contextKey, lookupKey]);

  if (!context) {
    return false;
  }
  return snapshot.lookupKey === lookupKey ? snapshot.busy : true;
}
