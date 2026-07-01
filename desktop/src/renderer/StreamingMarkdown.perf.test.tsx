/**
 * Per-tick render cost for StreamingMarkdown.
 *
 * The streaming surface re-renders on every visible-character advance.
 * Before block-level memoization, that cost scaled linearly with the
 * total answer length and made the main thread freeze on long
 * responses. The contract enforced here:
 *
 *   - average per-tick render stays bounded regardless of total length
 *   - p99 per-tick render stays under one frame (16ms)
 *
 * These thresholds are deliberately generous so CI machines under load
 * don't flake. The real-world budget is much tighter.
 */
import { afterEach, describe, expect, it } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { StreamingMarkdown } from "./StreamingMarkdown";
import { streamTextKey, streamTextStore } from "./StreamText";

const sectionTemplate = `## 标题 N

这是一个 \`useEffect\` 段落，包含 **粗体**、*斜体* 以及 [链接](https://example.com)。
依赖数组决定了 effect 何时重新执行。

\`\`\`ts
useEffect(() => {
  const id = setInterval(() => count++, 1000);
  return () => clearInterval(id);
}, []);
\`\`\`

- 第一项：列表里再嵌一段 \`code\`
- 第二项：lint 会警告依赖
- 第三项：每次都是新的引用

> 引用块：99% 的问题都是"我以为它没变，其实它变了"。

`;

// Long-ish answer (~12 000 chars) — well past anything a typical
// response would produce. If this stays smooth, shorter answers are
// trivially fine.
const longText = Array.from({ length: 40 }, (_, i) =>
  sectionTemplate.replace("N", String(i + 1))
).join("");

let root: Root | null = null;
let container: HTMLDivElement | null = null;

afterEach(() => {
  if (root) {
    act(() => { root!.unmount(); });
    root = null;
  }
  if (container) {
    container.remove();
    container = null;
  }
  streamTextStore.clearItem("turn", "perf");
});

describe("StreamingMarkdown perf", () => {
  it("keeps per-tick render cost bounded for long answers", async () => {
    const key = streamTextKey("turn", "perf", "text");
    streamTextStore.seed(key, longText);
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);

    // Drive the frame loop manually so we can measure each tick.
    const realRAF = window.requestAnimationFrame;
    const pending: FrameRequestCallback[] = [];
    let nextHandle = 1;
    const renderDurations: number[] = [];

    window.requestAnimationFrame = ((cb: FrameRequestCallback) => {
      pending.push(cb);
      return nextHandle++;
    }) as typeof window.requestAnimationFrame;

    act(() => {
      root!.render(
        <StreamingMarkdown
          streamKey={key}
          initialText=""
          isLive={true}
          phase="final_answer"
        />
      );
    });

    let tickCount = 0;
    while (pending.length > 0 && tickCount < 5000) {
      const cb = pending.shift()!;
      const ts = (tickCount + 1) * 16; // simulate 60fps
      const start = performance.now();
      await act(async () => {
        cb(ts);
      });
      renderDurations.push(performance.now() - start);
      tickCount++;
    }

    window.requestAnimationFrame = realRAF;

    expect(renderDurations.length).toBeGreaterThan(100);

    const sorted = renderDurations.slice().sort((a, b) => a - b);
    const total = renderDurations.reduce((s, v) => s + v, 0);
    const avg = total / renderDurations.length;
    const p50 = sorted[Math.floor(sorted.length * 0.5)];
    const p95 = sorted[Math.floor(sorted.length * 0.95)];
    const p99 = sorted[Math.floor(sorted.length * 0.99)];
    const max = sorted[sorted.length - 1];

    // Log for humans tuning the streamer.
    // eslint-disable-next-line no-console
    console.log(
      `\n=== StreamingMarkdown perf (${longText.length} chars, ${tickCount} ticks) ===\n` +
        `  avg: ${avg.toFixed(2)}ms\n` +
        `  p50: ${p50.toFixed(2)}ms\n` +
        `  p95: ${p95.toFixed(2)}ms\n` +
        `  p99: ${p99.toFixed(2)}ms\n` +
        `  max: ${max.toFixed(2)}ms\n` +
        `  total: ${total.toFixed(0)}ms`
    );

    // Hard ceilings. These are deliberately loose so they catch
    // catastrophic regressions (the kind that froze the UI before
    // block memoization) without flaking under CI noise.
    expect(avg).toBeLessThan(4);
    expect(p99).toBeLessThan(16);
  }, 15000);
});
