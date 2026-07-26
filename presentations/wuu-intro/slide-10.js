const pptxgen = require("pptxgenjs");
const { FONT_CN, FONT_EN, COLORS, ASSETS, addMiniBrand, addPageNumber, addCard, addImageContain, addPill, addFooterSource } = require("./helpers");

const slideConfig = { type: "summary", index: 10, title: "三步开始，把下一个任务交给 Wuu" };

function createSlide(pres, theme) {
  const slide = pres.addSlide();
  slide.background = { color: theme.primary };
  addMiniBrand(slide, true);
  addPill(slide, pres, "GET STARTED", 0.55, 0.92, 1.2, "2C2C2C", COLORS.gold2, 0.34);
  slide.addText("三步开始，\n把下一个任务交给 Wuu", { x: 0.55, y: 1.45, w: 5.9, h: 1.25, fontFace: FONT_CN, fontSize: 31, bold: true, color: COLORS.white, margin: 0, fit: "shrink" });
  const steps = [["1", "选择本地项目"], ["2", "连接模型服务"], ["3", "描述想要的结果"]];
  steps.forEach((s, i) => {
    const x = 0.58 + i * 1.92;
    addCard(slide, pres, x, 3.02, 1.65, 1.1, "202020", "3A3A3A", false);
    slide.addText(s[0], { x: x + 0.17, y: 3.2, w: 0.33, h: 0.33, fontFace: FONT_EN, fontSize: 14, bold: true, color: COLORS.gold, margin: 0 });
    slide.addText(s[1], { x: x + 0.18, y: 3.67, w: 1.28, h: 0.24, fontFace: FONT_CN, fontSize: 11, bold: true, color: COLORS.white, margin: 0, fit: "shrink" });
  });
  slide.addText("github.com/blueberrycongee/wuu", { x: 0.58, y: 4.48, w: 3.75, h: 0.28, fontFace: FONT_EN, fontSize: 13, bold: true, color: COLORS.gold2, margin: 0, hyperlink: { url: "https://github.com/blueberrycongee/wuu" } });
  slide.addText("下载 macOS 预览版 · 查看文档 · 参与开源", { x: 0.58, y: 4.84, w: 4.1, h: 0.24, fontFace: FONT_CN, fontSize: 10.5, color: "B9B4AA", margin: 0 });
  slide.addShape(pres.shapes.OVAL, { x: 6.88, y: 0.72, w: 2.65, h: 2.65, fill: { color: theme.accent, transparency: 8 }, line: { color: theme.accent, transparency: 100 } });
  addImageContain(slide, ASSETS.mascotFace, 256, 256, 7.18, 1.01, 2.05, 2.05);
  addCard(slide, pres, 6.25, 3.62, 3.08, 1.26, "202020", "3A3A3A", false);
  slide.addText("当前状态", { x: 6.52, y: 3.88, w: 0.86, h: 0.24, fontFace: FONT_CN, fontSize: 10, bold: true, color: COLORS.gold2, margin: 0 });
  slide.addText("v0.12.1 · 早期预览", { x: 7.38, y: 3.88, w: 1.55, h: 0.24, fontFace: FONT_CN, fontSize: 10, bold: true, color: COLORS.white, margin: 0 });
  slide.addText("打包桌面版目前面向 Apple 芯片 Mac，且尚未签名与公证。", { x: 6.52, y: 4.31, w: 2.42, h: 0.34, fontFace: FONT_CN, fontSize: 8.8, color: "A9A49B", margin: 0, fit: "shrink" });
  addFooterSource(slide, "资料来源：README_zh.md · docs/zh-cn/getting-started/index.md", true);
  addPageNumber(slide, pres, 10, theme, true);
  return slide;
}

if (require.main === module) { const p = new pptxgen(); p.layout = "LAYOUT_16x9"; createSlide(p, { primary: "111111", secondary: "2A2A2A", accent: "D4AF37", light: "F0D77B", bg: "F7F4ED" }); p.writeFile({ fileName: "slide-10-preview.pptx" }); }
module.exports = { createSlide, slideConfig };

