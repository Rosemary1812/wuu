const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { app, BrowserWindow } = require("electron");

const desktopRoot = path.resolve(__dirname, "..");
const repoRoot = path.resolve(desktopRoot, "..");
const rendererHtml = path.join(desktopRoot, "out", "renderer", "index.html");
const preload = path.join(__dirname, "streaming-e2e-preload.cjs");
const evidenceDir = path.join(desktopRoot, "out", "e2e");

process.env.WUU_STREAM_E2E_CWD = repoRoot;
app.commandLine.appendSwitch("disable-gpu");
app.commandLine.appendSwitch("disable-software-rasterizer");

app.whenReady().then(run).catch(fail);

async function run() {
  assert.ok(fs.existsSync(rendererHtml), "Renderer build is missing. Run npm run build first.");
  assert.ok(fs.existsSync(preload), "Streaming E2E preload is missing.");
  fs.mkdirSync(evidenceDir, { recursive: true });

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

  const threadID = await startActiveThread(win);
  const now = new Date().toISOString();
  const userItem = {
    id: "user-process-handoff-e2e",
    type: "user_message",
    status: "completed",
    text: "Alternate text, tools, then final text."
  };
  const commentaryItem = {
    id: "agent-process-handoff-commentary-e2e",
    type: "agent_message",
    phase: "commentary",
    status: "completed",
    text: "第一段说明：我会先查看文件，然后再给出最终回答。"
  };
  const toolItems = Array.from({ length: 8 }, (_value, index) => ({
    id: `tool-process-handoff-e2e-${index + 1}`,
    type: "tool_call",
    status: "completed",
    name: index % 2 === 0 ? "list_files" : "read_file",
    arguments:
      index % 2 === 0
        ? '{"path":"desktop/src/renderer"}'
        : `{"path":"desktop/src/renderer/${index + 1}.tsx"}`,
    result: "App.tsx\nStreamingMarkdown.tsx\nToolActivity.tsx"
  }));
  const finalItem = {
    id: "agent-process-handoff-final-e2e",
    type: "agent_message",
    phase: "final_answer",
    status: "in_progress",
    text: ""
  };
  const turn = {
    id: "turn-process-handoff-e2e",
    items: [userItem],
    items_view: "full",
    status: "in_progress",
    started_at: now
  };
  const finalText = "第二段最终回答应该接替上一段过程文字，而不是跳到另一个视觉位置。";

  emitNotification(win, "turn/started", { thread_id: threadID, turn });
  emitNotification(win, "item/completed", {
    thread_id: threadID,
    turn_id: turn.id,
    item: commentaryItem
  });
  for (const item of toolItems) {
    emitNotification(win, "item/completed", {
      thread_id: threadID,
      turn_id: turn.id,
      item
    });
  }

  const processRect = await waitFor(win, processCheckpointRect, 3000);
  assert.match(processRect.text, /第一段说明/, "Process preview should be visible before final text starts.");
  const beforeProcessState = await waitFor(win, processGroupState, 3000);
  assert.equal(beforeProcessState.expanded, true, "Process details should stay open while the turn is still in flight.");
  assert.equal(beforeProcessState.shellHasProcess, true, "Assistant shell should expose a stable process lane.");
  assert.equal(beforeProcessState.shellHasAnswer, false, "Answer lane should stay empty before final text starts.");
  assert.equal(beforeProcessState.checkpointVisible, false, "Process preview is hidden while the process fold is open.");
  assert.ok(beforeProcessState.detailsHeight > 1, `Open process details should occupy space. Height=${beforeProcessState.detailsHeight}`);
  await capture(win, "process-handoff-before.png");

  emitNotification(win, "item/started", {
    thread_id: threadID,
    turn_id: turn.id,
    item: finalItem
  });
  emitNotification(win, "item/agentMessage/delta", {
    thread_id: threadID,
    turn_id: turn.id,
    item_id: finalItem.id,
    delta: finalText
  });

  const finalRect = await waitFor(win, finalAnswerRect, 3000);
  const afterProcessState = await waitFor(win, processGroupState, 3000);
  assert.match(finalRect.text, /第二段/, "Final text should stream after process work.");
  assert.equal(afterProcessState.shellHasProcess, true, "Process lane should remain stable after final text starts.");
  assert.equal(afterProcessState.shellHasAnswer, true, "Final text should appear in the stable answer lane.");
  assert.equal(afterProcessState.checkpointVisible, false, "Process preview stays hidden while the in-flight process fold is open.");
  assert.equal(afterProcessState.expanded, true, "Process details should stay open until the turn completes.");
  assert.ok(
    afterProcessState.detailsHeight > 1,
    `Process details should remain visible during handoff. Height=${afterProcessState.detailsHeight}`
  );
  assert.ok(
    Number.parseFloat(finalRect.fontSize) >= 14,
    `Final text should render as readable answer body copy. Font size=${finalRect.fontSize}`
  );
  assert.ok(
    afterProcessState.groupToAnswerGap >= 4 && afterProcessState.groupToAnswerGap <= 24,
    `Process group and answer lane should use a compact stable gap. Gap=${afterProcessState.groupToAnswerGap}`
  );
  await capture(win, "process-handoff-after.png");

  emitNotification(win, "item/completed", {
    thread_id: threadID,
    turn_id: turn.id,
    item: { ...finalItem, status: "completed", text: finalText }
  });
  emitNotification(win, "turn/completed", {
    thread_id: threadID,
    turn: {
      ...turn,
      status: "completed",
      completed_at: new Date().toISOString(),
      duration_ms: 180,
      items: [
        userItem,
        commentaryItem,
        ...toolItems,
        { ...finalItem, status: "completed", text: finalText }
      ]
    }
  });

  await delay(700);
  const completedActionSlot = await waitFor(
    win,
    completedAgentActionSlotRect,
    3000
  );
  const completedFinalRect = await waitFor(win, finalAnswerRect, 3000);
  const completedProcessState = await waitFor(win, processGroupState, 3000);
  const bodyToActionsGap = completedActionSlot.top - (completedFinalRect.top + completedFinalRect.height);
  assert.equal(completedProcessState.shellHasProcess, true, "Completed process summary should remain in the stable shell.");
  assert.equal(completedProcessState.shellHasAnswer, true, "Completed answer should remain in the answer lane.");
  assert.equal(completedProcessState.checkpointVisible, false, "Completed process summary should not keep the live commentary preview.");
  assert.equal(completedProcessState.expanded, false, "Completed process details should remain collapsed by default.");
  assert.ok(
    completedProcessState.detailsHeight <= 24,
    `Completed process details should not occupy space until opened. Height=${completedProcessState.detailsHeight}`
  );
  assert.ok(
    Math.abs(completedFinalRect.left - finalRect.left) <= 2,
    `Completing the answer should not move text horizontally. Before=${finalRect.left}, after=${completedFinalRect.left}`
  );
  assert.ok(
    completedProcessState.groupToAnswerGap >= 4 && completedProcessState.groupToAnswerGap <= 48,
    `Process group and answer should use a compact hierarchy gap. Gap=${completedProcessState.groupToAnswerGap}`
  );
  assert.ok(
    bodyToActionsGap >= 4 && bodyToActionsGap <= 16,
    `Answer and action buttons should keep a compact breathing gap. Gap=${bodyToActionsGap}`
  );
  await capture(win, "process-handoff-completed.png");

  console.log("process handoff e2e passed");
  app.exit(0);
}

async function startActiveThread(win) {
  const started = await waitFor(
    win,
    () => {
      const textarea = document.querySelector(".composer textarea");
      if (!(textarea instanceof HTMLTextAreaElement)) {
        return false;
      }
      textarea.focus();
      const valueSetter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")?.set;
      valueSetter?.call(textarea, "Process handoff e2e seed.");
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
  assert.equal(started, true, "Composer should create an active e2e thread.");
  await waitFor(
    win,
    () => {
      const text = document.querySelector(".conversation-width")?.textContent ?? "";
      return text.includes("Process handoff e2e seed.");
    },
    3000
  );
  return "thread-immediate-title-e2e";
}

function processCheckpointRect() {
  const turns = Array.from(document.querySelectorAll(".turn"));
  const turn = turns.at(-1);
  const checkpoint =
    turn?.querySelector(".turn-process-preview-text") ??
    turn?.querySelector(".turn-process-fold-body") ??
    turn?.querySelector(".turn-process-fold");
  if (!(checkpoint instanceof HTMLElement)) {
    return null;
  }
  const rect = checkpoint.getBoundingClientRect();
  if (rect.width <= 0 || rect.height <= 0) {
    return null;
  }
  const text = checkpoint.textContent ?? "";
  if (!text.includes("第一段说明")) {
    return null;
  }
  const style = getComputedStyle(checkpoint);
  return {
    text,
    top: rect.top,
    left: rect.left,
    width: rect.width,
    height: rect.height,
    fontSize: style.fontSize,
    lineHeight: style.lineHeight
  };
}

function finalAnswerRect() {
  const turns = Array.from(document.querySelectorAll(".turn"));
  const turn = turns.at(-1);
  const finalLane = turn?.querySelector(".turn-answer-body");
  const markdown = finalLane?.querySelector(".agent-text .streaming-markdown");
  const finalText =
    finalLane?.querySelector(".agent-text .streaming-plain-text") ??
    markdown?.querySelector(".rich-paragraph, .rich-heading, .streaming-plain-text") ??
    markdown;
  if (!(finalText instanceof HTMLElement) || !finalText.textContent?.trim()) {
    return null;
  }
  const text = finalText.textContent ?? "";
  if (!text.includes("第二段")) {
    return null;
  }
  const rect = finalText.getBoundingClientRect();
  const style = getComputedStyle(finalText);
  return {
    text,
    top: rect.top,
    left: rect.left,
    width: rect.width,
    height: rect.height,
    fontSize: style.fontSize,
    lineHeight: style.lineHeight
  };
}

function processGroupState() {
  const turns = Array.from(document.querySelectorAll(".turn"));
  const turn = turns.at(-1);
  const shell = turn?.querySelector(".assistant-turn-shell");
  const group = shell?.querySelector(".turn-process-fold");
  if (!(group instanceof HTMLElement)) {
    return null;
  }
  const processLane = shell?.querySelector(".turn-process-fold");
  const answerLane = shell?.querySelector(".turn-answer-body");
  const answer = answerLane?.querySelector(".agent-block");
  const checkpoint = group.querySelector(".turn-process-preview");
  const details = group.querySelector(".collapsible-details");
  if (!(processLane instanceof HTMLElement)) {
    return null;
  }
  const answerRect =
    answer instanceof HTMLElement
      ? answer.getBoundingClientRect()
      : { top: 0, bottom: 0, height: 0 };
  const detailsRect =
    details instanceof HTMLElement
      ? details.getBoundingClientRect()
      : { top: 0, bottom: 0, height: 0 };
  const groupRect = group.getBoundingClientRect();
  const visibleActivityRows = Array.from(group.querySelectorAll(".activity-row")).filter((row) => {
    if (!(row instanceof HTMLElement)) {
      return false;
    }
    const rect = row.getBoundingClientRect();
    return (
      detailsRect.height > 1 &&
      rect.width > 0 &&
      rect.height > 0 &&
      rect.bottom > detailsRect.top &&
      rect.top < detailsRect.bottom
    );
  }).length;
  return {
    expanded: group.classList.contains("expanded"),
    shellHasProcess: shell instanceof HTMLElement && shell.classList.contains("has-process"),
    shellHasAnswer: shell instanceof HTMLElement && shell.classList.contains("has-answer"),
    checkpointVisible: checkpoint instanceof HTMLElement && checkpoint.getBoundingClientRect().height > 0,
    detailsHeight: detailsRect.height,
    visibleActivityRows,
    groupToAnswerGap:
      answer instanceof HTMLElement && answerRect.height > 0
        ? answerRect.top - groupRect.bottom
        : null
  };
}

function agentActionSlotRect() {
  const turns = Array.from(document.querySelectorAll(".turn"));
  const turn = turns.at(-1);
  const slot = turn?.querySelector(".agent-message-actions");
  if (!(slot instanceof HTMLElement)) {
    return null;
  }
  const rect = slot.getBoundingClientRect();
  if (rect.height <= 0) {
    return null;
  }
  return {
    top: rect.top,
    left: rect.left,
    width: rect.width,
    height: rect.height,
    hasButtons: slot.querySelectorAll(".message-action-button").length > 0
  };
}

function completedAgentActionSlotRect() {
  const turns = Array.from(document.querySelectorAll(".turn"));
  const turn = turns.at(-1);
  const slot = turn?.querySelector(".agent-message-actions");
  if (!(slot instanceof HTMLElement)) {
    return null;
  }
  const rect = slot.getBoundingClientRect();
  if (rect.height <= 0) {
    return null;
  }
  const hasButtons = slot.querySelectorAll(".message-action-button").length > 0;
  return hasButtons
    ? {
        top: rect.top,
        left: rect.left,
        width: rect.width,
        height: rect.height,
        hasButtons
      }
    : null;
}

function emitNotification(win, method, params) {
  win.webContents.send("test:server-event", {
    workdir: process.env.WUU_STREAM_E2E_CWD || process.cwd(),
    kind: "notification",
    message: { method, params }
  });
}

function loadFile(win, file) {
  return new Promise((resolve, reject) => {
    win.webContents.once("did-fail-load", (_event, _code, description) => reject(new Error(description)));
    win.webContents.once("did-finish-load", () => resolve());
    win.loadFile(file);
  });
}

async function waitFor(win, predicate, timeoutMs) {
  const started = Date.now();
  let lastValue;
  while (Date.now() - started < timeoutMs) {
    lastValue = await evaluate(win, predicate);
    if (lastValue) {
      return lastValue;
    }
    await delay(40);
  }
  throw new Error(`Timed out waiting for condition. Last value: ${JSON.stringify(lastValue)}`);
}

async function evaluate(win, fn) {
  return win.webContents.executeJavaScript(`(${fn.toString()})()`, true);
}

async function capture(win, name) {
  const image = await win.webContents.capturePage();
  fs.writeFileSync(path.join(evidenceDir, name), image.toPNG());
}

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function fail(error) {
  console.error(error);
  app.exit(1);
}
