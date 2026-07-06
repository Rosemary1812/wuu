# Landing Page Color Redesign Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Migrate `landing/index.html` from the v1/v2 warm-cream + coral palette to a v4 editorial system: pure white background, neutral text, a single pure-vermillion accent (`#FF3D00`) used as sparingly as possible, and a Dify-style column-edge frame replacing the current 200px uniform hero grid.

**Architecture:** Single-file rewrite of `landing/index.html`. No JS changes, no asset changes. The mascot set and round-table image stay where they are; only CSS tokens and a handful of color references change. Work proceeds in small, independently-committable chunks — each task ends with a `grep` verification confirming no forbidden tokens remain in its scope.

**Tech Stack:** Static HTML + inline CSS, served as a file (no build step). No test framework — verification is grep-based, plus a manual `python3 -m http.server` + WebFetch contrast check at the end.

**Reference design:** `docs/plans/2026-07-06-landing-color-redesign-design.md` (read this first; it is the spec).

---

## Pre-flight: read the spec

Before starting any task, read `docs/plans/2026-07-06-landing-color-redesign-design.md` end-to-end. Pay particular attention to:
- §1.1 (the new `:root` block — verbatim)
- §2 (the four-place accent budget — keep it in your head throughout)
- §3 (section migration table — use this as your checklist)
- §4 (hero grid — read twice before touching the hero background)
- §6 (contrast: `#FF3D00` on white = 4.06:1, body text MUST NOT use the accent)

---

## Task 1: Replace the `:root` palette

**Files:**
- Modify: `landing/index.html:14-34` (the original `:root`)
- Modify: `landing/index.html:275` (the v3 `--surface / --line / --line-2` override)
- Modify: `landing/index.html:301` (the v3 global `--paper / --paper-2 / --panel / --cream` override)

**Step 1: Replace lines 14–34 with the new token block from design §1.1**

The replacement is exact. Do not invent new tokens. Do not keep the old `--sans / --display / --mono` font stacks (those are unchanged and stay as-is in the new block).

Old block (lines 14–34):
```css
:root {
  /* warm comic palette, sampled from the mascot art */
  --paper:     #F4E9D6;   /* main page — deep warm cream */
  --paper-2:   #F8F0E1;   /* lighter cream band */
  --panel:     #FDF8F3;   /* near-white — matches the mascot art ground */
  --cream:     #F2E7D4;   /* mascot body cream, for chips */
  --ink:       #211D18;   /* warm near-black — outlines & text */
  --ink-2:     #6c6355;   /* muted secondary text */
  --ink-3:     #9a9080;   /* faint */
  --coral:     #EE6A48;   /* the flame — primary accent */
  --coral-deep:#DE5530;   /* pressed / hover */
  --coral-soft:#F9C9B7;   /* blush wash */
  --leaf:      #5C9C7A;   /* success green, muted to fit the cream world */
  --shadow:    #211D18;   /* hard comic shadow */

  --sans: "Inter", "Noto Sans SC", -apple-system, "PingFang SC", "Microsoft YaHei", sans-serif;
  --display: "Inter", "Noto Sans SC", -apple-system, "PingFang SC", "Microsoft YaHei", sans-serif;
  --mono: "IBM Plex Mono", ui-monospace, "SF Mono", Consolas, monospace;

  --halftone: radial-gradient(var(--ink) 1.4px, transparent 1.5px);
}
```

New block (from design §1.1):
```css
:root {
  /* neutrals */
  --bg:         #FFFFFF;            /* page background */
  --bg-soft:    #FAFAFA;            /* faint hero ground */
  --ink:        #0F0F0F;            /* primary text */
  --ink-2:      #6B6B6B;            /* secondary text */
  --ink-3:      #9A9A9A;            /* faint */
  --line:       rgba(0,0,0,.08);    /* hairline */
  --line-2:     rgba(0,0,0,.16);    /* card / button border */

  /* accent — the only color in the system */
  --accent:       #FF3D00;          /* pure vermillion */
  --accent-press: #E53600;          /* hover / pressed */
  --accent-soft:  #FFF0EB;          /* only for inline code bg */

  /* typography (unchanged) */
  --sans: "Inter", "Noto Sans SC", -apple-system, "PingFang SC", "Microsoft YaHei", sans-serif;
  --display: "Inter", "Noto Sans SC", -apple-system, "PingFang SC", "Microsoft YaHei", sans-serif;
  --mono: "IBM Plex Mono", ui-monospace, "SF Mono", Consolas, monospace;
}
```

**Step 2: Delete the v3 override on line 275**

Old:
```css
:root { --surface: #F6F5F1; --line: rgba(33,29,24,.10); --line-2: rgba(33,29,24,.16); }
```
Action: delete the entire line. The new tokens already provide `--line` and `--line-2` (now neutral, per design §1.1).

**Step 3: Delete the v3 override on line 301**

Old:
```css
:root { --paper: #F5F4F0; --paper-2: #FAF9F6; --panel: #FFFFFF; --cream: #F2F0EA; }
```
Action: delete the entire line. All four tokens are removed from the system.

**Step 4: Verify the new tokens are defined and the deleted tokens are gone**

Run:
```bash
grep -n -E '(--bg:|--bg-soft:|--ink:|--ink-2:|--ink-3:|--line:|--line-2:|--accent:|--accent-press:|--accent-soft:)' landing/index.html
```
Expected: 9 matches, one per token, all on contiguous lines inside the new `:root` block (lines 14–34).

Run:
```bash
grep -n -E '(--paper|--coral|--cream|--panel|--surface|--leaf|--shadow|--halftone)' landing/index.html
```
Expected: **no matches** (zero). These tokens must not exist in the file any more.

**Step 5: Commit**

```bash
git add landing/index.html
git commit -m "landing: replace :root palette with v4 (white + pure vermillion)"
```

---

## Task 2: Migrate body + nav

**Files:**
- Modify: `landing/index.html:38` (body background)
- Modify: `landing/index.html:46` (`::selection`)
- Modify: `landing/index.html:111` (nav.scrolled background)
- Modify: `landing/index.html:114` (logo em)
- Modify: `landing/index.html:117` (nav link hover)
- Modify: `landing/index.html:125` (lang toggle hover)
- Modify: `landing/index.html:303` (v3 nav.scrolled override)

**Step 1: Update body background**

Line 38, old:
```css
body {
  background: var(--paper);
```
Change to:
```css
body {
  background: var(--bg);
```

**Step 2: Update `::selection`**

Line 46, old:
```css
::selection { background: var(--coral); color: #fff; }
```
Change to:
```css
::selection { background: var(--accent); color: #fff; }
```

**Step 3: Update nav.scrolled background**

Line 111, old:
```css
nav.scrolled { background: rgba(244,233,214,.86); backdrop-filter: blur(12px); border-bottom-color: var(--ink); }
```
Change to:
```css
nav.scrolled { background: rgba(255,255,255,.86); backdrop-filter: blur(12px); border-bottom-color: var(--line-2); }
```

**Step 4: Delete the v3 nav.scrolled override on line 303**

Old:
```css
nav.scrolled { border-bottom-width: 1px; background: rgba(245,244,240,.82); }
```
Action: delete the entire line. (The new single rule on line 111 already covers both.)

**Step 5: Update logo em + nav link hover + lang toggle**

Line 114, old:
```css
.logo em { font-style: normal; color: var(--coral); }
```
Change to:
```css
.logo em { font-style: normal; color: var(--accent); }
```

Line 117, old:
```css
.nav-links a:hover { color: var(--coral-deep); }
```
Change to:
```css
.nav-links a:hover { color: var(--accent-press); }
```

Line 125, old:
```css
.lang-toggle:hover { background: var(--coral); color: #fff; transform: translate(-1px,-1px); box-shadow: 3px 3px 0 var(--ink); }
```
Change to:
```css
.lang-toggle:hover { background: var(--accent); color: #fff; transform: translate(-1px,-1px); box-shadow: 3px 3px 0 var(--ink); }
```

**Step 6: Verify no warm nav tokens remain**

Run:
```bash
grep -n -E '(244,233,214|245,244,240|--coral|--coral-deep)' landing/index.html
```
Expected: **no matches**.

Run:
```bash
grep -n -E '(--bg|--accent|--ink)' landing/index.html | head -20
```
Expected: matches on the body, selection, nav.scrolled, logo em, nav-links hover, lang-toggle hover lines.

**Step 7: Commit**

```bash
git add landing/index.html
git commit -m "landing: migrate body + nav to v4 tokens"
```

---

## Task 3: Migrate hero typography + buttons (no grid yet)

**Files:**
- Modify: `landing/index.html:64-65` (release chip underline `mark`)
- Modify: `landing/index.html:140` (h1 `.dot`)
- Modify: `landing/index.html:254` (v2 hero h1 `.accent` selector)
- Modify: `landing/index.html:285` (hero-copy h1)
- Modify: `landing/index.html:286` (hero-copy `.sub`)
- Modify: `landing/index.html:290-291` (hero `.btn-solid`)
- Modify: `landing/index.html:292-293` (hero `.btn-ghost`)
- Modify: `landing/index.html:294` (hero `.trust` color)
- Modify: `landing/index.html:342-343` (heading weights)

**Step 1: Migrate the release chip mark (lines 64–65)**

Line 64, old:
```css
background: linear-gradient(var(--coral), var(--coral)) no-repeat;
```
Change to:
```css
background: linear-gradient(var(--accent), var(--accent)) no-repeat;
```

**Step 2: Migrate h1 .dot (line 140)**

Line 140, old:
```css
.hero-copy h1 .dot { color: var(--coral); }
```
Change to:
```css
.hero-copy h1 .dot { color: var(--accent); }
```

**Step 3: Migrate h1 .accent (line 254)**

Line 254, old:
```css
.hero-copy h1 .accent { color: var(--coral); }
```
Change to:
```css
.hero-copy h1 .accent { color: var(--accent); }
```

**Step 4: Migrate hero buttons (lines 290–293)**

Lines 290–291, old:
```css
.hero .btn-solid { background: var(--coral); border-color: var(--coral); color: #fff; }
.hero .btn-solid:hover { background: var(--coral-deep); border-color: var(--coral-deep); }
```
Change to:
```css
.hero .btn-solid { background: var(--accent); border-color: var(--accent); color: #fff; }
.hero .btn-solid:hover { background: var(--accent-press); border-color: var(--accent-press); }
```

**Step 5: Soften the hero ghost button border (line 292)**

Line 292, old:
```css
.hero .btn-ghost { background: #fff; border: 1.5px solid var(--line-2); color: var(--ink); }
```
Change: unchanged (already uses `--line-2`). No edit needed.

**Step 6: Verify no warm hero tokens remain**

Run:
```bash
grep -n -E '(--coral|--coral-deep|--coral-soft)' landing/index.html
```
Expected: matches only in the button shadows elsewhere in the file (model section, footer) — these are migrated in Tasks 5 and 7. If you see matches in the hero, you missed an edit.

**Step 7: Commit**

```bash
git add landing/index.html
git commit -m "landing: migrate hero typography + buttons to v4"
```

---

## Task 4: Migrate install command + inline code

**Files:**
- Modify: `landing/index.html:70` (`.code.inline` block)
- Modify: `landing/index.html:155-156` (`.install-line .dollar` + `.copy-hint`)
- Modify: `landing/index.html:149-150` (`.install-line` border, originally 2.5px ink)

**Step 1: Soften the install command box border**

Line 149, old:
```css
.install-line { /* ... */ background: #fff; border: 2.5px solid var(--ink); border-radius: 12px; }
```
Find the actual `border` declaration (split across the file by the historical v1/v2/v3 cascade — search for `2.5px solid var(--ink)` inside the install area). Change to `1.5px solid var(--line-2)` so it stops looking like a comic-book panel.

**Step 2: Migrate the `$` prompt color**

Line 155, old:
```css
.install-line .dollar { color: var(--coral-deep); }
```
Change to:
```css
.install-line .dollar { color: var(--accent); }
```

**Step 3: Migrate the inline `code` element styling**

Line 70, old:
```css
code.inline { font-family: var(--mono); font-size: .86em; color: var(--coral-deep); background: #fff; border: 1.5px solid var(--ink); padding: 0 6px; border-radius: 6px; }
```
Change to:
```css
code.inline { font-family: var(--mono); font-size: .86em; color: var(--accent-press); background: var(--accent-soft); border: 1.5px solid var(--line-2); padding: 0 6px; border-radius: 6px; }
```

**Step 4: Verify no warm install/code tokens remain**

Run:
```bash
grep -n -E '(--coral-deep)' landing/index.html
```
Expected: **no matches** after this task. (Coral-deep is now fully eliminated; only `--accent-press` and `--accent` remain.)

**Step 5: Commit**

```bash
git add landing/index.html
git commit -m "landing: migrate install command + inline code to v4"
```

---

## Task 5: Migrate model marquee + section backgrounds

**Files:**
- Modify: `landing/index.html:179-180` (`.marquee .logo` + `:hover`)
- Modify: `landing/index.html:182` (`.logo.more`)
- Modify: `landing/index.html:307-310` (`.card`, `.capcard`, `.capcard:hover`, `.surface:hover`)
- Modify: `landing/index.html:320-321` (`.models`, `.everywhere`, `.faq-section` backgrounds)
- Modify: `landing/index.html:309` (the later v3 `.capcard { background: #FCFAF6; }`)

**Step 1: Migrate model marquee logo colors (lines 179–182)**

Line 179, old:
```css
.logo { display: inline-flex; align-items: center; gap: 15px; color: var(--ink); font-family: var(--display); font-weight: 600; font-size: 26px; white-space: nowrap; opacity: .72; transition: opacity .2s, color .2s; }
```
Change: keep all properties, but ensure `color: var(--ink)` (it already is — leave it). No change needed unless you've touched this line.

Line 180, old:
```css
.logo:hover { opacity: 1; color: var(--coral-deep); }
```
Change to:
```css
.logo:hover { opacity: 1; color: var(--accent-press); }
```

**Step 2: Migrate `.models` / `.everywhere` / `.faq-section` backgrounds**

Line 320, old:
```css
.models { border-top: 1px solid var(--line-2); border-bottom: 1px solid var(--line-2); background: var(--paper-2); }
```
Change to:
```css
.models { border-top: 1px solid var(--line-2); border-bottom: 1px solid var(--line-2); background: var(--bg); }
```

Line 321, old:
```css
.everywhere, .faq-section { border-top: 1px solid var(--line-2); border-bottom: 1px solid var(--line-2); background: var(--paper-2); }
```
Change to:
```css
.everywhere, .faq-section { border-top: 1px solid var(--line-2); border-bottom: 1px solid var(--line-2); background: var(--bg-soft); }
```

**Step 3: Migrate `.card` and `.capcard` to neutral shadows + white ground**

Line 307, old:
```css
.card { border: 1.5px solid var(--line-2); border-radius: 14px; box-shadow: 0 1px 2px rgba(33,29,24,.04), 0 12px 28px -20px rgba(33,29,24,.20); }
```
Change to:
```css
.card { border: 1.5px solid var(--line-2); border-radius: 14px; box-shadow: 0 1px 2px rgba(0,0,0,.04), 0 12px 28px -20px rgba(0,0,0,.20); }
```

Line 309 (v3 capcard), old:
```css
.capcard { background: #FCFAF6; }
```
Change to:
```css
.capcard { background: #fff; }
```

Line 310, old:
```css
.capcard:hover, .surface:hover { transform: translateY(-3px); box-shadow: 0 2px 4px rgba(33,29,24,.05), 0 18px 36px -20px rgba(33,29,24,.24); }
```
Change to:
```css
.capcard:hover, .surface:hover { transform: translateY(-3px); box-shadow: 0 2px 4px rgba(0,0,0,.05), 0 18px 36px -20px rgba(0,0,0,.24); }
```

**Step 4: Verify all section backgrounds + card shadows are neutral**

Run:
```bash
grep -n -E '(rgba\(33,29,24|#FCFAF6|#F6F5F1|#F2F0EA|--paper-2)' landing/index.html
```
Expected: **no matches** after this task.

**Step 5: Commit**

```bash
git add landing/index.html
git commit -m "landing: migrate section backgrounds + cards to v4 neutrals"
```

---

## Task 6: Migrate FAQ + credit hover + everywhere pseudo

**Files:**
- Modify: `landing/index.html:211` (`.faq-list summary:hover`)
- Modify: `landing/index.html:218` (`.faq-list details[open] summary`)
- Modify: `landing/index.html:219-220` (`.faq-list details p` link)
- Modify: `landing/index.html:229-230` (`.credit .n::after`)
- Modify: `landing/index.html:231` (`.credit p`)
- Modify: `landing/index.html:311` (`.credit:hover` shadow)
- Modify: `landing/index.html:322` (`.everywhere::before` — already `display: none`)

**Step 1: Migrate FAQ summary colors**

Line 211, old:
```css
.faq-list summary:hover { color: var(--coral-deep); }
```
Change to:
```css
.faq-list summary:hover { color: var(--accent-press); }
```

Line 218, old:
```css
.faq-list details[open] summary { color: var(--coral-deep); }
```
Change to:
```css
.faq-list details[open] summary { color: var(--accent-press); }
```

Line 220, old:
```css
.faq-list details p a { color: var(--coral-deep); font-weight: 600; }
```
Change to:
```css
.faq-list details p a { color: var(--accent-press); font-weight: 600; }
```

**Step 2: Migrate credit arrow + shadow**

Line 230, old:
```css
.credit .n::after { content: "↗"; font-size: 13px; color: var(--coral-deep); }
```
Change to:
```css
.credit .n::after { content: "↗"; font-size: 13px; color: var(--accent-press); }
```

Line 311, old:
```css
.credit:hover { transform: translateY(-3px) rotate(0deg); box-shadow: 0 2px 4px rgba(33,29,24,.05), 0 18px 36px -20px rgba(33,29,24,.24); }
```
Change to:
```css
.credit:hover { transform: translateY(-3px) rotate(0deg); box-shadow: 0 2px 4px rgba(0,0,0,.05), 0 18px 36px -20px rgba(0,0,0,.24); }
```

**Step 3: Verify no FAQ/credit warm tokens remain**

Run:
```bash
grep -n -E '(--coral-deep|rgba\(33,29,24)' landing/index.html
```
Expected: matches only on lines 324, 332, 338 that we haven't touched yet (hero shot, footer mascot, hero-art box shadow). Tasks 7 + 8 will address those.

**Step 4: Commit**

```bash
git add landing/index.html
git commit -m "landing: migrate FAQ + credit to v4 tokens"
```

---

## Task 7: Migrate footer + remaining shadows

**Files:**
- Modify: `landing/index.html:234` (footer background)
- Modify: `landing/index.html:235` (footer halftone overlay)
- Modify: `landing/index.html:240-241` (footer CTA buttons)
- Modify: `landing/index.html:245-246` (footer meta + link)
- Modify: `landing/index.html:324` (`.hero-shot .frame` shadow)
- Modify: `landing/index.html:327` (footer `border-top`)
- Modify: `landing/index.html:329-335` (footer cta text + meta)
- Modify: `landing/index.html:332` (`.footer-mascot .stage` shadow)
- Modify: `landing/index.html:333-335` (footer meta text)
- Modify: `landing/index.html:338` (`.hero-art` shadow)

**Step 1: Migrate footer background**

Line 234, old:
```css
footer { position: relative; padding: 92px 0 44px; background: var(--coral); border-top: 2.5px solid var(--ink); overflow: hidden; color: #fff; }
```
Change to:
```css
footer { position: relative; padding: 92px 0 44px; background: var(--ink); border-top: 1px solid var(--line-2); overflow: hidden; color: #fff; }
```

**Step 2: Update footer halftone overlay**

Line 235, old:
```css
footer::before { content: ""; position: absolute; inset: 0; background: radial-gradient(#fff 1.4px, transparent 1.5px); background-size: 22px 22px; opacity: .12; pointer-events: none; }
```
Change: leave the radial-gradient but reduce opacity to `.08` so it doesn't compete with the new clean system. (Optional — if it looks fine in Task 9 verification, leave at `.12`.)

**Step 3: Migrate footer CTA buttons**

Line 240, old:
```css
.footer-cta .btn { background: var(--panel); color: var(--ink); }
```
Change to:
```css
.footer-cta .btn { background: #fff; color: var(--ink); }
```

Line 241, old:
```css
.footer-cta .btn-solid { background: var(--ink); color: #fff; }
```
Change to:
```css
.footer-cta .btn-solid { background: var(--accent); color: #fff; }
```

**Step 4: Migrate footer meta + link**

Line 245, old:
```css
.footer-meta { position: relative; z-index: 2; margin-top: 72px; padding-top: 22px; border-top: 2.5px solid rgba(33,29,24,.35); display: flex; flex-wrap: wrap; gap: 14px 36px; justify-content: space-between; font-size: 13.5px; color: rgba(255,255,255,.85); }
```
Change to:
```css
.footer-meta { position: relative; z-index: 2; margin-top: 72px; padding-top: 22px; border-top: 1px solid rgba(255,255,255,.16); display: flex; flex-wrap: wrap; gap: 14px 36px; justify-content: space-between; font-size: 13.5px; color: rgba(255,255,255,.6); }
```

Line 246, old:
```css
.footer-meta a { color: #fff; text-decoration: none; font-weight: 500; }
```
Change: unchanged (already `#fff`).

**Step 5: Migrate the v3 footer overrides (lines 327, 329–335)**

Line 327, old:
```css
footer { background: var(--ink); border-top: 1px solid var(--line-2); }
```
Action: delete the entire line. (Step 1 above already sets `background: var(--ink)` on line 234; the override is now redundant.)

Line 329, old:
```css
.footer-cta h2 { color: #fff; }
```
Change: unchanged.

Line 330, old:
```css
.footer-cta .btn-solid { background: var(--coral); border-color: var(--coral); color: #fff; }
```
Change to:
```css
.footer-cta .btn-solid { background: var(--accent); border-color: var(--accent); color: #fff; }
```

Line 331, old:
```css
.footer-cta .btn:not(.btn-solid) { background: transparent; border-color: rgba(255,255,255,.35); color: #fff; }
```
Change: unchanged.

Line 332, old:
```css
.footer-mascot .stage { border: 1.5px solid #000; box-shadow: 0 20px 44px -22px rgba(0,0,0,.55); }
```
Change: unchanged (already neutral).

Line 333, old:
```css
.footer-meta { border-top-color: rgba(255,255,255,.16); color: rgba(255,255,255,.6); }
```
Action: delete the entire line. (Step 4 above already sets these properties on line 245.)

Line 334, old:
```css
.footer-meta a { color: rgba(255,255,255,.85); }
```
Change to:
```css
.footer-meta a { color: rgba(255,255,255,.85); }
```
(no change — this is the new final value)

Line 335, old:
```css
.footer-meta a:hover { color: #fff; }
```
Change: unchanged.

**Step 6: Migrate hero shot + hero-art shadows**

Line 324, old:
```css
.hero-shot .frame { border-width: 1.5px; box-shadow: 0 30px 60px -30px rgba(33,29,24,.30), 0 3px 10px rgba(33,29,24,.05); }
```
Change to:
```css
.hero-shot .frame { border-width: 1.5px; box-shadow: 0 30px 60px -30px rgba(0,0,0,.30), 0 3px 10px rgba(0,0,0,.05); }
```

Line 338, old:
```css
.hero-art { background: #FAF1E9; border: 1.5px solid var(--line-2); border-radius: 18px; overflow: hidden; box-shadow: 0 34px 64px -32px rgba(33,29,24,.30), 0 3px 10px rgba(33,29,24,.05); transform: rotateX(var(--rx,0deg)) rotateY(var(--ry,0deg)); transition: transform .25s ease-out; will-change: transform; }
```
Change to:
```css
.hero-art { background: var(--bg-soft); border: 1.5px solid var(--line-2); border-radius: 18px; overflow: hidden; box-shadow: 0 34px 64px -32px rgba(0,0,0,.30), 0 3px 10px rgba(0,0,0,.05); transform: rotateX(var(--rx,0deg)) rotateY(var(--ry,0deg)); transition: transform .25s ease-out; will-change: transform; }
```

**Step 7: Verify no warm shadow or footer tokens remain**

Run:
```bash
grep -n -E '(rgba\(33,29,24|#FAF1E9|--panel|--paper-2|--coral)' landing/index.html
```
Expected: **no matches**.

**Step 8: Commit**

```bash
git add landing/index.html
git commit -m "landing: migrate footer + remaining shadows to v4"
```

---

## Task 8: Hero grid rewrite (Dify-style column frame)

**Files:**
- Modify: `landing/index.html:278-281` (v3 `header.hero` background + ::before display)
- Modify: `landing/index.html:345` (the later v3 200px grid override)

**Step 1: Replace the hero background with the Dify-style frame**

Lines 278–281, old:
```css
header.hero { text-align: left; padding: 128px 0 78px; overflow: visible; background: var(--surface);
  background-image: linear-gradient(var(--line) 1px, transparent 1px), linear-gradient(90deg, var(--line) 1px, transparent 1px);
  background-size: 46px 46px; background-position: center top; }
header.hero::before { display: none; }
```

Change to:
```css
header.hero { text-align: left; padding: 128px 0 78px; overflow: visible; background: var(--bg);
  border: 1px solid rgba(0,0,0,.05);
  position: relative; }
header.hero::before { display: none; }
```

(The `position: relative` is required so the absolute-positioned column dividers anchor to the hero.)

**Step 2: Delete the v3 200px grid override on line 345**

Line 345, old:
```css
header.hero { background-size: 200px 200px; background-image: linear-gradient(rgba(33,29,24,.05) 1px, transparent 1px), linear-gradient(90deg, rgba(33,29,24,.05) 1px, transparent 1px); }
```
Action: delete the entire line.

**Step 3: Add the column-edge frame as child elements inside `.wrap`**

Find the hero's `.wrap` opening tag (line 561 area):
```html
<header class="hero" id="hero">
  <div class="wrap">
```

Insert four absolute-positioned vertical dividers **after the `<div class="wrap">` opening tag, before the first child** (`<div class="hero-copy">`). Use the actual `.9fr 1.1fr` column split — given a `.wrap` max-width of `~1180px` and 56px gap, the lines go at approximately these positions from the left edge of `.wrap`:

```html
<header class="hero" id="hero">
  <div class="wrap">
    <span class="col-line" style="left: 0%"></span>
    <span class="col-line" style="left: 22.5%"></span>
    <span class="col-line" style="left: 47%"></span>
    <span class="col-line" style="left: 100%"></span>
    <div class="hero-copy">
```

(The 4 lines mark: left edge of `.wrap`, the `.9fr` column boundary (the right edge of the text column), the `.9fr + 56px gap` start (left edge of hero-art), and right edge of `.wrap`. These are approximate — measure the actual content column edges in the rendered page and adjust the percentages to match. Aim for the lines to fall exactly on the column edges, not float in the gutter.)

**Step 4: Add the `.col-line` style**

Add to the CSS (right after the `header.hero` rule you rewrote in Step 1):

```css
header.hero .col-line { position: absolute; top: 0; bottom: 0; width: 1px; background: rgba(0,0,0,.05); pointer-events: none; }
```

**Step 5: Verify the grid rewrite removed both old lattice rules**

Run:
```bash
grep -n -E '(background-image: linear-gradient|background-size: (46px|200px))' landing/index.html
```
Expected: **no matches** in the hero context. (`background-image: linear-gradient(var(--accent)` on the mark underline is fine — it doesn't have a `background-size`.)

**Step 6: Commit**

```bash
git add landing/index.html
git commit -m "landing: hero grid → Dify-style column-edge frame"
```

---

## Task 9: Add focus rings + reduced-motion guard + theme-color meta

**Files:**
- Modify: `landing/index.html:8` (`<meta name="theme-color">`)
- Modify: `landing/index.html:35-49` area (add a `:focus-visible` rule)
- Modify: `landing/index.html:296-298` area (add a `prefers-reduced-motion` guard for the hero tilt)

**Step 1: Update the theme-color meta tag**

Line 8, old:
```html
<meta name="theme-color" content="#F4E9D6">
```
Change to:
```html
<meta name="theme-color" content="#FFFFFF">
```

**Step 2: Add a `:focus-visible` rule**

Find the `::selection` rule (line 46) and add this directly after it:

```css
:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; border-radius: 4px; }
```

**Step 3: Add a `prefers-reduced-motion` guard for the hero tilt**

Find the `.hero-art` rule (around line 338) and add this directly after it:

```css
@media (prefers-reduced-motion: reduce) {
  .hero-art, .tiltable { transform: none !important; transition: none !important; }
  .hero-art { will-change: auto; }
}
```

**Step 4: Verify the additions are in place**

Run:
```bash
grep -n -E '(theme-color|:focus-visible|prefers-reduced-motion)' landing/index.html
```
Expected: 3 matches — one on line ~8 (meta tag), one on the `:focus-visible` rule, one on the `@media` block.

**Step 5: Commit**

```bash
git add landing/index.html
git commit -m "landing: focus rings, reduced-motion guard, white theme-color"
```

---

## Task 10: Full-file audit + accessibility check

**Files:**
- Audit: `landing/index.html` (read-only, no edits expected)

**Step 1: Confirm zero forbidden tokens remain anywhere in the file**

Run:
```bash
grep -n -E '(--paper|--paper-2|--panel|--cream|--surface|--leaf|--shadow|--halftone|--coral|--coral-deep|--coral-soft|#F4E9D6|#F8F0E1|#FDF8F3|#F2E7D4|#EE6A48|#DE5530|#F9C9B7|#5C9C7A|rgba\(33,29,24)' landing/index.html
```
Expected: **no matches**. If any match, locate the line, return to the task that owned that area, and fix.

**Step 2: Confirm every new token is actually used**

Run:
```bash
for tok in '--bg' '--bg-soft' '--ink' '--ink-2' '--ink-3' '--line' '--line-2' '--accent' '--accent-press' '--accent-soft'; do
  count=$(grep -c -E "$tok:" landing/index.html)
  echo "$tok: defined in :root = 1, used = $((count - 1))"
done
```
Expected: every token shows "used = N" where N > 0. If any token is unused, either delete it from `:root` (YAGNI) or add the missing use site (more likely correct — surface `--bg-soft` is needed for the hero frame ground; `--ink-3` is needed for the hero trust line; etc.).

**Step 3: Serve the page locally and visually verify**

```bash
cd /Users/zzzz/wuu/landing
python3 -m http.server 8765 &
SERVER_PID=$!
sleep 1
echo "Server running on http://localhost:8765/index.html"
```

Open `http://localhost:8765/index.html` in a browser. Verify:
- Background is pure white, not cream.
- Body text is near-black, not warm.
- The single orange accent appears in: (a) the `wuu.` logo dot, (b) the hero "Get started" button, (c) the install command `$`, (d) the footer CTA button.
- The hero has a faint outer frame with 4 vertical column lines inside, **no** horizontal grid lattice.
- The round-table image is still in the hero right column with its speech bubbles.
- The footer is now neutral black, not coral.

When done:

```bash
kill $SERVER_PID
```

**Step 4: Accessibility / contrast spot-check**

Use the WebFetch tool against the served page (or open the page in a browser and use the Accessibility Inspector) to verify:
- Body text on body: contrast ≥ 13:1 (AAA) — should pass trivially.
- Orange button on white: contrast 4.06:1 (AA Large + AA UI) — pass.
- Orange `$` prompt on white: same as above — pass.

If anything fails, return to the offending token and adjust — but **do not** change `--accent` away from `#FF3D00` (the design is locked). The only legal adjustments are: changing the *use site* to a different token (e.g., move a non-CTA accent usage to `--ink` if contrast is the issue), or raising the *size* of the text using the accent until it qualifies as Large.

**Step 5: Final commit (only if Step 1 caught something you fixed)**

If you fixed any token in this task:

```bash
git add landing/index.html
git commit -m "landing: v4 final audit cleanup"
```

If Step 1 came back clean (the common case), there is nothing to commit — proceed to handoff.

---

## Handoff

When all 10 tasks are complete:
- `landing/index.html` is on v4 — pure white, neutral text, single vermillion accent, Dify-style column-edge hero frame.
- Mascots, the round-table image, and all JS / copy / typography are untouched.
- 9 atomic commits on `main` (or one fewer if Task 10 needed no fixup).

Suggested next step: visually review the rendered page side-by-side with the v3 state in the browser, confirm the new design is what the user signed off on, and merge / open a PR per project convention.
