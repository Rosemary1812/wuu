const path = require("path");

const FONT_CN = "Microsoft YaHei";
const FONT_EN = "Arial";

const COLORS = {
  ink: "111111",
  ink2: "222222",
  paper: "F7F4ED",
  white: "FFFFFF",
  fog: "E9E5DA",
  muted: "6F6B62",
  gold: "D4AF37",
  gold2: "F0D77B",
  blue: "3478F6",
  cyan: "5EC7C2",
  green: "47A36B",
  coral: "E66A4E",
};

const ASSETS = {
  appIcon: path.resolve(__dirname, "../../assets/app-icon-256.png"),
  desktop: path.resolve(__dirname, "../../landing/assets/desktop.png"),
  desktopApp: path.resolve(__dirname, "../../landing/assets/desktop-app.png"),
  mascotFace: path.resolve(__dirname, "../../desktop/src/renderer/assets/mascot-face.png"),
  scout: path.resolve(__dirname, "../../landing/assets/mascot/wuu-mascot-concept-30.png"),
  forge: path.resolve(__dirname, "../../landing/assets/mascot/wuu-mascot-concept-29.png"),
  sage: path.resolve(__dirname, "../../landing/assets/mascot/wuu-mascot-concept-28.png"),
  wave: path.resolve(__dirname, "../../landing/assets/mascot/wuu-mascot-concept-07.png"),
};

function makeShadow(opacity = 0.16, blur = 4, offset = 2) {
  return { type: "outer", color: "000000", opacity, blur, angle: 45, distance: offset };
}

function addPageNumber(slide, pres, page, theme, dark = false) {
  slide.addShape(pres.shapes.OVAL, {
    x: 9.3, y: 5.1, w: 0.4, h: 0.4,
    fill: { color: dark ? theme.accent : theme.primary },
    line: { color: dark ? theme.accent : theme.primary, transparency: 100 },
  });
  slide.addText(String(page).padStart(2, "0"), {
    x: 9.3, y: 5.1, w: 0.4, h: 0.4,
    fontFace: FONT_EN, fontSize: 10, bold: true,
    color: dark ? theme.primary : theme.bg,
    align: "center", valign: "middle", margin: 0,
  });
}

function addMiniBrand(slide, dark = false) {
  slide.addImage({ path: ASSETS.appIcon, x: 0.42, y: 0.34, w: 0.28, h: 0.28 });
  slide.addText("wuu", {
    x: 0.76, y: 0.32, w: 0.7, h: 0.3,
    fontFace: FONT_EN, fontSize: 12, bold: true,
    color: dark ? COLORS.white : COLORS.ink, margin: 0, valign: "middle",
  });
}

function addTitle(slide, title, eyebrow, dark = false) {
  if (eyebrow) {
    slide.addText(eyebrow.toUpperCase(), {
      x: 0.48, y: 0.72, w: 4.4, h: 0.25,
      fontFace: FONT_EN, fontSize: 10, bold: true, charSpacing: 1.4,
      color: dark ? COLORS.gold2 : COLORS.muted, margin: 0,
    });
  }
  slide.addText(title, {
    x: 0.48, y: 1.0, w: 9.0, h: 0.68,
    fontFace: FONT_CN, fontSize: 28, bold: true,
    color: dark ? COLORS.white : COLORS.ink, margin: 0, fit: "shrink",
  });
}

function addFooterSource(slide, text, dark = false) {
  slide.addText(text, {
    x: 0.48, y: 5.2, w: 7.8, h: 0.16,
    fontFace: FONT_CN, fontSize: 7.5,
    color: dark ? "9B978D" : "8A857B", margin: 0,
  });
}

function addPill(slide, pres, text, x, y, w, fill, color = COLORS.ink, h = 0.34) {
  slide.addShape(pres.shapes.ROUNDED_RECTANGLE, {
    x, y, w, h,
    rectRadius: 0.16,
    fill: { color: fill }, line: { color: fill, transparency: 100 },
  });
  slide.addText(text, {
    x, y, w, h,
    fontFace: FONT_CN, fontSize: 10, bold: true,
    color, align: "center", valign: "middle", margin: 0,
  });
}

function addCard(slide, pres, x, y, w, h, fill = COLORS.white, line = COLORS.fog, shadow = false) {
  slide.addShape(pres.shapes.ROUNDED_RECTANGLE, {
    x, y, w, h, rectRadius: 0.1,
    fill: { color: fill },
    line: { color: line, width: 1 },
    ...(shadow ? { shadow: makeShadow() } : {}),
  });
}

function addImageContain(slide, imagePath, srcW, srcH, x, y, w, h) {
  const scale = Math.min(w / srcW, h / srcH);
  const iw = srcW * scale;
  const ih = srcH * scale;
  slide.addImage({ path: imagePath, x: x + (w - iw) / 2, y: y + (h - ih) / 2, w: iw, h: ih });
}

function addImageFrame(slide, pres, imagePath, srcW, srcH, x, y, w, h, dark = false) {
  addCard(slide, pres, x, y, w, h, dark ? "1B1B1B" : COLORS.white, dark ? "3A3A3A" : "D9D4C9", true);
  addImageContain(slide, imagePath, srcW, srcH, x + 0.12, y + 0.12, w - 0.24, h - 0.24);
}

function addRichBullet(slide, marker, title, body, x, y, w, dark = false, markerColor = COLORS.gold) {
  slide.addShape("ellipse", {
    x, y: y + 0.03, w: 0.32, h: 0.32,
    fill: { color: markerColor }, line: { color: markerColor, transparency: 100 },
  });
  slide.addText(marker, {
    x, y: y + 0.03, w: 0.32, h: 0.32,
    fontFace: FONT_EN, fontSize: 9, bold: true,
    color: COLORS.ink, align: "center", valign: "middle", margin: 0,
  });
  slide.addText(title, {
    x: x + 0.46, y, w: w - 0.46, h: 0.28,
    fontFace: FONT_CN, fontSize: 14, bold: true,
    color: dark ? COLORS.white : COLORS.ink, margin: 0,
  });
  slide.addText(body, {
    x: x + 0.46, y: y + 0.32, w: w - 0.46, h: 0.52,
    fontFace: FONT_CN, fontSize: 10.5,
    color: dark ? "C8C4BB" : COLORS.muted, margin: 0, breakLine: false, fit: "shrink",
  });
}

module.exports = {
  FONT_CN, FONT_EN, COLORS, ASSETS,
  makeShadow, addPageNumber, addMiniBrand, addTitle, addFooterSource,
  addPill, addCard, addImageContain, addImageFrame, addRichBullet,
};

