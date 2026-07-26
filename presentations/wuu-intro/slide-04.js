const pptxgen = require("pptxgenjs");
const { COLORS, ASSETS, addMiniBrand, addTitle, addPageNumber, addImageFrame, addRichBullet, addFooterSource } = require("./helpers");

const slideConfig = { type: "content", subtype: "mixed-media", index: 4, title: "桌面端：在一个界面里看见工作发生" };

function createSlide(pres, theme) {
  const slide = pres.addSlide();
  slide.background = { color: theme.bg };
  addMiniBrand(slide);
  addTitle(slide, "桌面端：在一个界面里看见工作发生", "INTERACTIVE DESKTOP");
  addRichBullet(slide, "A", "按工作区组织会话", "让任务、文件和长期上下文归属于真实项目。", 0.55, 1.86, 4.0, false, COLORS.gold);
  addRichBullet(slide, "B", "工具过程持续可见", "查看搜索、修改、命令、计划和子 Agent 活动。", 0.55, 2.82, 4.0, false, COLORS.cyan);
  addRichBullet(slide, "C", "结果紧邻验证入口", "用 /files、/diff、/terminal 检查文件与运行结果。", 0.55, 3.78, 4.0, false, COLORS.coral);
  addImageFrame(slide, pres, ASSETS.desktopApp, 1760, 1945, 5.05, 1.54, 4.12, 3.54, false);
  addFooterSource(slide, "画面：Wuu 桌面应用 · 资料来源：README_zh.md");
  addPageNumber(slide, pres, 4, theme);
  return slide;
}

if (require.main === module) { const p = new pptxgen(); p.layout = "LAYOUT_16x9"; createSlide(p, { primary: "111111", secondary: "2A2A2A", accent: "D4AF37", light: "F0D77B", bg: "F7F4ED" }); p.writeFile({ fileName: "slide-04-preview.pptx" }); }
module.exports = { createSlide, slideConfig };

