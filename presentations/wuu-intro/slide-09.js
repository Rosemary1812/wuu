const pptxgen = require("pptxgenjs");
const { FONT_CN, FONT_EN, COLORS, addMiniBrand, addTitle, addPageNumber, addCard, addFooterSource } = require("./helpers");

const slideConfig = { type: "content", subtype: "use-cases", index: 9, title: "从理解代码到自动化交付" };

function createSlide(pres, theme) {
  const slide = pres.addSlide();
  slide.background = { color: theme.bg };
  addMiniBrand(slide);
  addTitle(slide, "从理解代码到自动化交付", "COMMON USE CASES");
  const cases = [
    ["理解", "解释这个仓库的结构，指出关键入口与验证方式。", "READ · SEARCH", COLORS.cyan],
    ["修复", "修复失败的测试，运行相关验证并总结改动。", "EDIT · TEST", COLORS.gold],
    ["评审", "检查当前改动是否引入回归，并给出证据。", "DIFF · REVIEW", COLORS.coral],
    ["自动化", "用 JSONL 把 Agent 运行接入脚本、CI 与机器人。", "EXEC · JSONL", COLORS.blue],
  ];
  cases.forEach((c, i) => {
    const col = i % 2;
    const row = Math.floor(i / 2);
    const x = 0.58 + col * 4.52;
    const y = 1.82 + row * 1.55;
    addCard(slide, pres, x, y, 4.2, 1.28, COLORS.white, "DED9CE", true);
    slide.addShape(pres.shapes.RECTANGLE, { x, y, w: 0.12, h: 1.28, fill: { color: c[3] }, line: { color: c[3], transparency: 100 } });
    slide.addText(c[0], { x: x + 0.3, y: y + 0.22, w: 0.8, h: 0.36, fontFace: FONT_CN, fontSize: 19, bold: true, color: COLORS.ink, margin: 0 });
    slide.addText(c[2], { x: x + 2.72, y: y + 0.24, w: 1.14, h: 0.25, fontFace: FONT_EN, fontSize: 8.5, bold: true, color: c[3], align: "right", margin: 0 });
    slide.addText(c[1], { x: x + 0.3, y: y + 0.72, w: 3.48, h: 0.36, fontFace: FONT_CN, fontSize: 10.5, color: COLORS.muted, margin: 0, fit: "shrink" });
  });
  slide.addShape(pres.shapes.ROUNDED_RECTANGLE, { x: 1.55, y: 4.92, w: 6.9, h: 0.34, rectRadius: 0.12, fill: { color: COLORS.ink }, line: { color: COLORS.ink, transparency: 100 } });
  slide.addText('wuu exec --json "审查当前 diff"', { x: 1.72, y: 4.92, w: 6.55, h: 0.34, fontFace: FONT_EN, fontSize: 10.5, color: COLORS.gold2, align: "center", valign: "middle", margin: 0 });
  addFooterSource(slide, "资料来源：README_zh.md · docs/en/automation/exec.md");
  addPageNumber(slide, pres, 9, theme);
  return slide;
}

if (require.main === module) { const p = new pptxgen(); p.layout = "LAYOUT_16x9"; createSlide(p, { primary: "111111", secondary: "2A2A2A", accent: "D4AF37", light: "F0D77B", bg: "F7F4ED" }); p.writeFile({ fileName: "slide-09-preview.pptx" }); }
module.exports = { createSlide, slideConfig };

