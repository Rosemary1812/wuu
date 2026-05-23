import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  RichContentBlock,
  parseRichBlocksWithOffsets,
  type RichBlockWithOffset
} from "./RichContent";
import { streamTextStore } from "./StreamText";

type StreamingMarkdownProps = {
  streamKey: string;
  initialText?: string;
  cwd?: string;
  className?: string;
  final?: boolean;
  onFrame?: () => void;
  onSettled?: () => void;
};

type FadeUnit = {
  text: string;
  start: number;
  whitespace: boolean;
};

const STREAM_CONFIG = {
  defaultCps: 60,
  flushCps: 180,
  maxCps: 320,
  minCps: 24,
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
  onFrame,
  onSettled
}: StreamingMarkdownProps): JSX.Element {
  const displayedText = useSmoothStreamText(streamKey, initialText, { final, onFrame, onSettled });
  const blocks = useMemo(() => parseRichBlocksWithOffsets(displayedText, { allowOpenFence: true }), [displayedText]);

  return (
    <div className={className}>
      {blocks.map((block, index) => (
        <StreamingMarkdownBlock
          key={`${block.startOffset}-${block.kind}`}
          block={block}
          blockKey={`${block.startOffset}-${block.kind}`}
          cwd={cwd}
          active={index === blocks.length - 1}
        />
      ))}
    </div>
  );
}

const StreamingMarkdownBlock = memo(
  function StreamingMarkdownBlock({
    block,
    blockKey,
    cwd,
    active
  }: {
    block: RichBlockWithOffset;
    blockKey: string;
    cwd?: string;
    active: boolean;
  }): JSX.Element {
    if (active && block.kind === "paragraph") {
      return (
        <p className="rich-paragraph stream-paragraph">
          <AnimatedStreamText text={block.text} blockKey={blockKey} />
        </p>
      );
    }
    if (active && (block.kind === "code" || block.kind === "mermaid")) {
      const language = block.kind === "code" ? block.language : "mermaid";
      return (
        <pre className="rich-code stream-code" data-language={language || undefined}>
          <code>{block.code}</code>
        </pre>
      );
    }
    return <RichContentBlock block={block} blockKey={blockKey} cwd={cwd} />;
  },
  (prev, next) =>
    prev.active === next.active &&
    prev.cwd === next.cwd &&
    prev.block.kind === next.block.kind &&
    prev.block.startOffset === next.block.startOffset &&
    richBlockContent(prev.block) === richBlockContent(next.block)
);

function AnimatedStreamText({ text, blockKey }: { text: string; blockKey: string }): JSX.Element {
  const units = useMemo(() => splitFadeUnits(text), [text]);
  return (
    <>
      {units.map((unit) =>
        unit.whitespace ? (
          unit.text
        ) : (
          <span key={`${blockKey}-${unit.start}`} className="stream-word">
            {unit.text}
          </span>
        )
      )}
    </>
  );
}

type SmoothStreamOptions = {
  final: boolean;
  onFrame?: () => void;
  onSettled?: () => void;
};

function useSmoothStreamText(
  streamKey: string,
  initialText: string,
  { final, onFrame, onSettled }: SmoothStreamOptions
): string {
  const [displayedText, setDisplayedText] = useState(initialText);
  const targetRef = useRef(initialText);
  const targetCharsRef = useRef([...initialText]);
  const displayedCountRef = useRef([...initialText].length);
  const displayedTextRef = useRef(initialText);
  const rafRef = useRef<number | undefined>(undefined);
  const lastFrameTsRef = useRef<number | undefined>(undefined);
  const finalRef = useRef(final);
  const onFrameRef = useRef(onFrame);
  const onSettledRef = useRef(onSettled);
  const settledNotifiedRef = useRef(false);

  useEffect(() => {
    finalRef.current = final;
    onFrameRef.current = onFrame;
    onSettledRef.current = onSettled;
  }, [final, onFrame, onSettled]);

  const notifySettled = useCallback((): void => {
    if (!finalRef.current || settledNotifiedRef.current) {
      return;
    }
    if (displayedCountRef.current < targetCharsRef.current.length) {
      return;
    }
    settledNotifiedRef.current = true;
    onSettledRef.current?.();
  }, []);

  const syncImmediate = useCallback((nextText: string): void => {
    if (rafRef.current !== undefined) {
      window.cancelAnimationFrame(rafRef.current);
      rafRef.current = undefined;
    }
    lastFrameTsRef.current = undefined;
    targetRef.current = nextText;
    targetCharsRef.current = [...nextText];
    displayedCountRef.current = targetCharsRef.current.length;
    displayedTextRef.current = nextText;
    settledNotifiedRef.current = false;
    setDisplayedText(nextText);
    onFrameRef.current?.();
  }, []);

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
      displayedTextRef.current = nextText;
      setDisplayedText(nextText);
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
      targetRef.current = nextText;
      targetCharsRef.current = [...nextText];
      settledNotifiedRef.current = false;
      startFrameLoop();
    };

    applyTarget(streamTextStore.get(streamKey));
    const unsubscribe = streamTextStore.subscribe(streamKey, applyTarget);
    return () => {
      unsubscribe();
      if (rafRef.current !== undefined) {
        window.cancelAnimationFrame(rafRef.current);
        rafRef.current = undefined;
      }
    };
  }, [initialText, startFrameLoop, streamKey, syncImmediate]);

  useEffect(() => {
    notifySettled();
  }, [final, notifySettled]);

  return displayedText;
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

function richBlockContent(block: RichBlockWithOffset): string {
  switch (block.kind) {
    case "paragraph":
      return block.text;
    case "image":
      return `${block.source}\n${block.alt ?? ""}`;
    case "code":
      return `${block.language}\n${block.code}`;
    case "mermaid":
      return block.code;
  }
}
