const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { app, BrowserWindow } = require("electron");

const desktopRoot = path.resolve(__dirname, "..");
const repoRoot = path.resolve(desktopRoot, "..");
const rendererHtml = path.join(desktopRoot, "out", "renderer", "index.html");
const preload = path.join(__dirname, "streaming-e2e-preload.cjs");

process.env.WUU_STREAM_E2E_CWD = repoRoot;
app.commandLine.appendSwitch("disable-gpu");
app.commandLine.appendSwitch("disable-software-rasterizer");

app.whenReady().then(run).catch(fail);

async function run() {
  assert.ok(fs.existsSync(rendererHtml), "Renderer build is missing. Run npm run build first.");
  assert.ok(fs.existsSync(preload), "Streaming E2E preload is missing.");

  const win = new BrowserWindow({
    width: 1100,
    height: 820,
    show: false,
    webPreferences: {
      contextIsolation: true,
      nodeIntegration: false,
      preload,
      sandbox: false
    }
  });

  win.webContents.on("render-process-gone", (_event, details) => {
    fail(new Error(`Renderer process exited: ${details.reason}`));
  });
  win.webContents.on("console-message", (_event, level, message, line, sourceId) => {
    if (level >= 2) {
      console.error(`renderer console: ${message} (${sourceId}:${line})`);
    }
  });

  await loadFile(win, rendererHtml);
  await waitFor(win, () => Boolean(document.querySelector(".conversation-pane")), 5000);
  await waitFor(win, () => Boolean(document.querySelector(".composer textarea")), 5000);

  const imeEnter = await evaluate(win, () => {
    const textarea = document.querySelector(".composer textarea");
    if (!(textarea instanceof HTMLTextAreaElement)) {
      throw new Error("Composer textarea not found.");
    }
    textarea.focus();
    const composingEnter = new KeyboardEvent("keydown", {
      key: "Enter",
      code: "Enter",
      bubbles: true,
      cancelable: true,
      isComposing: true
    });
    const composingDispatchResult = textarea.dispatchEvent(composingEnter);
    const plainEnter = new KeyboardEvent("keydown", {
      key: "Enter",
      code: "Enter",
      bubbles: true,
      cancelable: true
    });
    const plainDispatchResult = textarea.dispatchEvent(plainEnter);
    return {
      composingPrevented: composingEnter.defaultPrevented,
      composingDispatchResult,
      plainPrevented: plainEnter.defaultPrevented,
      plainDispatchResult
    };
  });
  assert.equal(imeEnter.composingPrevented, false, "IME Enter should not be intercepted by the composer.");
  assert.equal(imeEnter.composingDispatchResult, true, "IME Enter should remain available to the input method.");
  assert.equal(imeEnter.plainPrevented, true, "Plain Enter should still trigger the composer send shortcut.");
  assert.equal(imeEnter.plainDispatchResult, false, "Plain Enter should be consumed by the composer send shortcut.");

  const immediateSendStarted = await waitFor(
    win,
    () => {
      const textarea = document.querySelector(".composer textarea");
      if (!(textarea instanceof HTMLTextAreaElement)) {
        return false;
      }
      textarea.focus();
      const valueSetter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")?.set;
      valueSetter?.call(textarea, "Rename me immediately.");
      textarea.dispatchEvent(new Event("input", { bubbles: true }));
      const enter = new KeyboardEvent("keydown", {
        key: "Enter",
        code: "Enter",
        bubbles: true,
        cancelable: true
      });
      textarea.dispatchEvent(enter);
      return enter.defaultPrevented;
    },
    3000
  );
  assert.equal(immediateSendStarted, true, "Composer send should start an e2e thread.");
  const immediateTitle = await waitFor(
    win,
    () => {
      const title = window.e2eActiveTitleText();
      return title.includes("Rename me immediately.") ? title : null;
    },
    3000
  );
  assert.equal(immediateTitle, "Rename me immediately.", "Fresh conversation title should update from the first user turn.");

  const now = new Date().toISOString();
  const resetStarted = await evaluate(win, () => {
    const button = document.querySelector(".primary-nav .nav-item");
    if (!(button instanceof HTMLButtonElement)) {
      return false;
    }
    button.click();
    return true;
  });
  assert.equal(resetStarted, true, "New conversation button should be available after title update.");
  await waitFor(win, () => window.e2eActiveTitleText() === "新对话", 3000);

  const streamingThreadStarted = await waitFor(
    win,
    () => {
      const textarea = document.querySelector(".composer textarea");
      if (!(textarea instanceof HTMLTextAreaElement)) {
        return false;
      }
      textarea.focus();
      const valueSetter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")?.set;
      valueSetter?.call(textarea, "Write a long streaming response.");
      textarea.dispatchEvent(new Event("input", { bubbles: true }));
      const enter = new KeyboardEvent("keydown", {
        key: "Enter",
        code: "Enter",
        bubbles: true,
        cancelable: true
      });
      textarea.dispatchEvent(enter);
      return enter.defaultPrevented;
    },
    3000
  );
  assert.equal(streamingThreadStarted, true, "Streaming e2e should create an active thread before server events.");
  await waitFor(
    win,
    () => window.e2eActiveTitleText().includes("Write a long streaming response."),
    3000
  );

  const thread = {
    id: "thread-streaming-e2e",
    preview: "Streaming E2E",
    model_provider: "e2e",
    model: "mock-stream",
    cwd: repoRoot,
    status: "in_progress",
    created_at: now,
    updated_at: now,
    turns: []
  };
  const userItem = {
    id: "user-streaming-e2e",
    type: "user_message",
    status: "completed",
    text: "Write a long streaming response."
  };
  const agentItem = {
    id: "agent-streaming-e2e",
    type: "agent_message",
    status: "in_progress",
    text: ""
  };
  const reasoningItem = {
    id: "reasoning-streaming-e2e",
    type: "reasoning",
    status: "completed",
    text: "Check the live turn state before producing the final answer."
  };
  const toolItem = {
    id: "tool-streaming-e2e",
    type: "tool_call",
    status: "completed",
    name: "grep",
    arguments: '{"pattern":"streaming-word","path":"desktop/src/renderer"}',
    result: "desktop/src/renderer/App.tsx: streaming-word"
  };
  const turn = {
    id: "turn-streaming-e2e",
    items: [userItem],
    items_view: "full",
    status: "in_progress",
    started_at: now
  };
  const longTail = Array.from({ length: 120 }, (_value, index) => `streaming-word-${index}`).join(" ");
  const fullText = [
    "# Streaming markdown",
    "",
    "Intro with **bold text** and [a link](https://example.com).",
    "",
    "- first item",
    "- [x] completed item",
    "",
    "| Name | Value |",
    "| --- | --- |",
    "| alpha | beta |",
    "",
    "```ts",
    "const answer = 42;",
    "```",
    "",
    longTail
  ].join("\n");

  emitNotification(win, "thread/resumed", { thread });
  emitNotification(win, "turn/started", { thread_id: thread.id, turn });
  emitNotification(win, "item/completed", { thread_id: thread.id, turn_id: turn.id, item: reasoningItem });
  emitNotification(win, "item/completed", { thread_id: thread.id, turn_id: turn.id, item: toolItem });
  emitNotification(win, "item/started", { thread_id: thread.id, turn_id: turn.id, item: agentItem });
  emitNotification(win, "item/agentMessage/delta", { thread_id: thread.id, turn_id: turn.id, item_id: agentItem.id, delta: fullText });

  await waitFor(win, () => Boolean(document.querySelector(".agent-text .streaming-markdown")), 3000);
  const partial = await waitFor(
    win,
    () => {
      const snapshot = streamingSnapshot();
      return snapshot.hasStreaming && snapshot.textLength > 0 && snapshot.textLength < window.__STREAMING_E2E_FULL_LENGTH__
        ? snapshot
        : null;
    },
    3000,
    { fullLength: fullText.length }
  );
  assert.equal(partial.hasStaticFallback, false, "Assistant content should not switch to static RichContent fallback.");

  const debug = await waitFor(
    win,
    () => {
      const button = document.querySelector(".run-debug-button");
      if (!button) {
        return null;
      }
      if (!document.querySelector(".run-debug-panel")) {
        button.click();
      }
      const panel = document.querySelector(".run-debug-panel");
      const text = panel?.textContent ?? "";
      return text.includes("reply/first-delta") && text.includes("正在生成回复")
        ? {
            phase: panel?.querySelector(".run-debug-phase")?.textContent ?? "",
            text
          }
        : null;
    },
    3000,
    { fullLength: fullText.length }
  );
  assert.equal(debug.phase, "正在生成回复", "Debug panel should expose the live response phase.");
  assert.match(debug.text, /reply\/first-delta/, "Debug panel should include the first reply delta event.");

  const newThreadEnabledWhileRunning = await evaluate(win, () => {
    const button = document.querySelector(".primary-nav .nav-item");
    return button instanceof HTMLButtonElement ? !button.disabled : false;
  });
  assert.equal(newThreadEnabledWhileRunning, true, "New conversation should stay enabled while the active thread is running.");

  emitNotification(win, "item/completed", {
    thread_id: thread.id,
    turn_id: turn.id,
    item: { ...agentItem, status: "completed", text: fullText }
  });
  emitNotification(win, "turn/completed", {
    thread_id: thread.id,
    turn: {
      ...turn,
      status: "completed",
      completed_at: new Date().toISOString(),
      duration_ms: 100,
      items: [userItem, reasoningItem, toolItem, { ...agentItem, status: "completed", text: fullText }]
    }
  });

  const final = await waitFor(
    win,
    () => {
      const snapshot = streamingSnapshot();
      return snapshot.hasStreaming && snapshot.text.includes("streaming-word-119") ? snapshot : null;
    },
    16000,
    { fullLength: fullText.length }
  );
  assert.equal(final.hasStaticFallback, false, "Assistant content should remain on StreamingMarkdown after settling.");
  assert.ok(final.text.includes("streaming-word-119"), "StreamingMarkdown should eventually show the complete response.");
  assert.match(final.streamState, /^(settling|settled)$/, "Final text should be in a completion visual state.");

  const collapsedProcess = await waitFor(
    win,
    () => {
      const group = document.querySelector(".turn-process-group");
      const toggle = group?.querySelector(".turn-process-toggle");
      const details = group?.querySelector(".turn-process-details");
      const activity = group?.querySelector(".activity-group");
      if (!(group instanceof HTMLElement) || !(toggle instanceof HTMLButtonElement) || !(details instanceof HTMLElement)) {
        return null;
      }
      if (toggle.getAttribute("aria-expanded") !== "false" || details.getAttribute("aria-hidden") !== "true") {
        return null;
      }
      return {
        text: group.textContent ?? "",
        activityExpanded: activity?.classList.contains("expanded") ?? false
      };
    },
    3000
  );
  assert.match(collapsedProcess.text, /过程记录/, "Completed process records should be summarized behind a compact toggle.");
  assert.equal(collapsedProcess.activityExpanded, false, "Completed tool activity details should be folded by default.");

  const expandedProcess = await waitFor(
    win,
    () => {
      const button = document.querySelector(".turn-process-toggle");
      if (!(button instanceof HTMLButtonElement)) {
        return null;
      }
      if (button.getAttribute("aria-expanded") !== "true") {
        button.click();
        return null;
      }
      const group = document.querySelector(".turn-process-group");
      const details = group?.querySelector(".turn-process-details");
      return {
        expanded: button.getAttribute("aria-expanded"),
        hidden: details?.getAttribute("aria-hidden"),
        text: group?.textContent ?? ""
      };
    },
    3000
  );
  assert.equal(expandedProcess.expanded, "true", "Process records should be expandable after auto-collapse.");
  assert.equal(expandedProcess.hidden, "false", "Expanded process records should expose their details.");
  assert.match(expandedProcess.text, /搜索 1 次/, "Expanded process records should include the folded tool activity summary.");

  const settled = await waitFor(
    win,
    () => {
      const snapshot = streamingSnapshot();
      return snapshot.streamState === "settled" ? snapshot : null;
    },
    3000,
    { fullLength: fullText.length }
  );
  assert.equal(settled.hasStreaming, true, "Assistant content should still use StreamingMarkdown after completion.");
  assert.equal(settled.hasStaticFallback, false, "No static fallback should be rendered after stream completion.");
  assert.equal(settled.animatedWords, 0, "Settled content should not keep running word animations.");
  assert.equal(settled.heading, "Streaming markdown", "Final assistant content should render Markdown headings.");
  assert.equal(settled.bold, "bold text", "Final assistant content should render Markdown emphasis.");
  assert.equal(settled.linkHref, "https://example.com/", "Final assistant content should render safe Markdown links.");
  assert.equal(settled.listItems, 2, "Final assistant content should render Markdown lists.");
  assert.equal(settled.checkedTasks, 1, "Final assistant content should render GFM task list items.");
  assert.equal(settled.hasTable, true, "Final assistant content should render GFM tables.");
  assert.equal(settled.code, "const answer = 42;", "Final assistant content should render fenced code blocks.");

  const cancelledUserItem = {
    id: "user-cancelled-error-e2e",
    type: "user_message",
    status: "completed",
    text: "Start then stop."
  };
  const cancelledAgentItem = {
    id: "agent-cancelled-error-e2e",
    type: "agent_message",
    status: "completed",
    text: "Partial answer stays visible."
  };
  const cancelledTurn = {
    id: "turn-cancelled-error-e2e",
    items: [cancelledUserItem, cancelledAgentItem],
    items_view: "full",
    status: "in_progress",
    started_at: now
  };
  const rawCancelledError =
    'stream request failed: request failed: Post "https://chatgpt.com/backend-api/codex/responses": context canceled';
  emitNotification(win, "turn/started", { thread_id: thread.id, turn: cancelledTurn });
  emitNotification(win, "turn/error", {
    thread_id: thread.id,
    turn_id: cancelledTurn.id,
    error: rawCancelledError,
    turn: {
      ...cancelledTurn,
      status: "interrupted",
      error: { message: rawCancelledError },
      completed_at: new Date().toISOString(),
      duration_ms: 240,
      items: [cancelledUserItem]
    }
  });
  const cancelledNotice = await waitFor(
    win,
    () => {
      const notice = document.querySelector(".turn-notice");
      const text = document.querySelector(".conversation-width")?.textContent ?? "";
      return notice
        ? {
            noticeClass: notice.className,
            noticeText: notice.textContent ?? "",
            conversationText: text,
            hasRedError: Boolean(document.querySelector(".turn-error"))
          }
        : null;
    },
    3000
  );
  assert.match(cancelledNotice.noticeClass, /neutral/, "Cancelled turns should use the neutral notice tone.");
  assert.equal(
    cancelledNotice.conversationText.includes("Partial answer stays visible."),
    true,
    "Cancelled turns should preserve partial assistant output."
  );
  assert.match(cancelledNotice.noticeText, /回复已中断/, "Cancelled turns should render a neutral interrupted notice.");
  assert.equal(cancelledNotice.hasRedError, false, "Cancelled turns should not render the red error block.");
  assert.equal(cancelledNotice.conversationText.includes("chatgpt.com/backend-api"), false, "Cancelled UI should hide backend URLs.");
  assert.equal(cancelledNotice.conversationText.includes("context canceled"), false, "Cancelled UI should hide raw cancellation text.");

  const failedUserItem = {
    id: "user-real-error-e2e",
    type: "user_message",
    status: "completed",
    text: "Trigger a real failure."
  };
  const failedTurn = {
    id: "turn-real-error-e2e",
    items: [failedUserItem],
    items_view: "full",
    status: "in_progress",
    started_at: now
  };
  const rawNetworkError =
    'stream request failed: request failed: Post "https://chatgpt.com/backend-api/codex/responses": dial tcp: no such host';
  emitNotification(win, "turn/started", { thread_id: thread.id, turn: failedTurn });
  emitNotification(win, "turn/error", {
    thread_id: thread.id,
    turn_id: failedTurn.id,
    error: rawNetworkError,
    turn: {
      ...failedTurn,
      status: "failed",
      error: { message: rawNetworkError },
      completed_at: new Date().toISOString(),
      duration_ms: 180,
      items: [failedUserItem]
    }
  });
  const failedNotice = await waitFor(
    win,
    () => {
      const notice = document.querySelector(".turn-notice.error");
      const text = document.querySelector(".conversation-width")?.textContent ?? "";
      return notice
        ? {
            noticeText: notice.textContent ?? "",
            conversationText: text
          }
        : null;
    },
    3000
  );
  assert.match(failedNotice.noticeText, /连接暂时不可用/, "Real network failures should render an error notice.");
  assert.equal(failedNotice.conversationText.includes("chatgpt.com/backend-api"), false, "Failure UI should hide backend URLs.");
  assert.equal(failedNotice.conversationText.includes("stream request failed"), false, "Failure UI should hide wrapped internal errors.");

  console.log("streaming markdown e2e passed");
  app.exit(0);
}

function emitNotification(win, method, params) {
  win.webContents.send("test:server-event", {
    workdir: process.env.WUU_STREAM_E2E_CWD || process.cwd(),
    kind: "notification",
    message: { method, params }
  });
}

function emitServerRequest(win, id, method, params) {
  win.webContents.send("test:server-event", {
    workdir: process.env.WUU_STREAM_E2E_CWD || process.cwd(),
    kind: "server-request",
    message: { id, method, params }
  });
}

function loadFile(win, file) {
  return new Promise((resolve, reject) => {
    win.webContents.once("did-fail-load", (_event, _code, description) => reject(new Error(description)));
    win.webContents.once("did-finish-load", () => resolve());
    win.loadFile(file);
  });
}

async function waitFor(win, predicate, timeoutMs, options = {}) {
  const started = Date.now();
  let lastValue;
  while (Date.now() - started < timeoutMs) {
    lastValue = await evaluate(win, predicate, options);
    if (lastValue) {
      return lastValue;
    }
    await delay(40);
  }
  throw new Error(`Timed out waiting for condition. Last value: ${JSON.stringify(lastValue)}`);
}

async function evaluate(win, fn, options = {}) {
  const source = `(() => {
    window.__STREAMING_E2E_FULL_LENGTH__ = ${Number(options.fullLength ?? 0)};
    window.e2eActiveTitleText = () => {
      const heading = document.querySelector(".title-block h1")?.textContent?.trim();
      if (heading) {
        return heading;
      }
      return document.querySelector(".session-tab.active .session-tab-title")?.textContent?.trim() ?? "";
    };
    window.streamingSnapshot = () => {
      const streaming = document.querySelector(".agent-text .streaming-markdown");
      const staticFallback = document.querySelector(".agent-text > .rich-content:not(.streaming-markdown)");
      const text = streaming?.textContent ?? "";
      const animatedWords = streaming
        ? Array.from(streaming.querySelectorAll(".stream-word")).filter(
            (node) => getComputedStyle(node).animationName !== "none"
          ).length
        : 0;
      return {
        hasStreaming: Boolean(streaming),
        hasStaticFallback: Boolean(staticFallback),
        streamState: streaming?.getAttribute("data-stream-state") ?? null,
        animatedWords,
        text,
        textLength: text.length,
        heading: streaming?.querySelector("h1")?.textContent ?? "",
        bold: streaming?.querySelector("strong")?.textContent ?? "",
        linkHref: streaming?.querySelector("a")?.href ?? "",
        listItems: streaming?.querySelectorAll("li").length ?? 0,
        checkedTasks: streaming?.querySelectorAll('input[type="checkbox"]:checked').length ?? 0,
        hasTable: Boolean(streaming?.querySelector("table")),
        code: streaming?.querySelector("pre code")?.textContent?.trim() ?? ""
      };
    };
    return (${fn.toString()})();
  })()`;
  return win.webContents.executeJavaScript(source, true);
}

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function fail(error) {
  console.error(error);
  app.exit(1);
}
