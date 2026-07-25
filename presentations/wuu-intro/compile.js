const pptxgen = require("pptxgenjs");
const path = require("path");

const pres = new pptxgen();
pres.layout = "LAYOUT_16x9";
pres.author = "wuu";
pres.company = "wuu";
pres.subject = "Wuu 中文产品介绍";
pres.title = "Wuu：一群 AI 同事，把活干完";
pres.lang = "zh-CN";
pres.theme = {
  headFontFace: "Microsoft YaHei",
  bodyFontFace: "Microsoft YaHei",
  lang: "zh-CN",
};

const theme = {
  primary: "111111",
  secondary: "2A2A2A",
  accent: "D4AF37",
  light: "F0D77B",
  bg: "F7F4ED",
};

for (let i = 1; i <= 10; i += 1) {
  const num = String(i).padStart(2, "0");
  require(`./slide-${num}.js`).createSlide(pres, theme);
}

const output = path.resolve(__dirname, "output/wuu-product-intro-zh.pptx");
pres.writeFile({ fileName: output });

