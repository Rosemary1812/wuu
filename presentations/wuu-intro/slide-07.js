const pptxgen = require("pptxgenjs");
const { FONT_CN, FONT_EN, COLORS, addMiniBrand, addTitle, addPageNumber, addCard, addFooterSource } = require("./helpers");

const slideConfig = { type: "content", subtype: "diagram", index: 7, title: "同一个内核，两种使用方式" };

function createSlide(pres, theme) {
  const slide = pres.addSlide();
  slide.background = { color: theme.bg };
  addMiniBrand(slide);
  addTitle(slide, "同一个内核，两种使用方式", "ONE CORE, MULTIPLE SHELLS");
  addCard(slide, pres, 0.58, 1.9, 2.55, 2.45, COLORS.white, "DCD7CB", true);
  addCard(slide, pres, 6.87, 1.9, 2.55, 2.45, COLORS.white, "DCD7CB", true);
  slide.addText("桌面应用", { x: 0.9, y: 2.2, w: 1.9, h: 0.4, fontFace: FONT_CN, fontSize: 20, bold: true, color: COLORS.ink, align: "center", margin: 0 });
  slide.addText("面向人", { x: 1.35, y: 2.77, w: 1.0, h: 0.28, fontFace: FONT_CN, fontSize: 11, bold: true, color: COLORS.blue, align: "center", margin: 0 });
  slide.addText("会话 · 附件 · diff\n终端 · 原生桌面集成", { x: 0.92, y: 3.26, w: 1.86, h: 0.56, fontFace: FONT_CN, fontSize: 10.5, color: COLORS.muted, align: "center", margin: 0 });
  slide.addText("wuu exec", { x: 7.2, y: 2.2, w: 1.9, h: 0.4, fontFace: FONT_EN, fontSize: 20, bold: true, color: COLORS.ink, align: "center", margin: 0 });
  slide.addText("面向自动化", { x: 7.63, y: 2.77, w: 1.08, h: 0.28, fontFace: FONT_CN, fontSize: 11, bold: true, color: COLORS.coral, align: "center", margin: 0 });
  slide.addText("脚本 · CI · 机器人\n纯文本或 JSONL 输出", { x: 7.2, y: 3.26, w: 1.9, h: 0.56, fontFace: FONT_CN, fontSize: 10.5, color: COLORS.muted, align: "center", margin: 0 });
  slide.addShape(pres.shapes.OVAL, { x: 3.82, y: 2.15, w: 2.36, h: 2.36, fill: { color: COLORS.ink }, line: { color: COLORS.gold, width: 2 } });
  slide.addText("Go Core", { x: 4.18, y: 2.67, w: 1.64, h: 0.38, fontFace: FONT_EN, fontSize: 21, bold: true, color: COLORS.white, align: "center", margin: 0 });
  slide.addText("模型 · 工具 · 会话\n权限 · 编排", { x: 4.2, y: 3.25, w: 1.6, h: 0.58, fontFace: FONT_CN, fontSize: 10.5, color: COLORS.gold2, align: "center", margin: 0 });
  [[3.16, 4.05], [6.22, 6.81]].forEach((pair) => {
    slide.addShape(pres.shapes.LINE, { x: pair[0], y: 3.15, w: pair[1] - pair[0], h: 0, line: { color: COLORS.gold, width: 1.8, endArrowType: "triangle" } });
  });
  slide.addText("app-server · JSON-RPC", { x: 3.62, y: 4.74, w: 2.78, h: 0.25, fontFace: FONT_EN, fontSize: 10, color: COLORS.muted, align: "center", margin: 0 });
  addFooterSource(slide, "桌面端与 wuu exec 复用同一个核心运行时，而不是两套行为不同的产品。资料来源：docs/en/automation/exec.md");
  addPageNumber(slide, pres, 7, theme);
  return slide;
}

if (require.main === module) { const p = new pptxgen(); p.layout = "LAYOUT_16x9"; createSlide(p, { primary: "111111", secondary: "2A2A2A", accent: "D4AF37", light: "F0D77B", bg: "F7F4ED" }); p.writeFile({ fileName: "slide-07-preview.pptx" }); }
module.exports = { createSlide, slideConfig };

