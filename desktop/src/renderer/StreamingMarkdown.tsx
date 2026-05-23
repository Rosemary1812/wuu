import { useCallback, useEffect, useRef, useState } from "react";
import { MarkdownContent, type RichTextRenderer } from "./RichContent";
import { streamTextStore } from "./StreamText";

type StreamingMarkdownProps = {
  streamKey: string;
  initialText?: string;
  cwd?: string;
  className?: string;
  final?: boolean;
  live?: boolean;
  onFrame?: () => void;
  onSettled?: () => void;
};

type FadeUnit = {
  text: string;
  start: number;
  whitespace: boolean;
};

type StreamVisualState = "streaming" | "settling" | "settled";

type SmoothStreamState = {
  text: string;
  visualState: StreamVisualState;
};

const STREAM_CONFIG = {
  defaultCps: 60,
  flushCps: 180,
  maxCps: 320,
  minCps: 24,
  settleAnimationMs: 220,
  targetLagChars: 8
};

const FADE_UNIT_PATTERN =
  /\s+|[\p{Script=Han}\p{Script=Hiragana}\p{Script=Katakana}\p{Script=Hangul}]|[^\s\p{Script=Han}\p{Script=Hiragana}\p{Script=Katakana}\p{Script=Hangul}]+/gu;

export function StreamingMarkdown({
  streamKey,
  initialText = "",
  cwd,
  className = "streaming-markdown rich-content",
  final = false,
  live = !final,
  onFrame,
  onSettled
}: StreamingMarkdownProps): JSX.Element {
  const { text, visualState } = useSmoothStreamText(streamKey, initialText, { final, live, onFrame, onSettled });

  return (
    <div className={className} data-stream-state={visualState}>
      <MarkdownContent text={text} cwd={cwd} renderText={renderStreamText} />
    </div>
  );
}

const renderStreamText: RichTextRenderer = (text, keyPrefix) => {
  return splitFadeUnits(text).map((unit) =>
    unit.whitespace ? (
      unit.text
    ) : (
      <span key={`${keyPrefix}-${unit.start}`} className="stream-word">
        {unit.text}
      </span>
    )
  );
};

function useSmoothStreamText(
  streamKey: string,
  initialText: string,
  {
    final,
    live,
    onFrame,
    onSettled
  }: {
    final: boolean;
    live: boolean;
    onFrame?: () => void;
    onSettled?: () => void;
  }
): SmoothStreamState {
  const [displayedText, setDisplayedText] = useState(initialText);
  const [visualState, setVisualState] = useState<StreamVisualState>(
    live ? (final ? "settling" : "streaming") : "settled"
  );
  const targetRef = useRef(initialText);
  const targetCharsRef = useRef([...initialText]);
  const displayedCountRef = useRef([...initialText].length);
  const rafRef = useRef<number | undefined>(undefined);
  const settleTimerRef = useRef<number | undefined>(undefined);
  const lastFrameTsRef = useRef<number | undefined>(undefined);
  const finalRef = useRef(final);
  const liveRef = useRef(live);
  const onFrameRef = useRef(onFrame);
  const onSettledRef = useRef(onSettled);
  const settledNotifiedRef = useRef(false);

  useEffect(() => {
    finalRef.current = final;
    liveRef.current = live;
    onFrameRef.current = onFrame;
    onSettledRef.current = onSettled;
  }, [final, live, onFrame, onSettled]);

  const clearSettleTimer = useCallback((): void => {
    if (settleTimerRef.current === undefined) {
      return;
    }
    window.clearTimeout(settleTimerRef.current);
    settleTimerRef.current = undefined;
  }, []);

  const scheduleSettled = useCallback((): void => {
    if (!liveRef.current || !finalRef.current || settledNotifiedRef.current) {
      return;
    }
    if (displayedCountRef.current < targetCharsRef.current.length) {
      return;
    }
    setVisualState("settling");
    if (settleTimerRef.current !== undefined) {
      return;
    }
    settleTimerRef.current = window.setTimeout(() => {
      settleTimerRef.current = undefined;
      if (!finalRef.current || settledNotifiedRef.current) {
        return;
      }
      if (displayedCountRef.current < targetCharsRef.current.length) {
        return;
      }
      settledNotifiedRef.current = true;
      setVisualState("settled");
      onSettledRef.current?.();
    }, STREAM_CONFIG.settleAnimationMs);
  }, []);

  const syncImmediate = useCallback((nextText: string): void => {
    if (rafRef.current !== undefined) {
      window.cancelAnimationFrame(rafRef.current);
      rafRef.current = undefined;
    }
    clearSettleTimer();
    lastFrameTsRef.current = undefined;
    targetRef.current = nextText;
    targetCharsRef.current = [...nextText];
    displayedCountRef.current = targetCharsRef.current.length;
    settledNotifiedRef.current = false;
    setVisualState(liveRef.current ? (finalRef.current ? "settling" : "streaming") : "settled");
    setDisplayedText(nextText);
    onFrameRef.current?.();
    scheduleSettled();
  }, [clearSettleTimer, scheduleSettled]);

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
        scheduleSettled();
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
      setDisplayedText(nextText);
      setVisualState(finalRef.current ? "settling" : "streaming");
      onFrameRef.current?.();
      if (nextCount >= targetCount) {
        rafRef.current = undefined;
        lastFrameTsRef.current = undefined;
        scheduleSettled();
        return;
      }
      rafRef.current = window.requestAnimationFrame(tick);
    };

    rafRef.current = window.requestAnimationFrame(tick);
  }, [scheduleSettled]);

  useEffect(() => {
    if (!live) {
      clearSettleTimer();
      if (rafRef.current !== undefined) {
        window.cancelAnimationFrame(rafRef.current);
        rafRef.current = undefined;
      }
      lastFrameTsRef.current = undefined;
      targetRef.current = initialText;
      targetCharsRef.current = [...initialText];
      displayedCountRef.current = targetCharsRef.current.length;
      settledNotifiedRef.current = false;
      setVisualState("settled");
      setDisplayedText(initialText);
      return;
    }

    streamTextStore.seed(streamKey, initialText);
    const seedText = streamTextStore.seedValue(streamKey);
    syncImmediate(seedText);

    const applyTarget = (nextText: string): void => {
      if (nextText === targetRef.current) {
        return;
      }
      const previousTarget = targetRef.current;
      if (!nextText.startsWith(previousTarget)) {
        syncImmediate(nextText);
        return;
      }
      clearSettleTimer();
      targetRef.current = nextText;
      targetCharsRef.current = [...nextText];
      settledNotifiedRef.current = false;
      setVisualState(finalRef.current ? "settling" : "streaming");
      startFrameLoop();
    };

    applyTarget(streamTextStore.get(streamKey));
    const unsubscribe = streamTextStore.subscribe(streamKey, applyTarget);
    return () => {
      unsubscribe();
      clearSettleTimer();
      if (rafRef.current !== undefined) {
        window.cancelAnimationFrame(rafRef.current);
        rafRef.current = undefined;
      }
    };
  }, [clearSettleTimer, initialText, live, startFrameLoop, streamKey, syncImmediate]);

  useEffect(() => {
    if (!live) {
      clearSettleTimer();
      settledNotifiedRef.current = false;
      setVisualState("settled");
      return;
    }
    if (!final) {
      clearSettleTimer();
      settledNotifiedRef.current = false;
      setVisualState("streaming");
      return;
    }
    setVisualState("settling");
    scheduleSettled();
  }, [clearSettleTimer, final, live, scheduleSettled]);

  return { text: displayedText, visualState };
}

function splitFadeUnits(text: string): FadeUnit[] {
  const units: FadeUnit[] = [];
  FADE_UNIT_PATTERN.lastIndex = 0;
  let match: RegExpExecArray | null;
  while ((match = FADE_UNIT_PATTERN.exec(text))) {
    const value = match[0];
    units.push({
      text: value,
      start: match.index,
      whitespace: value.trim().length === 0
    });
  }
  return units;
}
