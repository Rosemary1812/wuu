import {
  type Dispatch,
  type RefObject,
  type SetStateAction,
  useEffect,
  useRef,
  useState
} from "react";
import type { ServerEvent } from "../shared/protocol";
import type { AppState } from "./AppState";
import type {
  ComposerFile,
  ComposerImage,
  QueuedComposerMessage
} from "./ComposerMessages";
import {
  buildRunDebugSnapshot,
  runDebugEventFromServerEvent,
  type RunDebugEvent
} from "./RunDebugPanel";

const DEBUG_CONTROLS_KEY = "wuu.desktop.debugControlsEnabled";

function initialDebugControlsEnabled(enabled: boolean, forced: boolean): boolean {
  if (!enabled) {
    return false;
  }
  if (forced) {
    return true;
  }
  return window.localStorage.getItem(DEBUG_CONTROLS_KEY) === "true";
}

export function useAppDebugState({
  enabled,
  forced,
  onHideDebugControls
}: {
  enabled: boolean;
  forced: boolean;
  onHideDebugControls: () => void;
}): {
  debugControlsEnabled: boolean;
  setDebugControlsEnabled: Dispatch<SetStateAction<boolean>>;
  debugControlsVisible: boolean;
  runDebugOpen: boolean;
  setRunDebugOpen: Dispatch<SetStateAction<boolean>>;
  conversationGridVisible: boolean;
  setConversationGridVisible: Dispatch<SetStateAction<boolean>>;
  runDebugEvents: RunDebugEvent[];
  runDebugCopied: boolean;
  runDebugRef: RefObject<HTMLDivElement | null>;
  appendRunDebugEvent: (entry: Omit<RunDebugEvent, "id" | "at">) => void;
  resetRunDebugEvents: (entry: Omit<RunDebugEvent, "id" | "at">) => void;
  recordRunDebugEvent: (event: ServerEvent) => void;
  copyRunDebugInfo: (params: {
    state: AppState;
    queuedMessages: QueuedComposerMessage[];
    guideMessages: QueuedComposerMessage[];
    composerImages: ComposerImage[];
    composerFiles: ComposerFile[];
  }) => Promise<void>;
} {
  const [debugControlsEnabled, setDebugControlsEnabled] = useState(() =>
    initialDebugControlsEnabled(enabled, forced)
  );
  const [runDebugOpen, setRunDebugOpen] = useState(false);
  const [conversationGridVisible, setConversationGridVisible] = useState(false);
  const [runDebugEvents, setRunDebugEvents] = useState<RunDebugEvent[]>([]);
  const [runDebugCopied, setRunDebugCopied] = useState(false);
  const runDebugRef = useRef<HTMLDivElement>(null);
  const runDebugEventIDRef = useRef(0);
  const runDebugDeltaSeenRef = useRef(new Set<string>());
  const debugControlsVisible = enabled && debugControlsEnabled;

  function appendRunDebugEvent(entry: Omit<RunDebugEvent, "id" | "at">): void {
    const next: RunDebugEvent = {
      ...entry,
      id: ++runDebugEventIDRef.current,
      at: Date.now()
    };
    setRunDebugEvents((current) => [...current, next].slice(-80));
  }

  function resetRunDebugEvents(entry: Omit<RunDebugEvent, "id" | "at">): void {
    runDebugDeltaSeenRef.current.clear();
    const next: RunDebugEvent = {
      ...entry,
      id: ++runDebugEventIDRef.current,
      at: Date.now()
    };
    setRunDebugEvents([next]);
  }

  function recordRunDebugEvent(event: ServerEvent): void {
    const entry = runDebugEventFromServerEvent(event, runDebugDeltaSeenRef.current);
    if (entry) {
      appendRunDebugEvent(entry);
    }
  }

  async function copyRunDebugInfo({
    state,
    queuedMessages,
    guideMessages,
    composerImages,
    composerFiles
  }: {
    state: AppState;
    queuedMessages: QueuedComposerMessage[];
    guideMessages: QueuedComposerMessage[];
    composerImages: ComposerImage[];
    composerFiles: ComposerFile[];
  }): Promise<void> {
    const snapshot = buildRunDebugSnapshot({
      state,
      events: runDebugEvents,
      queuedMessages,
      guideMessages,
      composerImages,
      composerFiles
    });
    try {
      await navigator.clipboard.writeText(snapshot);
      setRunDebugCopied(true);
      window.setTimeout(() => setRunDebugCopied(false), 1200);
    } catch (error) {
      appendRunDebugEvent({
        source: "client",
        method: "debug/copy",
        detail: error instanceof Error ? error.message : "复制失败",
        tone: "error"
      });
    }
  }

  useEffect(() => {
    if (enabled) {
      window.localStorage.setItem(DEBUG_CONTROLS_KEY, String(debugControlsEnabled));
    }
  }, [debugControlsEnabled, enabled]);

  useEffect(() => {
    if (debugControlsVisible) {
      return;
    }
    setConversationGridVisible(false);
    setRunDebugOpen(false);
    onHideDebugControls();
  }, [debugControlsVisible, onHideDebugControls]);

  useEffect(() => {
    if (!debugControlsVisible) {
      return;
    }

    const handleKeyDown = (event: KeyboardEvent): void => {
      if (
        event.key.toLowerCase() !== "g" ||
        event.metaKey ||
        event.ctrlKey ||
        event.altKey
      ) {
        return;
      }
      const target = event.target;
      if (
        target instanceof HTMLElement &&
        (target.isContentEditable ||
          target.tagName === "INPUT" ||
          target.tagName === "TEXTAREA" ||
          target.tagName === "SELECT")
      ) {
        return;
      }
      event.preventDefault();
      setConversationGridVisible((visible) => !visible);
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [debugControlsVisible]);

  return {
    debugControlsEnabled,
    setDebugControlsEnabled,
    debugControlsVisible,
    runDebugOpen,
    setRunDebugOpen,
    conversationGridVisible,
    setConversationGridVisible,
    runDebugEvents,
    runDebugCopied,
    runDebugRef,
    appendRunDebugEvent,
    resetRunDebugEvents,
    recordRunDebugEvent,
    copyRunDebugInfo
  };
}
