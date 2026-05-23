import { useCallback, useEffect, useRef, useState } from "react";
import {
  MarkdownContent,
  imageTarget,
  parseRichBlocksWithOffsets,
  resolveImageSource,
  type RichBlockWithOffset
} from "./RichContent";
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

type SettledMarkdown = {
  streamKey: string;
  text: string;
};

type StreamingBlockView = {
  key: string;
  kind: RichBlockWithOffset["kind"];
  element: HTMLElement;
  textNode?: Text;
  codeNode?: HTMLElement;
  content: string;
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
  onFrame,
  onSettled
}: StreamingMarkdownProps): JSX.Element {
  const [settled, setSettled] = useState<SettledMarkdown | undefined>(() =>
    !live && final ? { streamKey, text: initialText } : undefined
  );

  useEffect(() => {
    if (!live && final) {
      setSettled({ streamKey, text: initialText });
      return;
    }
    setSettled(undefined);
  }, [final, initialText, live, streamKey]);

  const handleSettled = useCallback(
    (text: string) => {
      setSettled({ streamKey, text });
      onSettled?.();
    },
    [onSettled, streamKey]
  );

  if (final && settled?.streamKey === streamKey) {
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
  const rootRef = useRef<HTMLDivElement | null>(null);
  const viewsRef = useRef<StreamingBlockView[]>([]);
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

  const renderText = useCallback(
    (text: string): void => {
      if (!rootRef.current) {
        return;
      }
      renderStreamingBlocks(rootRef.current, viewsRef.current, text, cwd);
    },
    [cwd]
  );

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
    (nextText: string): void => {
      if (rafRef.current !== undefined) {
        window.cancelAnimationFrame(rafRef.current);
        rafRef.current = undefined;
      }
      lastFrameTsRef.current = undefined;
      targetRef.current = nextText;
      targetCharsRef.current = [...nextText];
      displayedCountRef.current = targetCharsRef.current.length;
      settledNotifiedRef.current = false;
      renderText(nextText);
      onFrameRef.current?.();
      notifySettled();
    },
    [notifySettled, renderText]
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
      renderText(nextText);
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
  }, [notifySettled, renderText]);

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
      clearStreamingBlocks(rootRef.current, viewsRef.current);
    };
  }, [initialText, live, startFrameLoop, streamKey, syncImmediate]);

  return <div ref={rootRef} className={className} data-stream-state={final ? "settling" : "streaming"} />;
}

function renderStreamingBlocks(
  root: HTMLElement,
  views: StreamingBlockView[],
  text: string,
  cwd: string | undefined
): void {
  if (!text) {
    clearStreamingBlocks(root, views);
    return;
  }

  const blocks = parseRichBlocksWithOffsets(text, { allowOpenFence: true });
  let firstChanged = 0;
  while (firstChanged < blocks.length && firstChanged < views.length && blockViewMatches(views[firstChanged], blocks[firstChanged])) {
    firstChanged += 1;
  }

  for (let index = views.length - 1; index >= firstChanged; index -= 1) {
    views[index].element.remove();
    views.pop();
  }

  for (let index = firstChanged; index < blocks.length; index += 1) {
    const view = createStreamingBlockView(blocks[index], index === blocks.length - 1, cwd);
    views.push(view);
    root.append(view.element);
  }

  for (let index = 0; index < blocks.length; index += 1) {
    updateStreamingBlockView(views[index], blocks[index], index === blocks.length - 1, cwd);
  }
}

function clearStreamingBlocks(root: HTMLElement | null, views: StreamingBlockView[]): void {
  views.length = 0;
  if (root) {
    root.textContent = "";
  }
}

function blockViewMatches(view: StreamingBlockView, block: RichBlockWithOffset): boolean {
  return view.key === blockViewKey(block) && view.kind === block.kind;
}

function createStreamingBlockView(
  block: RichBlockWithOffset,
  active: boolean,
  cwd: string | undefined
): StreamingBlockView {
  switch (block.kind) {
    case "paragraph": {
      const element = document.createElement("p");
      const textNode = document.createTextNode("");
      element.append(textNode);
      return updateStreamingBlockView(
        { key: blockViewKey(block), kind: block.kind, element, textNode, content: "" },
        block,
        active,
        cwd
      );
    }
    case "image": {
      const element = document.createElement("figure");
      const image = document.createElement("img");
      element.append(image);
      return updateStreamingBlockView(
        { key: blockViewKey(block), kind: block.kind, element, content: "" },
        block,
        active,
        cwd
      );
    }
    case "code":
    case "mermaid": {
      const element = document.createElement("pre");
      const codeNode = document.createElement("code");
      element.append(codeNode);
      return updateStreamingBlockView(
        { key: blockViewKey(block), kind: block.kind, element, codeNode, content: "" },
        block,
        active,
        cwd
      );
    }
  }
}

function updateStreamingBlockView(
  view: StreamingBlockView,
  block: RichBlockWithOffset,
  active: boolean,
  cwd: string | undefined
): StreamingBlockView {
  view.element.className = streamingBlockClassName(block, active);

  switch (block.kind) {
    case "paragraph":
      if (view.content !== block.text && view.textNode) {
        view.textNode.nodeValue = block.text;
      }
      view.content = block.text;
      break;
    case "image": {
      const source = imageTarget(block.source);
      const content = `${source}\n${block.alt ?? ""}`;
      if (view.content !== content) {
        const image = view.element.firstElementChild instanceof HTMLImageElement ? view.element.firstElementChild : null;
        if (image) {
          image.className = "rich-image";
          image.src = resolveImageSource(source, cwd);
          image.alt = block.alt ?? "";
          image.title = source;
          image.loading = "lazy";
        }
      }
      view.content = content;
      break;
    }
    case "code":
    case "mermaid": {
      const language = block.kind === "code" ? block.language : "mermaid";
      if (language) {
        view.element.dataset.language = language;
      } else {
        delete view.element.dataset.language;
      }
      if (view.content !== block.code && view.codeNode) {
        view.codeNode.textContent = block.code;
      }
      view.content = block.code;
      break;
    }
  }

  return view;
}

function streamingBlockClassName(block: RichBlockWithOffset, active: boolean): string {
  switch (block.kind) {
    case "paragraph":
      return active ? "rich-paragraph stream-paragraph" : "rich-paragraph";
    case "image":
      return "rich-image-block";
    case "code":
    case "mermaid":
      return active ? "rich-code stream-code" : "rich-code";
  }
}

function blockViewKey(block: RichBlockWithOffset): string {
  return `${block.startOffset}-${block.kind}`;
}
