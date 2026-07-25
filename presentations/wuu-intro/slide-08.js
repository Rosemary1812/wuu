const pptxgen = require("pptxgenjs");
const { FONT_CN, FONT_EN, COLORS, addMiniBrand, addTitle, addPageNumber, addCard, addPill, addFooterSource } = require("./helpers");

const slideConfig = { type: "content", subtype: "comparison", index: 8, title: "模型可选，边界可见" };

function createSlide(pres, theme) {
  const slide = pres.addSlide();
  slide.background = { color: theme.primary };
  addMiniBrand(slide, true);
  addTitle(slide, "模型可选，边界可见", "MODELS & DATA", true);
  addCard(slide, pres, 0.55, 1.75, 4.2, 2.92, "202020", "3A3A3A", false);
  addCard(slide, pres, 5.0, 1.75, 4.4, 2.92, "202020", "3A3A3A", false);
  slide.addText("BYOK · 自选模型服务", { x: 0.85, y: 2.05, w: 3.4, h: 0.38, fontFace: FONT_CN, fontSize: 18, bold: true, color: COLORS.white, margin: 0 });
  [["Anthropic", 0.88, 2.67, 1.05], ["OpenAI 兼容", 2.05, 2.67, 1.45], ["本地网关", 3.62, 2.67, 0.85]].forEach((p) => addPill(slide, pres, p[0], p[1], p[2], p[3], "333333", COLORS.gold2, 0.34));
  slide.addText("你选择提供商、模型、端点与凭据。\nWuu 不绑定订阅，也不代管模型账单。", { x: 0.86, y: 3.36, w: 3.44, h: 0.72, fontFace: FONT_CN, fontSize: 11.5, color: "C8C4BB", margin: 0 });
  slide.addText("注意：BYOK 不等于模型一定在本地运行。", { x: 0.86, y: 4.25, w: 3.4, h: 0.24, fontFace: FONT_CN, fontSize: 9.5, bold: true, color: COLORS.coral, margin: 0 });
  slide.addText("本地执行 · 明确权限", { x: 5.33, y: 2.05, w: 3.4, h: 0.38, fontFace: FONT_CN, fontSize: 18, bold: true, color: COLORS.white, margin: 0 });
  const rows = [["read_only", "只读检查"], ["standard", "工作区内修改"], ["unconfined", "用户级完整本机权限"]];
  rows.forEach((r, i) => {
    const y = 2.68 + i * 0.52;
    slide.addText(r[0], { x: 5.35, y, w: 1.25, h: 0.26, fontFace: FONT_EN, fontSize: 10, bold: true, color: i === 2 ? COLORS.coral : COLORS.gold2, margin: 0 });
    slide.addText(r[1], { x: 6.74, y, w: 2.08, h: 0.26, fontFace: FONT_CN, fontSize: 10.5, color: "C8C4BB", margin: 0 });
  });
  slide.addText("相关上下文会发送给所选模型服务；工具在本机执行。\n权限模式不等于操作系统沙箱。", { x: 5.34, y: 4.18, w: 3.55, h: 0.56, fontFace: FONT_CN, fontSize: 9.5, color: "A9A49B", margin: 0, fit: "shrink" });
  addFooterSource(slide, "资料来源：README_zh.md · docs/zh-cn/reference/security-model.md", true);
  addPageNumber(slide, pres, 8, theme, true);
  return slide;
}

if (require.main === module) { const p = new pptxgen(); p.layout = "LAYOUT_16x9"; createSlide(p, { primary: "111111", secondary: "2A2A2A", accent: "D4AF37", light: "F0D77B", bg: "F7F4ED" }); p.writeFile({ fileName: "slide-08-preview.pptx" }); }
module.exports = { createSlide, slideConfig };

