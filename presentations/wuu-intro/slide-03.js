const pptxgen = require("pptxgenjs");
const { FONT_CN, FONT_EN, COLORS, addMiniBrand, addTitle, addPageNumber, addFooterSource } = require("./helpers");

const slideConfig = { type: "content", subtype: "process", index: 3, title: "一个从目标到证据的闭环" };

function createSlide(pres, theme) {
  const slide = pres.addSlide();
  slide.background = { color: theme.primary };
  addMiniBrand(slide, true);
  addTitle(slide, "一个从目标到证据的闭环", "THE WORK LOOP", true);

  const steps = [
    ["01", "理解目标", "读取仓库与规则"],
    ["02", "制定计划", "拆分可验证步骤"],
    ["03", "执行工具", "搜索 · 编辑 · 命令"],
    ["04", "评审验证", "diff · 测试 · 回归"],
    ["05", "交付结果", "说明改动与证据"],
  ];
  steps.forEach((s, i) => {
    const x = 0.52 + i * 1.82;
    const active = i === 2;
    slide.addShape(pres.shapes.ROUNDED_RECTANGLE, {
      x, y: active ? 2.02 : 2.18, w: 1.55, h: active ? 1.92 : 1.6, rectRadius: 0.1,
      fill: { color: active ? theme.accent : "232323" },
      line: { color: active ? theme.accent : "3A3A3A", width: 1 },
    });
    slide.addText(s[0], {
      x: x + 0.17, y: active ? 2.22 : 2.38, w: 0.55, h: 0.32,
      fontFace: FONT_EN, fontSize: 15, bold: true,
      color: active ? COLORS.ink : COLORS.gold2, margin: 0,
    });
    slide.addText(s[1], {
      x: x + 0.17, y: active ? 2.75 : 2.88, w: 1.2, h: 0.36,
      fontFace: FONT_CN, fontSize: 15, bold: true,
      color: active ? COLORS.ink : COLORS.white, margin: 0, fit: "shrink",
    });
    slide.addText(s[2], {
      x: x + 0.17, y: active ? 3.25 : 3.35, w: 1.18, h: 0.42,
      fontFace: FONT_CN, fontSize: 9.5,
      color: active ? "493E15" : "AAA69E", margin: 0, fit: "shrink",
    });
    if (i < steps.length - 1) {
      slide.addShape(pres.shapes.LINE, {
        x: x + 1.58, y: 2.98, w: 0.19, h: 0,
        line: { color: theme.accent, width: 1.5, beginArrowType: "none", endArrowType: "triangle" },
      });
    }
  });
  slide.addText("每一步都发生在选定工作区内，并通过工具活动、文件变化和命令结果保持可见。", {
    x: 1.0, y: 4.47, w: 8.0, h: 0.35,
    fontFace: FONT_CN, fontSize: 12, color: "C2BDB3", align: "center", margin: 0,
  });
  addFooterSource(slide, "资料来源：README_zh.md · docs/zh-cn/getting-started/index.md", true);
  addPageNumber(slide, pres, 3, theme, true);
  return slide;
}

if (require.main === module) { const p = new pptxgen(); p.layout = "LAYOUT_16x9"; createSlide(p, { primary: "111111", secondary: "2A2A2A", accent: "D4AF37", light: "F0D77B", bg: "F7F4ED" }); p.writeFile({ fileName: "slide-03-preview.pptx" }); }
module.exports = { createSlide, slideConfig };

