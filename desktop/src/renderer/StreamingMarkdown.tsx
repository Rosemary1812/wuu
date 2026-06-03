import {
  memo,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState
} from "react";
import { MarkdownContent, type RichTextRenderer } from "./RichContent";
import {
  useStreamedText
} from "./StreamText";

/**
 * Progressive Markdown renderer used while assistant text is arriving.
 * The stream reveals text with a small inline cursor and keeps the final
 * markup on the same surface so completion does not cause a layout swap.
 */
type StreamingMarkdownProps = {
  streamKey: string;
  initialText?: string;
  cwd?: string;
  className?: string;
  /** When true, the upstream has finished writing. The component will
   *  drain the visible cursor to the end of the target text and then
   *  fire `onSettled`. */
  final?: boolean;
  /** When true, the surface is "live" — it animates the visible cursor
   *  toward the target. When false, the surface renders the full text
   *  immediately with no animation. */
  live?: boolean;
  onFrame?: () => void;
  onSettled?: () => void;
};

type StreamPhase = "streaming" | "settling" | "settled";

const DEFAULT_CLASS_NAME = "streaming-markdown rich-content";
const CURSOR_CLASS_NAME = "stream-cursor";
const CURSOR_SENTINEL = "\uE000";

const STREAM_CONFIG = {
  /** Calm default rate used while the reader is keeping up. */
  baseCps: 72,
  /** Rate when the upstream is ahead of the cursor (catch-up). */
  burstCps: 220,
  /** Hard ceiling on the catch-up rate. */
  maxCps: 360,
  /** Floor that keeps the cursor moving even on very small backlogs. */
  minCps: 28,
  /** Allow a small lead-in so the next character is ready before reveal. */
  targetLagChars: 6,
  /** Cap a single frame's reveal count so we never skip characters. */
  maxRevealPerFrame: 6
} as const;

export function StreamingMarkdown({
  streamKey,
  initialText = "",
  cwd,
  className = DEFAULT_CLASS_NAME,
  final = false,
  live = !final,
  onFrame,
  onSettled
}: StreamingMarkdownProps): JSX.Element {
  /* ------------------------- External store wiring ------------------------ */
  const targetText = useStreamedText(streamKey, initialText);

  /* ----------------------------- Sticky text ------------------------------ */
  // The text we actually render. The store may be cleared (in `onSettled`)
  // before the parent unmounts us, so the hook falls back to `initialText`
  // instead of blanking the visible message.
  const [renderedText, setRenderedText] = useState(targetText);
  useLayoutEffect(() => {
    if (targetText !== renderedText) {
      setRenderedText(targetText);
    }
  }, [targetText, renderedText]);

  /* ------------------------------ Phase ----------------------------------- */
  const phase: StreamPhase = final
    ? live ? "settling" : "settled"
    : "streaming";

  /* ------------------------- Visible character cursor -------------------- */
  const [visibleLength, setVisibleLength] = useState<number>(initialText.length);

  /* --------------------- Cursor lifecycle (shown -> fading -> gone) ------- */
  const [cursorState, setCursorState] = useState<"shown" | "fading" | "gone">("shown");

  /* ------------------------------- Refs ---------------------------------- */
  const renderedTextRef = useRef(renderedText);
  const visibleRef = useRef(visibleLength);
  const rafRef = useRef<number | undefined>(undefined);
  const lastFrameTsRef = useRef<number | undefined>(undefined);
  const finalRef = useRef(final);
  const liveRef = useRef(live);
  const onFrameRef = useRef(onFrame);
  const onSettledRef = useRef(onSettled);
  const settledNotifiedRef = useRef(false);

  /* ----------------------- Refs always track props ------------------------ */
  useLayoutEffect(() => {
    renderedTextRef.current = renderedText;
  }, [renderedText]);
  useLayoutEffect(() => {
    visibleRef.current = visibleLength;
  }, [visibleLength]);
  useLayoutEffect(() => {
    finalRef.current = final;
    liveRef.current = live;
    onFrameRef.current = onFrame;
    onSettledRef.current = onSettled;
  }, [final, live, onFrame, onSettled]);

  /* -------------------------- Settle notification ------------------------ */
  // Fire `onSettled` once the upstream is final and the visible cursor has
  // caught up. The parent uses this callback to turn `live` off and clear the
  // stream cache.
  const trySettle = useCallback((): void => {
    if (settledNotifiedRef.current) return;
    if (!finalRef.current || !liveRef.current) return;
    if (visibleRef.current < renderedTextRef.current.length) return;
    settledNotifiedRef.current = true;
    onSettledRef.current?.();
  }, []);

  // If `final` arrives after the cursor has already caught up, there is no
  // animation frame left to trigger the callback.
  useEffect(() => {
    trySettle();
  }, [final, live, renderedText, trySettle]);

  /* --------------------------- Sync / RAF loop --------------------------- */
  // Snap visible to text length without animation. Used when the surface
  // goes non-live (e.g. unmounting, or an out-of-band text replacement).
  const syncImmediate = useCallback((text: string): void => {
    if (rafRef.current !== undefined) {
      window.cancelAnimationFrame(rafRef.current);
      rafRef.current = undefined;
    }
    lastFrameTsRef.current = undefined;
    visibleRef.current = text.length;
    setVisibleLength(text.length);
    settledNotifiedRef.current = false;
  }, []);

  // The frame loop. Advances `visible` toward the target at a rate
  // proportional to the backlog. We never skip characters: each frame
  // reveals at most `maxRevealPerFrame` characters.
  const startFrameLoop = useCallback((): void => {
    if (rafRef.current !== undefined) return;
    const tick = (ts: number): void => {
      const text = renderedTextRef.current;
      const targetLen = text.length;
      const current = visibleRef.current;
      if (current >= targetLen) {
        rafRef.current = undefined;
        lastFrameTsRef.current = undefined;
        trySettle();
        return;
      }
      const lastTs = lastFrameTsRef.current ?? ts;
      lastFrameTsRef.current = ts;
      // Clamp delta so a stalled tab cannot reveal a huge burst in one
      // step.
      const deltaSeconds = Math.max(0.001, Math.min((ts - lastTs) / 1000, 0.05));
      const backlog = targetLen - current;
      const lagged = Math.max(0, backlog - STREAM_CONFIG.targetLagChars);
      const cps = Math.min(
        STREAM_CONFIG.maxCps,
        Math.max(
          STREAM_CONFIG.minCps,
          lagged > 0 ? STREAM_CONFIG.burstCps : STREAM_CONFIG.baseCps
        )
      );
      const revealCount = Math.max(
        1,
        Math.min(
          backlog,
          STREAM_CONFIG.maxRevealPerFrame,
          Math.ceil(cps * deltaSeconds)
        )
      );
      const next = Math.min(targetLen, current + revealCount);
      visibleRef.current = next;
      setVisibleLength(next);
      onFrameRef.current?.();
      if (next >= targetLen) {
        rafRef.current = undefined;
        lastFrameTsRef.current = undefined;
        trySettle();
        return;
      }
      rafRef.current = window.requestAnimationFrame(tick);
    };
    rafRef.current = window.requestAnimationFrame(tick);
  }, [trySettle]);

  /* -------------------- Effect: keep target in sync --------------------- */
  useEffect(() => {
    // Non-live surfaces render the latest text without animation.
    if (!liveRef.current) {
      syncImmediate(renderedTextRef.current);
      return undefined;
    }
    const text = renderedTextRef.current;
    // If the upstream shrank the text (e.g. `*/replace`), clamp the
    // visible cursor so we never overshoot.
    if (visibleRef.current > text.length) {
      syncImmediate(text);
      trySettle();
      return undefined;
    }
    if (visibleRef.current < text.length) {
      settledNotifiedRef.current = false;
      startFrameLoop();
    } else {
      // Already caught up; settle if appropriate.
      trySettle();
    }
    return () => {
      if (rafRef.current !== undefined) {
        window.cancelAnimationFrame(rafRef.current);
        rafRef.current = undefined;
      }
    };
  }, [renderedText, live, startFrameLoop, syncImmediate, trySettle]);

  /* --------------------- Cursor visibility & fade-out ------------------- */
  const hasMoreToReveal = visibleLength < renderedText.length;
  useEffect(() => {
    if (hasMoreToReveal) {
      // Still streaming: keep the cursor visible.
      setCursorState("shown");
      return;
    }
    if (!live) {
      // Caught up and not live: fade out, then remove from DOM.
      setCursorState("fading");
      const t = window.setTimeout(() => {
        setCursorState("gone");
      }, 200);
      return () => window.clearTimeout(t);
    }
    // Caught up but still live: keep visible while we wait for more.
    setCursorState("shown");
  }, [hasMoreToReveal, live]);

  /* ------------------------- Derived view data -------------------------- */
  const visibleText = renderedText.slice(0, visibleLength);
  const showCursor = cursorState !== "gone";
  const cursorClassName =
    CURSOR_CLASS_NAME + (cursorState === "fading" ? " is-fading" : "");
  const cursorTextRenderer = useMemo(
    () => showCursor ? createCursorTextRenderer(cursorClassName) : undefined,
    [cursorClassName, showCursor]
  );
  // Mermaid is expensive; defer until the stream settles.
  const renderMermaid = phase === "settled";

  // Split the visible text into stable blocks + an open tail. Every
  // stable block is its own memoized markdown surface, so promoting a
  // new block from the tail only costs that single block's parse — the
  // earlier blocks stay mounted as-is. This caps per-tick work at
  // O(tail) and per-promotion work at O(one block), independent of the
  // total answer length.
  //
  // Once the stream is settled, we render the whole text in one pass so
  // the final DOM matches the non-streaming layout exactly.
  const split = useMemo(
    () =>
      phase === "settled"
        ? { blocks: [] as string[], tail: visibleText }
        : splitIntoStableBlocks(visibleText),
    [phase, visibleText]
  );
  const tailText = showCursor ? `${split.tail}${CURSOR_SENTINEL}` : split.tail;

  /* ------------------------------- Render -------------------------------- */
  return (
    <div
      className={className}
      data-stream-state={phase}
    >
      {split.blocks.map((block, index) => (
        <MemoMarkdownContent
          // Stable blocks are append-only; once at index N, never moves.
          key={index}
          text={block}
          cwd={cwd}
          renderMermaid={renderMermaid}
        />
      ))}
      <MarkdownContent
        text={tailText}
        cwd={cwd}
        renderText={cursorTextRenderer}
        renderMermaid={renderMermaid}
      />
    </div>
  );
}

/**
 * Memoized markdown surface. Stable blocks are passed in by value;
 * React.memo's default shallow compare on the `text` string is exactly
 * what we want — identical text means identical render.
 */
const MemoMarkdownContent = memo(MarkdownContent);

function createCursorTextRenderer(cursorClassName: string): RichTextRenderer {
  return (text, keyPrefix) => {
    if (!text.includes(CURSOR_SENTINEL)) {
      return [text];
    }
    const output: Array<JSX.Element | string> = [];
    const parts = text.split(CURSOR_SENTINEL);
    parts.forEach((part, index) => {
      if (part) {
        output.push(part);
      }
      if (index < parts.length - 1) {
        output.push(
          <span
            key={`${keyPrefix}-cursor-${index}`}
            className={cursorClassName}
            aria-hidden="true"
          />
        );
      }
    });
    return output;
  };
}

/**
 * Split `text` into a sequence of "stable" markdown blocks plus an
 * open tail. A block is everything between two blank-line boundaries
 * (`\n\n`). Blocks inside an unclosed fenced code section are deferred
 * to the tail — they aren't yet stable.
 *
 * Each stable block has self-contained markdown semantics: prepending
 * or appending more text to the overall document cannot change how the
 * block parses. That property is what makes block-level memoization
 * safe.
 *
 * Exported for tests.
 */
export function splitIntoStableBlocks(
  text: string
): { blocks: string[]; tail: string } {
  const blocks: string[] = [];
  let inFence = false;
  let blockStart = 0;
  let lineStart = 0;
  for (let i = 0; i < text.length; i += 1) {
    const ch = text.charCodeAt(i);
    if (ch === 10 /* \n */) {
      if (!inFence && i + 1 < text.length && text.charCodeAt(i + 1) === 10) {
        // Found the end of a stable block — include the trailing
        // double newline so block separation is preserved.
        const end = i + 2;
        blocks.push(text.slice(blockStart, end));
        blockStart = end;
      }
      lineStart = i + 1;
      continue;
    }
    if (
      ch === 96 /* ` */ &&
      i === lineStart &&
      i + 2 < text.length &&
      text.charCodeAt(i + 1) === 96 &&
      text.charCodeAt(i + 2) === 96
    ) {
      inFence = !inFence;
      i += 2;
    }
  }
  return { blocks, tail: text.slice(blockStart) };
}
