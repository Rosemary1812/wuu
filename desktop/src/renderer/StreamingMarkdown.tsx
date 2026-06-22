import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState
} from "react";
import { MarkdownContent } from "./RichContent";
import {
  useStreamedText
} from "./StreamText";

/**
 * Progressive Markdown renderer used while assistant text is arriving.
 *
 * Single source of truth: the parent owns `isLive`, derived from the
 * back-end thread item. We do not maintain an internal
 * streaming/settling/settled state machine — `isLive` flips the renderer
 * between two modes:
 *   - `isLive=true`:  RAF loop reveals characters one at a time.
 *   - `isLive=false`: text is rendered in full immediately.
 *
 * The caret is owned by Streamdown: we pass `isLive` through to the
 * tail's `MarkdownContent`, and Streamdown shows or hides its caret
 * accordingly. The surface-level phase is for styling hooks only —
 * commentary and final-answer text share the same rendering path so a
 * late phase resolution never causes a typography jump.
 */
type StreamingMarkdownProps = {
  streamKey: string;
  initialText?: string;
  cwd?: string;
  className?: string;
  /** Whether the source item is still receiving deltas. */
  isLive: boolean;
  /**
   * The thread item phase. It is semantic metadata for the parent layout;
   * this renderer keeps commentary and final-answer text visually identical.
   */
  phase: "commentary" | "final_answer";
  onFrame?: () => void;
  onSettled?: () => void;
};

type StreamPhase = "streaming" | "settled";

const DEFAULT_CLASS_NAME = "streaming-markdown rich-content";

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
  isLive,
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
  // Single internal phase: streaming while upstream is live, settled once
  // it isn't. The back-end message phase never gates rendering of the text.
  const phase: StreamPhase = isLive ? "streaming" : "settled";

  /* ------------------------- Visible character cursor -------------------- */
  const [visibleLength, setVisibleLength] = useState<number>(initialText.length);

  /* ------------------------------- Refs ---------------------------------- */
  const renderedTextRef = useRef(renderedText);
  const visibleRef = useRef(visibleLength);
  const rafRef = useRef<number | undefined>(undefined);
  const lastFrameTsRef = useRef<number | undefined>(undefined);
  const isLiveRef = useRef(isLive);
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
    isLiveRef.current = isLive;
    onFrameRef.current = onFrame;
    onSettledRef.current = onSettled;
  }, [isLive, onFrame, onSettled]);

  /* -------------------------- Settle notification ------------------------ */
  // Fire `onSettled` once the upstream is no longer live AND the visible
  // cursor has caught up to the target text. The parent uses this to drop
  // any external "live" tracking (we don't manage it ourselves anymore).
  const trySettle = useCallback((): void => {
    if (settledNotifiedRef.current) return;
    if (isLiveRef.current) return;
    if (visibleRef.current < renderedTextRef.current.length) return;
    settledNotifiedRef.current = true;
    onSettledRef.current?.();
  }, []);

  // If `isLive` arrives after the cursor has already caught up, there is
  // no animation frame left to trigger the callback.
  useEffect(() => {
    trySettle();
  }, [isLive, renderedText, trySettle]);

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
    if (!isLiveRef.current) {
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
  }, [renderedText, isLive, startFrameLoop, syncImmediate, trySettle]);

  /* --------- Effect: settled path skips the RAF chase -------------------- */
  // The legacy implementation gated "settled" on the RAF loop catching up
  // to `renderedText`, which made long commentary items (1000+ chars)
  // appear to keep streaming for seconds after the upstream went idle. We
  // now treat `isLive=false` as an authoritative settle signal: snap the
  // visible cursor to the end of the text immediately and let `trySettle`
  // fire `onSettled` in the next effect tick.
  useEffect(() => {
    if (isLive) return;
    const text = renderedTextRef.current;
    if (visibleRef.current < text.length) {
      syncImmediate(text);
    }
    settledNotifiedRef.current = false;
    trySettle();
  }, [isLive, syncImmediate, trySettle]);

  /* ------------------------- Derived view data -------------------------- */
  const visibleText = renderedText.slice(0, visibleLength);
  // The caret is shown for any live surface: while the back-end is still
  // pushing deltas, and while the visible character cursor is still
  // catching up to the latest target. Once both flip, the caret goes away
  // because Streamdown drops it as soon as `isLive` returns false.
  const tailIsLive = isLive || visibleLength < renderedText.length;

  // Split the visible text into stable blocks + an open tail. Every
  // stable block is its own memoized markdown surface, so promoting a
  // new block from the tail only costs that single block's parse — the
  // earlier blocks stay mounted as-is. This caps per-tick work at
  // O(tail) and per-promotion work at O(one block), independent of the
  // total answer length.
  //
  // The settled phase uses the same split as streaming: keeping the
  // block layout stable across the streaming → settled transition means
  // React reconciles by updating props in place instead of unmounting
  // every previously memoized block and remounting one big tail. That
  // reconciliation jump is what caused the visible "settle flick" on
  // long answers (block-level memo would be wiped the instant the
  // upstream went idle).
  const split = useMemo(
    () => splitIntoStableBlocks(visibleText),
    [visibleText]
  );

  /* ------------------------------- Render -------------------------------- */
  return (
    <div
      className={className}
      data-stream-state={phase}
    >
      {split.blocks.map((block, index) => (
        // Wrap each stable block in a `.streaming-markdown-block` so the
        // off-screen content-visibility optimization (see turns.css) can
        // skip layout/paint for blocks the user has scrolled past. The
        // wrapper carries the React list key; the inner memoized
        // MarkdownContent keeps the existing block-level memoization
        // contract intact.
        <div className="streaming-markdown-block" key={index}>
          <MarkdownContent text={block} cwd={cwd} isLive={false} />
        </div>
      ))}
      <MarkdownContent text={split.tail} cwd={cwd} isLive={tailIsLive} />
    </div>
  );
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
