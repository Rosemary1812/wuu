import { useCallback, useEffect, useRef, useState } from "react";
import type { Thread } from "../shared/protocol";

const WORKSPACE_BROWSER_HOME_URL = "wuu://new-tab";

export function useThreadBrowserPreview({
  activeThread,
  activeThreadID,
  onOpenBrowser,
}: {
  activeThread?: Thread;
  activeThreadID?: string;
  onOpenBrowser: () => void;
}): {
  pendingBrowserURL?: string;
  consumePendingBrowserURL: () => void;
  openBrowserURL: (url: string) => void;
  rememberBrowserURLForActiveThread: (url: string) => void;
} {
  const activeBrowserThreadIDRef = useRef<string | undefined>(undefined);
  const browserURLByThreadRef = useRef(new Map<string, string>());
  const lastAutoNavigatedBrowserURLByThreadRef = useRef(
    new Map<string, string>(),
  );
  const onOpenBrowserRef = useRef(onOpenBrowser);
  const [pendingBrowserURL, setPendingBrowserURL] = useState<
    string | undefined
  >(undefined);

  useEffect(() => {
    onOpenBrowserRef.current = onOpenBrowser;
  }, [onOpenBrowser]);

  const rememberBrowserURLForActiveThread = useCallback(
    (url: string): void => {
      if (!activeThreadID) {
        return;
      }
      if (!url || url === WORKSPACE_BROWSER_HOME_URL) {
        browserURLByThreadRef.current.delete(activeThreadID);
        return;
      }
      browserURLByThreadRef.current.set(activeThreadID, url);
    },
    [activeThreadID],
  );

  const consumePendingBrowserURL = useCallback((): void => {
    setPendingBrowserURL(undefined);
  }, []);

  const openBrowserURL = useCallback((url: string): void => {
    onOpenBrowserRef.current();
    setPendingBrowserURL(url);
  }, []);

  // Switching sessions restores that session's browser URL without forcing the
  // right panel open. A new preview URL reported by the already-active session
  // still auto-opens the browser once.
  useEffect(() => {
    if (!activeThreadID) {
      activeBrowserThreadIDRef.current = undefined;
      setPendingBrowserURL(WORKSPACE_BROWSER_HOME_URL);
      return;
    }
    const previewURL = primaryBrowserURLForThread(activeThread);
    const switchedThread = activeBrowserThreadIDRef.current !== activeThreadID;
    if (switchedThread) {
      activeBrowserThreadIDRef.current = activeThreadID;
      if (previewURL) {
        lastAutoNavigatedBrowserURLByThreadRef.current.set(
          activeThreadID,
          previewURL,
        );
      }
      setPendingBrowserURL(
        restoreBrowserURLForThread(activeThread, browserURLByThreadRef.current),
      );
      return;
    }
    if (
      !previewURL ||
      lastAutoNavigatedBrowserURLByThreadRef.current.get(activeThreadID) ===
        previewURL
    ) {
      return;
    }
    lastAutoNavigatedBrowserURLByThreadRef.current.set(
      activeThreadID,
      previewURL,
    );
    onOpenBrowserRef.current();
    setPendingBrowserURL(previewURL);
  }, [
    activeThreadID,
    activeThread,
    activeThread?.browser_state?.current_url,
    activeThread?.browser_state?.primary_preview_url,
    activeThread?.listening_ports,
  ]);

  return {
    pendingBrowserURL,
    consumePendingBrowserURL,
    openBrowserURL,
    rememberBrowserURLForActiveThread,
  };
}

function primaryBrowserURLForThread(thread: Thread | undefined): string | undefined {
  const browserStateURL = thread?.browser_state?.primary_preview_url?.trim();
  if (browserStateURL) {
    return browserStateURL;
  }
  const ports = thread?.listening_ports;
  return ports && ports.length > 0 ? `http://localhost:${ports[0]}` : undefined;
}

function restoreBrowserURLForThread(
  thread: Thread | undefined,
  browserURLs: Map<string, string>,
): string {
  if (!thread) {
    return WORKSPACE_BROWSER_HOME_URL;
  }
  return (
    browserURLs.get(thread.id) ||
    thread.browser_state?.current_url?.trim() ||
    primaryBrowserURLForThread(thread) ||
    WORKSPACE_BROWSER_HOME_URL
  );
}
