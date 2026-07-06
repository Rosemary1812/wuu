# Landing Page Color Redesign — Design

Status: approved (2026-07-06). Touches `landing/index.html` only.
The mascots in `landing/assets/mascot/` stay untouched on purpose.

## 0. Direction

The landing has been iterated toward an editorial / mature dev-tool look
(commits `542d177a` … `6cb708f9` strip the comic-book shadows, dead v1/v2
CSS, hero eyebrow, redundant section labels). The current `:root` palette
is the last holdout from the warm-comic era: cream backgrounds, warm-ink
text, coral accent, a `--leaf` success green. The result is **temperature
incoherence** — warm hero (cream) vs. cold footer (`#211D18`) vs. warm
CTA (`#EE6A48`).

v4 collapses the system to a Linear / Vercel / Stripe pattern:
**~95% neutral, ~5% screaming accent, used as sparingly as possible**.
The accent is the equivalent of `#0133FF` (pure, ~100% saturation, mid
lightness) shifted into the orange hue range. The cream + black-ink
mascot set — which already lives in the same warm-cream world as the
current palette — actually gains contrast against a pure white ground.

## 1. Tokens

### 1.1 New `:root` (replaces lines 14–34 and the v3 overrides at 275 / 301)

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
}
```

### 1.2 Tokens being deleted

`--paper`, `--paper-2`, `--panel`, `--cream`, `--surface`, `--coral`,
`--coral-deep`, `--coral-soft`, `--leaf`, `--shadow`, `--halftone`.

All existing call sites (lines 38, 82, 83, 88, 94, 100–125, 230, 234,
240–246, 268, 309, 310, 320, 321, 327, 333–335, 347, 348) migrate to
the new tokens per the table in §3.

### 1.3 Selection / focus

- `::selection` background: `--accent`, foreground `#fff` (line 46).
- `:focus-visible` ring: 2px `--accent` outline, 2px offset, on every
  interactive element (currently absent; the v3 strip pass removed
  button shadows without adding a focus replacement).

## 2. Accent budget — four places only

1. **Primary CTA** — `background: var(--accent); color: #fff`. Hero
   "Get started" button, footer CTA button, install command `copy` hint
   (the latter is a small UI hint, not a button).
2. **Logo accent** — the `.` after `wuu` in the nav (current line 114,
   `--coral`). Migrates to `--accent`.
3. **Key link hover** — `.nav-links a:hover` (line 117), FAQ summary
   hover + open (lines 211, 218), release chip `arr` (line 567), model
   logo hover (line 180), and any inline link in copy. Migrate
   `--coral-deep` → `--accent-press`.
4. **Install command `$`** — the prompt prefix at line 587 (current
   `--coral-deep`).

**Forbidden zones** (state explicitly so future commits don't drift
back):

- Headlines, subheadings, body text.
- Section eyebrows / kicker labels.
- Secondary / ghost buttons (stay white + `--line-2` border + `--ink`
  text; hover border → `--ink`).
- Card backgrounds, dividers, marquee background.
- Decorative line under the release chip (current line 64 — `mark`
  highlight), FAQ chevron, install command `copy` background.
- The hero grid frame (see §4).
- The mascot art (cream body + black ink) and the round-table image's
  speech bubbles' backgrounds.

**Simultaneous rule**: at most **two accent blocks on screen at once**.
The hero CTA + footer CTA is one accepted pair (different sections).
The logo dot is a non-block character and does not count.

## 3. Section migration table

| Section | Current state | v4 state |
|---|---|---|
| `<body>` | `background: var(--paper)` | `background: var(--bg)` |
| Nav (`.logo em`) | `color: var(--coral)` | `color: var(--accent)` |
| Nav link hover | `color: var(--coral-deep)` | `color: var(--accent-press)` |
| Nav scrolled | `background: rgba(244,233,214,.86)` (warm) | `background: rgba(255,255,255,.86)` |
| Lang toggle hover | `bg --coral` | `bg --accent` |
| Hero | `bg var(--surface)` + 200px grid + warm lines | `bg var(--bg)` + see §4 (column-edge frame) |
| Hero h1 | `--ink` (warm) | `--ink` (neutral — no change in hex family, token swap) |
| Hero h1 `.dot` / `.accent` | `--coral` | `--accent` |
| Hero `.btn-solid` | `bg --coral` | `bg --accent` |
| Hero `.btn-solid:hover` | `bg --coral-deep` | `bg --accent-press` |
| Hero `.btn-ghost` | `bg #fff; border --line-2; color --ink` | unchanged |
| Install command box | `bg #fff; border 1.5px --ink` | `bg #fff; border 1.5px --ink-2` (soften the comic ink) |
| Install command `$` | `--coral-deep` | `--accent` |
| Model marquee | `bg --paper-2` | `bg var(--bg)` with `border-y: 1px solid var(--line-2)` |
| Card | `border 1.5px --line-2; box-shadow` (warm `rgba(33,29,24,…)`) | `border 1.5px --line-2; box-shadow` with `rgba(0,0,0,…)` (neutral) |
| `.capcard` | `bg #FCFAF6` (warm) | `bg #fff` |
| `.everywhere`, `.faq-section` | `bg --paper-2` | `bg var(--bg-soft)` (`#FAFAFA`) |
| FAQ summary hover / open | `--coral-deep` | `--accent-press` |
| FAQ details border | `--line-2` | unchanged |
| `.hero-shot .frame` shadow | warm `rgba(33,29,24,…)` | `rgba(0,0,0,…)` |
| Footer | `bg --ink` (warm) | `bg --ink` (neutral — same `#0F0F0F` token, no drift) |
| Footer CTA (`.btn-solid`) | `bg --coral` | `bg --accent` |
| Footer ghost button border | `rgba(255,255,255,.35)` | unchanged |
| Footer meta link color | `rgba(255,255,255,.85)` | unchanged |
| `code.inline` | `color --coral-deep; bg #fff; border 1.5px --ink` | `color --accent-press; bg --accent-soft; border 1.5px --line-2` |

## 4. Hero grid — Dify-style column edge frame

Reference: the Dify landing page hero uses a faint rectangular frame
(1px, `rgba(0,0,0,…)` at very low opacity) around the hero block, with
several vertical column dividers inside, all the same weight, in
asymmetric column widths. There is no horizontal grid lattice; the
frame and the column lines **are** the layout's column structure
made visible.

### 4.1 Replaces

The current `header.hero` background (lines 278–280 then overridden at
line 345) — a 200px × 200px uniform lattice with both horizontal and
vertical lines, all `rgba(33,29,24,.05)`. Pure decoration, no
information.

### 4.2 Implementation

- 1px frame on all four sides of the hero, color
  `rgba(0,0,0,.05)`, drawn with `border` (or `outline` if the existing
  `overflow: visible` is needed for the tilted hero-art).
- **No background-image grid** — delete both `background-image` rules.
- Vertical column dividers: 4 inner lines, placed to mark the
  *actual* `.9fr 1.1fr` content columns plus two intermediate guide
  lines so the eye can read the rhythm. Implementation options:
  - (preferred) absolutely-positioned `::before` / `::after` / child
    `span` elements inside `.wrap`, `1px wide`, `bg rgba(0,0,0,.05)`,
    `height: 100%`.
  - or CSS `linear-gradient` background-image on the hero with only
    vertical stops (no horizontal stops), aligned to the column
    grid.
- Lines are `rgba(0,0,0,.05)`, **never** `--accent`. The accent is
  reserved for content that must be acted on; the frame is structural
  furniture and stays neutral.
- The `header.hero::before` halftone wash (line 131) and the
  v3 `::before` override (`display: none` at line 281) remain
  `display: none`.

### 4.3 Mobile

At `max-width: 900px` the hero collapses to a single column (line
297). The frame stays (4-sided). The 4 inner vertical dividers become
2 (one mid-line as a centering guide, one at the column edge), or
collapse to zero. Decision during implementation: keep one mid-line
for typographic rhythm, drop the rest.

## 5. Round-table image placement

`landing/assets/mascot/wuu-mascot-concept-27.png` (the "agents
gathered around one table" image) **stays in the hero right column**
(current location, line 598). Reasoning: it is the literal visual
anchor of the headline "Agents, one group chat" — splitting it into
a separate section would weaken the first-screen narrative.

- The `.tiltable` 3D-tilt effect on `.hero-art` is kept (existing
  behavior, lines 296, 338–339, 351).
- The two speech bubbles (`@石头 交给你了` / `测试全绿 ✓`,
  lines 599–600) keep their `var(--coral-soft)` style updated to
  `var(--bg-soft)` and their text color to `var(--ink)`. No accent
  on the bubbles — they are narrative captions, not CTAs.
- The `.hero-art` background `var(--bg-soft)` overrides
  `var(--coral-soft)`; the cartoon image carries its own warm tones
  and reads fine on neutral.

## 6. Accessibility

- `#FF3D00` on `#FFFFFF`: contrast ratio **4.06 : 1**. Passes WCAG
  AA for Large Text and UI Components (≥ 3 : 1). Fails AA for body
  text (≥ 4.5 : 1). The accent rule (§2) is the enforcement: no
  body text uses `--accent`, so the only legitimate uses are
  buttons, large display text (none currently), and the logo dot.
- All text uses neutral `--ink` family; contrast on `--bg` is
  ≥ 13 : 1 (WCAG AAA).
- Focus rings (§1.3) restore keyboard navigation visibility that the
  v3 pass removed when it stripped button shadows.
- Reduce-motion: the `.tiltable` 3D-tilt should respect
  `prefers-reduced-motion: reduce` (currently does not). Add the
  media query to disable the tilt transform.

## 7. What is explicitly out of scope

- Typography (Inter + Noto Sans SC, Fraunces / Noto Serif SC) —
  unchanged.
- Copy, FAQ, model roster, footer CTA copy — unchanged.
- Mascot assets — unchanged.
- Dark mode — not introduced in this pass. The system is built so a
  dark counterpart is mechanically derivable later: invert the
  neutrals, keep `--accent` (it works on both grounds).
- `desktop.png` product screenshot — unchanged.

## 8. File-level summary of changes

Single file: `landing/index.html`. Approximate change footprint:

- Replace `:root` block (lines 14–34) with §1.1.
- Delete the v3 `:root` overrides at lines 275 and 301 (they only
  existed to override the v1/v2 cream palette).
- Migrate every `--coral` / `--coral-deep` / `--coral-soft` /
  `--paper*` / `--panel` / `--cream` / `--surface` / `--leaf` /
  `--shadow` call site per the table in §3.
- Rewrite the hero background per §4.2.
- Add `prefers-reduced-motion` guard for the hero tilt.
- Add `:focus-visible` ring per §1.3.
- No other files change. No JS changes. No asset changes.
