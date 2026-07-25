const pptxgen = require("pptxgenjs");
const { FONT_CN, FONT_EN, COLORS, ASSETS, addMiniBrand, addTitle, addPageNumber, addCard, addImageContain, addFooterSource } = require("./helpers");

const slideConfig = { type: "content", subtype: "process", index: 5, title: "把大任务交给一支小队" };

function createSlide(pres, theme) {
  const slide = pres.addSlide();
  slide.background = { color: theme.bg };
  addMiniBrand(slide);
  addTitle(slide, "把大任务交给一支小队", "MULTI-AGENT ORCHESTRATION");
  const agents = [
    ["Scout", "排查", "理解仓库\n定位问题\n收集证据", ASSETS.scout, COLORS.cyan],
    ["Forge", "实现", "修改代码\n运行命令\n推进交付", ASSETS.forge, COLORS.gold],
    ["Sage", "评审", "检查风险\n回归验证\n完成质量", ASSETS.sage, COLORS.coral],
  ];
  agents.forEach((a, i) => {
    const x = 0.62 + i * 2.62;
    addCard(slide, pres, x, 1.84, 2.22, 2.7, COLORS.white, "DED9CE", true);
    slide.addShape(pres.shapes.OVAL, {
      x: x + 0.63, y: 2.05, w: 0.96, h: 0.96,
      fill: { color: a[4], transparency: 18 }, line: { color: a[4], transparency: 100 },
    });
    addImageContain(slide, a[3], 1024, 1024, x + 0.66, 2.08, 0.9, 0.9);
    slide.addText(a[0], {
      x: x + 0.2, y: 3.17, w: 1.82, h: 0.28,
      fontFace: FONT_EN, fontSize: 16, bold: true, color: COLORS.ink, align: "center", margin: 0,
    });
    slide.addText(a[1], {
      x: x + 0.66, y: 3.52, w: 0.9, h: 0.28,
      fontFace: FONT_CN, fontSize: 10, bold: true, color: COLORS.ink, align: "center", margin: 0,
    });
    slide.addText(a[2], {
      x: x + 0.25, y: 3.82, w: 1.72, h: 0.58,
      fontFace: FONT_CN, fontSize: 8.8, color: COLORS.muted, align: "center", margin: 0, fit: "shrink",
    });
    if (i < 2) {
      slide.addShape(pres.shapes.LINE, {
        x: x + 2.26, y: 3.16, w: 0.28, h: 0,
        line: { color: COLORS.gold, width: 1.5, endArrowType: "triangle" },
      });
    }
  });
  slide.addShape(pres.shapes.ROUNDED_RECTANGLE, {
    x: 8.45, y: 2.22, w: 0.88, h: 1.95, rectRadius: 0.12,
    fill: { color: COLORS.ink }, line: { color: COLORS.ink, transparency: 100 },
  });
  slide.addText("你", {
    x: 8.45, y: 2.58, w: 0.88, h: 0.45,
    fontFace: FONT_CN, fontSize: 22, bold: true, color: COLORS.white, align: "center", margin: 0,
  });
  slide.addText("拍板\n与验收", {
    x: 8.52, y: 3.18, w: 0.74, h: 0.55,
    fontFace: FONT_CN, fontSize: 10, color: COLORS.gold2, align: "center", margin: 0,
  });
  slide.addShape(pres.shapes.LINE, {
    x: 8.16, y: 3.17, w: 0.23, h: 0,
    line: { color: COLORS.gold, width: 1.5, endArrowType: "triangle" },
  });
  addFooterSource(slide, "角色为演示示例；Wuu 支持子 Agent 的专项研究、并行工作与隔离实现。资料来源：README_zh.md · landing/index.html");
  addPageNumber(slide, pres, 5, theme);
  return slide;
}

if (require.main === module) { const p = new pptxgen(); p.layout = "LAYOUT_16x9"; createSlide(p, { primary: "111111", secondary: "2A2A2A", accent: "D4AF37", light: "F0D77B", bg: "F7F4ED" }); p.writeFile({ fileName: "slide-05-preview.pptx" }); }
module.exports = { createSlide, slideConfig };

