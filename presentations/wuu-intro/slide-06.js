const pptxgen = require("pptxgenjs");
const { FONT_CN, FONT_EN, COLORS, addMiniBrand, addTitle, addPageNumber, addCard, addFooterSource } = require("./helpers");

const slideConfig = { type: "content", subtype: "grid", index: 6, title: "为长任务准备的运行时" };

function createSlide(pres, theme) {
  const slide = pres.addSlide();
  slide.background = { color: theme.primary };
  addMiniBrand(slide, true);
  addTitle(slide, "为长任务准备的运行时", "BUILT FOR CONTINUITY", true);
  const blocks = [
    ["01", "计划", "把多步工作变成可见的待办、进行中与已完成。", COLORS.gold],
    ["02", "持久目标", "跨上下文窗口追踪同一个结果，不因一次回复结束。", COLORS.cyan],
    ["03", "会话与分叉", "恢复已有工作，或从检查点 fork 新方向。", COLORS.coral],
    ["04", "记忆", "保留真正有复用价值的偏好与经验。", COLORS.green],
    ["05", "技能", "按任务加载专属流程、规范与工具用法。", COLORS.blue],
    ["06", "后台工作", "管理长时间命令和可持续的执行状态。", COLORS.gold2],
  ];
  blocks.forEach((b, i) => {
    const col = i % 3;
    const row = Math.floor(i / 3);
    const x = 0.52 + col * 3.05;
    const y = 1.78 + row * 1.5;
    addCard(slide, pres, x, y, 2.77, 1.22, "202020", "3A3A3A", false);
    slide.addText(b[0], {
      x: x + 0.2, y: y + 0.17, w: 0.48, h: 0.25,
      fontFace: FONT_EN, fontSize: 11, bold: true, color: b[3], margin: 0,
    });
    slide.addText(b[1], {
      x: x + 0.74, y: y + 0.15, w: 1.75, h: 0.3,
      fontFace: FONT_CN, fontSize: 15, bold: true, color: COLORS.white, margin: 0,
    });
    slide.addText(b[2], {
      x: x + 0.2, y: y + 0.57, w: 2.3, h: 0.43,
      fontFace: FONT_CN, fontSize: 9.5, color: "BDB8AE", margin: 0, fit: "shrink",
    });
  });
  slide.addText("长期任务不是更长的一次聊天，而是一个可以恢复、分工和检查的执行系统。", {
    x: 1.15, y: 4.92, w: 7.7, h: 0.24,
    fontFace: FONT_CN, fontSize: 11.5, color: COLORS.gold2, align: "center", margin: 0,
  });
  addFooterSource(slide, "资料来源：README_zh.md · ROADMAP.md", true);
  addPageNumber(slide, pres, 6, theme, true);
  return slide;
}

if (require.main === module) { const p = new pptxgen(); p.layout = "LAYOUT_16x9"; createSlide(p, { primary: "111111", secondary: "2A2A2A", accent: "D4AF37", light: "F0D77B", bg: "F7F4ED" }); p.writeFile({ fileName: "slide-06-preview.pptx" }); }
module.exports = { createSlide, slideConfig };

