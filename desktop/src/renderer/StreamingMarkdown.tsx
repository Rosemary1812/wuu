import { useCallback, useEffect, useRef, useState } from "react";
import { MarkdownContent } from "./RichContent";
import { streamTextStore } from "./StreamText";

type StreamingMarkdownProps = {
  streamKey: string;
  initialText?: string;
  cwd?: string;
  className?: string;
  final?: boolean;
  live?: boolean;
  settleMode?: "rich" | "stream";
  onFrame?: () => void;
  onSettled?: () => void;
};

type SettledMarkdown = {
  streamKey: string;
  text: string;
};

const STREAM_CONFIG = {
  defaultCps: 60,
  flushCps: 180,
  maxCps: 320,
  minCps: 24,
  targetLagChars: 8
};

export function StreamingMarkdown({
  streamKey,
  initialText = "",
  cwd,
  className = "streaming-markdown rich-content",
  final = false,
  live = !final,
  settleMode = "rich",
  onFrame,
  onSettled
}: StreamingMarkdownProps): JSX.Element {
  const useRichSettledView = settleMode === "rich";
  const [settled, setSettled] = useState<SettledMarkdown | undefined>(() =>
    useRichSettledView && !live && final ? { streamKey, text: initialText } : undefined
  );

  useEffect(() => {
    if (!useRichSettledView) {
      setSettled(undefined);
      return;
    }
    if (!live && final) {
      setSettled({ streamKey, text: initialText });
      return;
    }
    setSettled(undefined);
  }, [final, initialText, live, streamKey, useRichSettledView]);

  const handleSettled = useCallback(
    (text: string) => {
      if (useRichSettledView) {
        setSettled({ streamKey, text });
      }
      onSettled?.();
    },
    [onSettled, streamKey, useRichSettledView]
  );

  if (useRichSettledView && final && settled?.streamKey === streamKey) {
    return (
      <div className={className} data-stream-state="settled">
        <MarkdownContent text={settled.text} cwd={cwd} />
      </div>
    );
  }

  return (
    <StreamingMarkdownSurface
      streamKey={streamKey}
      initialText={initialText}
      cwd={cwd}
      className={className}
      final={final}
      live={live}
      onFrame={onFrame}
      onSettled={handleSettled}
    />
  );
}

function StreamingMarkdownSurface({
  streamKey,
  initialText,
  cwd,
  className,
  final,
  live,
  onFrame,
  onSettled
}: {
  streamKey: string;
  initialText: string;
  cwd?: string;
  className: string;
  final: boolean;
  live: boolean;
  onFrame?: () => void;
  onSettled: (text: string) => void;
}): JSX.Element {
  const [visibleText, setVisibleText] = useState(initialText);
  const targetRef = useRef(initialText);
  const targetCharsRef = useRef([...initialText]);
  const displayedCountRef = useRef([...initialText].length);
  const rafRef = useRef<number | undefined>(undefined);
  const lastFrameTsRef = useRef<number | undefined>(undefined);
  const finalRef = useRef(final);
  const liveRef = useRef(live);
  const onFrameRef = useRef(onFrame);
  const onSettledRef = useRef(onSettled);
  const settledNotifiedRef = useRef(false);

  const notifySettled = useCallback((): void => {
    if (!finalRef.current || !liveRef.current || settledNotifiedRef.current) {
      return;
    }
    if (displayedCountRef.current < targetCharsRef.current.length) {
      return;
    }
    settledNotifiedRef.current = true;
    onSettledRef.current(targetRef.current);
  }, []);

  const syncImmediate = useCallback(
    (nextText: string, notify = true): void => {
      if (rafRef.current !== undefined) {
        window.cancelAnimationFrame(rafRef.current);
        rafRef.current = undefined;
      }
      lastFrameTsRef.current = undefined;
      targetRef.current = nextText;
      targetCharsRef.current = [...nextText];
      displayedCountRef.current = targetCharsRef.current.length;
      settledNotifiedRef.current = false;
      setVisibleText(nextText);
      onFrameRef.current?.();
      if (notify) {
        notifySettled();
      }
    },
    [notifySettled]
  );

  const startFrameLoop = useCallback((): void => {
    if (rafRef.current !== undefined) {
      return;
    }

    const tick = (ts: number): void => {
      const targetCount = targetCharsRef.current.length;
      const displayedCount = displayedCountRef.current;
      const backlog = targetCount - displayedCount;
      if (backlog <= 0) {
        rafRef.current = undefined;
        lastFrameTsRef.current = undefined;
        notifySettled();
        return;
      }

      const lastFrameTs = lastFrameTsRef.current ?? ts;
      lastFrameTsRef.current = ts;
      const deltaSeconds = Math.max(0.001, Math.min((ts - lastFrameTs) / 1000, 0.05));
      const laggedBacklog = Math.max(0, backlog - STREAM_CONFIG.targetLagChars);
      const cps = Math.min(
        STREAM_CONFIG.maxCps,
        Math.max(STREAM_CONFIG.minCps, laggedBacklog > 0 ? STREAM_CONFIG.flushCps : STREAM_CONFIG.defaultCps)
      );
      const revealCount = Math.max(1, Math.min(backlog, Math.ceil(cps * deltaSeconds)));
      const nextCount = displayedCount + revealCount;
      const nextText = targetCharsRef.current.slice(0, nextCount).join("");

      displayedCountRef.current = nextCount;
      setVisibleText(nextText);
      onFrameRef.current?.();
      if (nextCount >= targetCount) {
        rafRef.current = undefined;
        lastFrameTsRef.current = undefined;
        notifySettled();
        return;
      }
      rafRef.current = window.requestAnimationFrame(tick);
    };

    rafRef.current = window.requestAnimationFrame(tick);
  }, [notifySettled]);

  useEffect(() => {
    finalRef.current = final;
    liveRef.current = live;
    onFrameRef.current = onFrame;
    onSettledRef.current = onSettled;
    notifySettled();
  }, [final, live, notifySettled, onFrame, onSettled]);

  useEffect(() => {
    if (!live) {
      syncImmediate(initialText);
      return undefined;
    }

    streamTextStore.seed(streamKey, initialText);
    const seedText = streamTextStore.seedValue(streamKey);
    syncImmediate(seedText, false);

    const applyTarget = (nextText: string): void => {
      if (nextText === targetRef.current) {
        return;
      }
      const previousTarget = targetRef.current;
      if (!nextText.startsWith(previousTarget)) {
        syncImmediate(nextText);
        return;
      }
      targetRef.current = nextText;
      targetCharsRef.current = [...nextText];
      settledNotifiedRef.current = false;
      startFrameLoop();
    };

    applyTarget(streamTextStore.get(streamKey));
    notifySettled();
    const unsubscribe = streamTextStore.subscribe(streamKey, applyTarget);
    return () => {
      unsubscribe();
      if (rafRef.current !== undefined) {
        window.cancelAnimationFrame(rafRef.current);
        rafRef.current = undefined;
      }
    };
  }, [initialText, live, notifySettled, startFrameLoop, streamKey, syncImmediate]);

  const streamState = final ? (live ? "settling" : "settled") : "streaming";
  return (
    <div className={className} data-stream-state={streamState}>
      {live ? (
        <div className="streaming-plain-text">{visibleText}</div>
      ) : (
        <MarkdownContent text={visibleText} cwd={cwd} renderMermaid />
      )}
    </div>
  );
}
