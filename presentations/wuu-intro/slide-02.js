const pptxgen = require("pptxgenjs");
const { FONT_CN, FONT_EN, COLORS, addMiniBrand, addTitle, addPageNumber, addCard, addFooterSource } = require("./helpers");

const slideConfig = { type: "content", subtype: "comparison", index: 2, title: "不只是回答问题，而是推进到完成" };

function createSlide(pres, theme) {
  const slide = pres.addSlide();
  slide.background = { color: theme.bg };
  addMiniBrand(slide);
  addTitle(slide, "不只是回答问题，而是推进到完成", "WHY WUU");

  const items = [
    ["01", "上下文容易断", "复杂任务跨越多轮、多文件和长时间运行，单次对话很难承载完整过程。"],
    ["02", "工具过程像黑盒", "真正的开发需要读代码、改文件、跑命令、看 diff，并留下可检查的证据。"],
    ["03", "人被迫做调度", "当任务变大，研究、实现、评审和验证需要分工，而不是都挤在一个回复里。"],
  ];
  items.forEach((item, i) => {
    const x = 0.5 + i * 3.04;
    addCard(slide, pres, x, 1.92, 2.78, 2.2, COLORS.white, "DED9CE", true);
    slide.addText(item[0], {
      x: x + 0.22, y: 2.13, w: 0.58, h: 0.46,
      fontFace: FONT_EN, fontSize: 24, bold: true, color: COLORS.gold, margin: 0,
    });
    slide.addText(item[1], {
      x: x + 0.22, y: 2.72, w: 2.25, h: 0.34,
      fontFace: FONT_CN, fontSize: 17, bold: true, color: COLORS.ink, margin: 0,
    });
    slide.addText(item[2], {
      x: x + 0.22, y: 3.22, w: 2.28, h: 0.64,
      fontFace: FONT_CN, fontSize: 10.5, color: COLORS.muted, margin: 0, fit: "shrink",
    });
  });
  slide.addShape(pres.shapes.ROUNDED_RECTANGLE, {
    x: 0.5, y: 4.38, w: 8.85, h: 0.48, rectRadius: 0.12,
    fill: { color: COLORS.ink }, line: { color: COLORS.ink, transparency: 100 },
  });
  slide.addText("Wuu 把模型、工具、会话与多 Agent 编排放进同一个可检查的工作循环。", {
    x: 0.74, y: 4.38, w: 8.35, h: 0.48,
    fontFace: FONT_CN, fontSize: 13, bold: true, color: COLORS.white,
    margin: 0, valign: "middle", align: "center",
  });
  addFooterSource(slide, "资料来源：README_zh.md · landing/index.html");
  addPageNumber(slide, pres, 2, theme);
  return slide;
}

if (require.main === module) { const p = new pptxgen(); p.layout = "LAYOUT_16x9"; createSlide(p, { primary: "111111", secondary: "2A2A2A", accent: "D4AF37", light: "F0D77B", bg: "F7F4ED" }); p.writeFile({ fileName: "slide-02-preview.pptx" }); }
module.exports = { createSlide, slideConfig };

