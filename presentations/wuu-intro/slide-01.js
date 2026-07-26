const pptxgen = require("pptxgenjs");
const { FONT_CN, FONT_EN, COLORS, ASSETS, addPill, addImageContain } = require("./helpers");

const slideConfig = { type: "cover", index: 1, title: "一群 AI 同事，把活干完" };

function createSlide(pres, theme) {
  const slide = pres.addSlide();
  slide.background = { color: theme.primary };

  slide.addShape(pres.shapes.OVAL, {
    x: 7.25, y: -0.95, w: 3.8, h: 3.8,
    fill: { color: theme.accent, transparency: 10 },
    line: { color: theme.accent, transparency: 100 },
  });
  slide.addShape(pres.shapes.OVAL, {
    x: 8.16, y: 2.2, w: 2.1, h: 2.1,
    fill: { color: "343434" }, line: { color: "343434", transparency: 100 },
  });
  addImageContain(slide, ASSETS.wave, 1024, 1024, 7.02, 0.18, 2.75, 2.75);
  slide.addImage({ path: ASSETS.appIcon, x: 0.54, y: 0.48, w: 0.48, h: 0.48 });
  slide.addText("wuu", {
    x: 1.14, y: 0.48, w: 1.2, h: 0.48,
    fontFace: FONT_EN, fontSize: 21, bold: true,
    color: COLORS.white, margin: 0, valign: "middle",
  });
  addPill(slide, pres, "OPEN SOURCE · BYOK · MULTI-AGENT", 0.55, 1.42, 2.85, "2B2B2B", COLORS.gold2, 0.36);
  slide.addText("一群 AI 同事，\n把活干完", {
    x: 0.52, y: 1.92, w: 6.7, h: 1.7,
    fontFace: FONT_CN, fontSize: 38, bold: true,
    color: COLORS.white, margin: 0, breakLine: false, fit: "shrink",
  });
  slide.addText("面向本地开发的开源 AI Coding Agent", {
    x: 0.56, y: 3.82, w: 5.7, h: 0.42,
    fontFace: FONT_CN, fontSize: 17,
    color: "D5D0C5", margin: 0,
  });
  slide.addText("Agent 们排查、实现、评审、跑测试；你掌握模型、权限与最终决定。", {
    x: 0.56, y: 4.35, w: 6.7, h: 0.55,
    fontFace: FONT_CN, fontSize: 12,
    color: "A8A399", margin: 0, fit: "shrink",
  });
  slide.addText("v0.12.1 · 2026", {
    x: 8.0, y: 5.14, w: 1.45, h: 0.18,
    fontFace: FONT_EN, fontSize: 8.5, color: "8D887D", align: "right", margin: 0,
  });
  return slide;
}

if (require.main === module) {
  const pres = new pptxgen(); pres.layout = "LAYOUT_16x9";
  createSlide(pres, { primary: "111111", secondary: "2A2A2A", accent: "D4AF37", light: "F0D77B", bg: "F7F4ED" });
  pres.writeFile({ fileName: "slide-01-preview.pptx" });
}
module.exports = { createSlide, slideConfig };

