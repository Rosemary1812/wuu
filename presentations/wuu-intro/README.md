# Wuu 中文产品介绍 PPT

这份 16:9 演示文稿面向第一次了解 Wuu 的开发者或团队，内容基于仓库内的产品文档、桌面截图和品牌素材。

## 文件

- `output/wuu-product-intro-zh.pptx`：可直接演示和编辑的 PowerPoint 文件
- `output/wuu-product-intro-zh.pdf`：用于快速预览全稿的 PDF
- `preview/`：逐页 PNG 预览
- `slide-01.js` 至 `slide-10.js`：每页的 PptxGenJS 源文件
- `compile.js`：合并并生成最终 PPTX

## 重新生成

```bash
cd presentations/wuu-intro
npm install
node compile.js
```

演示内容主要参考：

- `README_zh.md`
- `docs/zh-cn/getting-started/index.md`
- `docs/zh-cn/reference/security-model.md`
- `docs/en/automation/exec.md`
- `landing/index.html`


