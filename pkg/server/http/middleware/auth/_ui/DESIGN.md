---
name: Turna Auth Console
description: A typographic instrument register for the Turna auth middleware — ruled, sealed, square-cornered, with wax red reserved for what cannot be undone.
colors:
  ground: "#dce1db"
  sheet: "#f2f4f0"
  raised: "#e8ebe4"
  ink: "#16191a"
  muted: "#565f59"
  faint: "#6f7872"
  rule: "#c2c9bf"
  rule-strong: "#16191a"
  seal: "#a3252b"
  carbon: "#2a4674"
  endorsed: "#2f6b4a"
  caution: "#7d5110"
  on-carbon: "#ffffff"
  on-seal: "#ffffff"
  vault-ground: "#101311"
  vault-sheet: "#181c19"
  vault-raised: "#1f2420"
  vault-ink: "#e4e9e2"
  vault-muted: "#98a199"
  vault-faint: "#8a938c"
  vault-rule: "#2c332d"
  vault-rule-strong: "#e4e9e2"
  vault-seal: "#e4737a"
  vault-carbon: "#7fa8e8"
  vault-endorsed: "#4fa97a"
  vault-caution: "#c79a3e"
  vault-on-carbon: "#0c1220"
  vault-on-seal: "#1a0708"
typography:
  display:
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, Liberation Mono, Courier New, monospace"
    fontSize: "4.5rem"
    fontWeight: 700
    lineHeight: 0.9
    letterSpacing: "-0.035em"
    fontVariant: "tabular-nums"
  headline:
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, Liberation Mono, Courier New, monospace"
    fontSize: "2.75rem"
    fontWeight: 700
    lineHeight: 0.95
    letterSpacing: "-0.03em"
    fontVariant: "tabular-nums"
  title:
    fontFamily: "ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, Segoe UI, sans-serif"
    fontSize: "1.85rem"
    fontWeight: 700
    lineHeight: 1.15
    letterSpacing: "-0.02em"
  body:
    fontFamily: "ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, Segoe UI, sans-serif"
    fontSize: "13px"
    fontWeight: 400
    lineHeight: 1.6
    letterSpacing: "normal"
  label:
    fontFamily: "ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, Segoe UI, sans-serif"
    fontSize: "10.5px"
    fontWeight: 600
    lineHeight: 1.4
    letterSpacing: "0.15em"
  label-literal:
    fontFamily: "ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, Segoe UI, sans-serif"
    fontSize: "11px"
    fontWeight: 500
    lineHeight: 1.4
    letterSpacing: "0.01em"
  code:
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, Liberation Mono, Courier New, monospace"
    fontSize: "12.5px"
    fontWeight: 400
    lineHeight: 1.65
    letterSpacing: "normal"
rounded:
  xs: "1px"
  sm: "2px"
  md: "2px"
  lg: "3px"
spacing:
  hair: "0.375rem"
  tight: "0.5rem"
  field: "0.75rem"
  block: "1.25rem"
  masthead: "1.25rem"
  section: "3rem"
components:
  act:
    backgroundColor: "transparent"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    rounded: "{rounded.sm}"
    padding: "0.375rem 0.8rem"
  act-hover:
    backgroundColor: "color-mix(in srgb, #16191a 8%, transparent)"
  act-disabled:
    textColor: "{colors.faint}"
  act-primary:
    backgroundColor: "{colors.carbon}"
    textColor: "{colors.on-carbon}"
    rounded: "{rounded.sm}"
    padding: "0.375rem 0.8rem"
  act-primary-hover:
    backgroundColor: "color-mix(in srgb, #2a4674 86%, #16191a)"
  act-seal:
    backgroundColor: "transparent"
    textColor: "{colors.seal}"
    rounded: "{rounded.sm}"
    padding: "0.375rem 0.8rem"
  act-seal-hover:
    backgroundColor: "{colors.seal}"
    textColor: "{colors.on-seal}"
  act-quiet:
    backgroundColor: "transparent"
    textColor: "{colors.muted}"
    rounded: "{rounded.sm}"
    padding: "0.375rem 0.4rem"
  act-quiet-hover:
    textColor: "{colors.ink}"
    backgroundColor: "color-mix(in srgb, #16191a 7%, transparent)"
  entry:
    backgroundColor: "transparent"
    textColor: "{colors.ink}"
    rounded: "0"
    padding: "0.3rem 0.1rem"
    size: "13.5px"
    width: "100%"
  entry-focus:
    textColor: "{colors.ink}"
  exhibit:
    backgroundColor: "{colors.raised}"
    textColor: "{colors.ink}"
    typography: "{typography.code}"
    rounded: "{rounded.sm}"
    padding: "0.7rem 0.8rem"
    width: "100%"
  sheet:
    backgroundColor: "{colors.sheet}"
    textColor: "{colors.ink}"
    rounded: "0"
  stamp:
    textColor: "{colors.muted}"
    typography: "{typography.label}"
  stamp-ink:
    textColor: "{colors.ink}"
    typography: "{typography.label}"
  stamp-raw:
    textColor: "{colors.muted}"
    typography: "{typography.label-literal}"
  nav-item:
    backgroundColor: "transparent"
    textColor: "{colors.muted}"
    rounded: "0"
    padding: "7px 0.5rem"
    size: "13px"
  nav-item-hover:
    backgroundColor: "{colors.raised}"
    textColor: "{colors.ink}"
  nav-item-active:
    backgroundColor: "{colors.raised}"
    textColor: "{colors.ink}"
  switch-rail-on:
    backgroundColor: "{colors.carbon}"
    rounded: "0"
    width: "34px"
    height: "18px"
  switch-rail-consequential:
    backgroundColor: "{colors.seal}"
    rounded: "0"
    width: "34px"
    height: "18px"
---

# Design System: Turna Auth Console

> **Scope.** This document covers only the embedded auth management console at
> `pkg/server/http/middleware/auth/_ui/`. It does not describe the rest of the
> `turna` repository — loader, preprocess, runner, proxy and the other
> middlewares have no shared visual system with this surface. Product truth for
> this console lives in `../PRODUCT.md`; DESIGN.md owns durable visual
> decisions only.
>
> Recorded from the shipped source (`src/style.css`, `src/components/`,
> `index.html`) after the build, and checked against 33 screenshots of a
> running instance in `.impeccable/review/`.

## Overview

**Creative North Star: "The Key Ceremony"**

Every record in this console is an issued instrument. It carries a serial, a
custody trail and a seal, and the interface's whole job is to state what this
instance currently *is*, who last changed it, and what cannot be undone — then
let the operator change it with the consequence in view. The system is
typographic and never skeuomorphic: there is no rendered wax, no leather, no
faked paper grain. A certificate here is a certificate because of how it is
*set*, not because of what it is textured to resemble.

The category default this world refuses by name is the dashboard of
equal-weight grey cards. Nothing is boxed for the sake of looking contained.
Separation comes from hairline rules, ground shifts between three related
greens, and weight — a section heading sits *on* its rule rather than above a
card. Corners are effectively square (1–3px), because documents do not have
rounded corners. Density is high and deliberately so: operators here work in
JSON records, PEM blocks, LDAP DNs and permission host/path/method triples, and
body copy runs at 13px against a 70ch measure so a page of real material fits
in one reading.

One colour carries the entire moral weight of the system. Wax-seal red
(`#a3252b`) is reserved for the irreversible and for the two places that report
irreversibility — never for emphasis, never for a primary button. Carbon indigo
(`#2a4674`) is the colour of ordinary action. Green endorses, ochre holds. Two
surfaces ship, not one theme with a fallback: **Instrument**, the document plane
read under office light, and **Vault**, the same instruments in the archive.
Both are first-class, so neither may be unreachable at any width.

**Key Characteristics:**
- Rules, not boxes — hairline `#c2c9bf` and one legal double rule per page
- Square corners (1–3px) everywhere, including the boolean switch knob
- Engraved letterspaced caps (10.5px/0.15em) as the console-wide label register
- Every server-sourced value set in mono with tabular numerals
- Wax red reserved exclusively for the irreversible
- Flat by default: exactly one shadow in the entire build, and it is a scrim
- One authored motion moment (a serial re-stamping); everything else is a
  120–150ms state transition

## Colors

The masthead and index rail are vault-coloured on both surfaces and are not part
of this system — the palette below is the plane between them. On the Instrument
surface the grounds are **neutral by decision**, and the entire colour budget is
spent on the four meaning-bearing inks and on the ink itself. The vault inverts
to low-light neutrals.

**Why the plane is not tinted.** Two passes tried it. At low chroma the surfaces
landed a few points off neutral and read as fog; at the chroma needed to read as
a colour at all, the plane started asserting a mood the console has no business
asserting. A near-white ground makes no claim and hands the whole contrast budget
to the accents. If a future pass wants to tint it, it has to beat that argument,
not just prefer a hue.

**Ground and sheet are 1.05:1 apart** — too close to see, deliberately. A panel
is bounded by its rule, not by a fill; this is the same argument the rest of the
system makes about boxes, applied to the plane itself.

### Primary
- **Wax Seal Red** (`#cd1e33` light / `#e4737a` vault): the irreversible. It
  marks the `act-seal` control, the broken seal mark, a rejected docket entry,
  the focus ring, the caret and the text selection. It is never the primary
  button and never decorative emphasis.
- **Mark Red** (`#ef233c` light / `#e4737a` vault): the *same* seal where it is
  never read as text — today only the tamper-evident hatch band, a 14% wash in
  which the darkened seal goes muddy. Source red at full chroma, which as text
  sits at 3.1:1 on the ground and is therefore barred from the text tier.
- **Carbon Blue** (`#3a63bb` light / `#7fa8e8` vault): the action colour.
  Held 1.77:1 away from ink on purpose — preflight resets `a` to
  `text-decoration: inherit`, so a link is never underlined and colour is its
  only cue.
  Fills `act-primary`, colours links, colours the `Switch` rail when on, and is
  the hover colour for the Overview holdings list.

### Secondary
- **Endorsement Green** (`#1d7550` light / `#4fa97a` vault): standing that is
  intact — the endorsed seal mark, a committed docket entry, a completed
  ceremony step, the certificate seal when the database link holds.
- **Held Ochre** (`#b2471e` light / `#c79a3e` vault): standing that is
  suspended or incomplete — the held seal mark, the bootstrap-admin banner, an
  outstanding ceremony count.

### Neutral
- **Register Ground** (`#f9f9f9` / vault `#101311`): the desk the instruments
  lie on. Body background and the scroll region behind every page.
- **Sheet** (`#ffffff` / vault `#181c19`): the instrument's own stock. `.sheet`
  panels, ledger tables, docket entries — and, in vault colours whichever
  surface is active, the masthead and index rail.
- **Raised** (`#ededed` / vault `#1f2420`): a third ground half a step up, used
  for the `.exhibit` code well and for the active/hover state of index entries.
- **Ink** (`#3d405b` / vault `#e4e9e2`): all primary text; also `rule-strong`,
  the colour of the double rule and of every `.act` border. Indigo, not black —
  it clears AAA at 9.8:1 on the sheet, so the plane costs nothing in legibility.
- **Muted** (`#565a6e` / vault `#98a199`): secondary text — labels, hints,
  notes, placeholders, inactive index entries.
- **Faint** (`#9a9aa2` / vault `#8a938c`): **not a text colour.** Disabled
  control text, the off-state switch knob, the void seal ring, the scrollbar
  thumb on hover.
- **Rule** (`#c3c3c3` / vault `#2c332d`): every hairline. Panel borders, table
  row separators, field underlines, group dividers, scrollbar thumb. It is the
  rule, not a fill, that bounds a panel on this surface.


### Named Rules

**The Wax Rule.** Seal red belongs to what cannot be taken back and to the two
system surfaces that report it (focus ring, selection). If an element is red and
its action is reversible, the element is wrong. Ordinary commits are carbon.

**The Faint-Is-Not-Text Rule.** On the light ground no third, lighter text level
reaches 4.5:1 against sheet — so `--w-faint` is scoped to disabled controls and
non-text marks, which WCAG exempts. Placeholder text uses **muted**, because a
placeholder is body text as far as the contrast floor is concerned. Never
introduce a `text-faint` for prose; the tier does not exist by design, not by
omission.

**The Per-Theme Accent-Text Rule.** `--w-on-carbon` and `--w-on-seal` are
declared separately in each theme. The vault's accents are tints, so white laid
on them falls under 3:1 — the vault uses near-black instead (`#0c1220`,
`#1a0708`). Any new filled-accent surface must define its own on-colour per
theme; never hardcode white on an accent.

**The Two Surfaces Rule.** Instrument and Vault are both shipped products, not a
theme and a fallback. The resolved value is written to `data-theme` by an inline
script in `index.html` before first paint, so there is no flash. The control sits
at the masthead's right edge and is rendered at every width — its labels and the
`Re-read` action shrink below `sm` rather than being hidden, because a shipped
surface the visitor cannot reach is not shipped.

## Typography

**Display / Body Font:** the browser's system sans (`ui-sans-serif`, `system-ui`)
**Serial / Code Font:** the browser's system monospace (`ui-monospace`,
SFMono-Regular, Menlo, Monaco, Consolas, Liberation Mono, Courier New)

**Character:** A civic-register sans doing all the speaking, and a monospace
doing all the *swearing to* — the second face is not stylistic, it is a claim
about provenance. Hierarchy is carried almost entirely by weight and by the
engraved caps register, not by a wide size ramp: the working range between a
hint and a section heading is barely 3px.

Both roles use browser and operating-system fonts. No font files are bundled or
downloaded, which keeps the `go:embed` payload smaller and lets every supported
script use the platform's native coverage. Exact glyph metrics vary by platform,
so hierarchy is defined by role, weight and spacing rather than by one face's
metrics.

### Hierarchy
- **Display** (mono, 700, 4.5rem, lh 0.9, ls −0.035em): exactly one use — the
  live auth version on the Overview certificate.
- **Headline** (mono, 700, 2.75rem, lh 0.95, ls −0.03em): a large standalone
  serial inside a page body.
- **Title** (sans, 700, 1.6rem → 1.85rem at `sm`, lh 1.15, ls −0.02em): the
  `Instrument` masthead heading. One per page.
- **Body** (sans, 400, 13px, lh 1.55–1.65): prose and table cells. Notes and
  field values run 13.5px. Measure is capped at 70ch for prose, 62ch for switch
  hints, 104ch for the whole reading column.
- **Small** (sans, 400, 12px, lh 1.5): field hints and secondary detail.
- **Label — the stamp** (sans, 600, 10.5px, ls 0.15em, uppercase, tabular): the
  console-wide label register. Section headings, field labels, index group
  headings, seal captions, custody keys.
- **Label — literal** (sans, 500, 11px, ls 0.01em, **no** transform, tabular):
  the same register for values read literally.
- **Code** (mono, 12.5px, lh 1.65, tab-size 2): `.exhibit` wells for JSON,
  templates, PEM and JWKS.

### Named Rules

**The Serial Rule.** Anything that came from the server — an id, a version, a
key id, a path, a hostname, a count, a timestamp — is set in the system
monospace with tabular numerals via `.serial`. Prose about that value stays in
the system sans. The face change is the boundary between what the console says
and what the server swore to.

**The Case-Sensitivity Rule.** `.stamp` uppercases; `.stamp-raw` does not. A
value whose case is meaningful — an API path, a namespace identifier, a kid, a
DN, a hostname — takes `.stamp-raw`. Uppercasing a case-sensitive path turns a
fact into a lie, so `.stamp` on a server-sourced literal is always a defect.

**The Tabular Rule.** `font-variant-numeric: tabular-nums` is set on `body` and
re-asserted on `.stamp`, `.stamp-raw` and `.serial`. Figures in a register must
align down a column even when nothing draws the column.

## Layout

**Shell.** A fixed two-row grid on `h-screen` with `overflow: hidden` on `body`:
a 56px-minimum masthead, then a `[15rem 1fr]` grid below it at `lg` and up. Only
the main region and the rail's own list scroll; the page never does. The masthead
carries where the instance lives — prefix path and storage backend at `md`+ — and
its controls on the right: the surface control and `Re-read`. Standing (the
database link) and the live auth version are stated once, on the Overview
certificate, rather than repeated in the chrome of every page.

**Index rail.** 15rem fixed, sheet ground, right-ruled. Below `lg` it becomes a
17rem (max 85vw) drawer over an `ink/45` scrim, opened from the masthead or by
pressing `/`, closed on Escape or on selecting an entry. The rail is a three-part
column: a pinned search field, a scrolling list of seven collapsible groups, and
a pinned `SURFACE` footer.

**Page column.** `Instrument` wraps every page in `px-5 sm:px-8 lg:px-10` with
`pt-8 pb-24`, and constrains both header and body to `max-w-[104ch]` unless the
page opts into `wide` (used by ledger tables that need the room). The generous
bottom padding exists so the last row of a long register is not pinned to the
viewport floor.

**Rhythm.** Not a strict 4/8 grid — a documentary rhythm that repeats at four
intervals: `0.375rem` from an engraved label to its field, `0.5–0.75rem` inside
a control, `1.25rem` between the header blocks of an instrument masthead, and
`3rem` between sections. Field rows are laid out with flex wrap and `basis-*`
rather than fixed columns, so a dense form reflows to a single column on a phone
without a breakpoint per page.

### Named Rules

**The Two-Scroll Rule.** The document never scrolls. Exactly two regions do —
the index list and the main region — and both carry `overscroll-contain` so a
flick inside one never drags the other.

**The Measure Rule.** Prose is capped at 70ch and the reading column at 104ch.
A ledger may go `wide`; an explanation may not.

## Elevation & Depth

This system is flat. There is no shadow vocabulary and no elevation scale: depth
is expressed entirely through three related grounds (ground → sheet → raised),
hairline rules, and type weight. A panel is a panel because it is a lighter
ground inside a 1px rule, not because it floats.

There is exactly one shadow in the whole build — a `shadow-2xl` on the mobile
index drawer, where the drawer must read as *over* the page rather than beside
it, and where a scrim is already doing the semantic work. It is a shell
affordance, not a design token, and it should not be generalised: adding a
second shadow anywhere would make the first one look like a system.

Two printed textures substitute for depth where a surface needs to feel
security-printed:

- **Guilloche** (`.guilloche`): a two-origin repeating radial pantograph at 9%
  opacity (7% in the vault), drawn on a `z-index: -1` pseudo-element. Reserved
  for sealed regions — the Overview certificate, a freshly issued API key, a
  freshly issued recovery-code block, a completed access-check verdict.
- **Hatch** (`.hatch`): −45° repeating seal-red stripes at 14% (20% in the
  vault), mixed with `color-mix` so it tints rather than covers. Reserved for
  tamper-evident bands: the `BreakSeal` consequence line, the break-glass and
  bootstrap banners, and standing warnings about irreversible configuration.

### Named Rules

**The Flat Rule.** Surfaces are flat at rest and flat on hover. State is
expressed as a ground shift (`color-mix` of ink at 7–8%), a border-colour
change, or a 1px `translateY` on press — never as a lift.

**The Security-Print Rule.** Guilloche marks *sealed*; hatch marks *dangerous*.
Neither is decoration. A guilloche on an ordinary settings panel would devalue
the certificate, which is the only place the thesis fully lands.

## Shapes

Corners are square in practice: the radius scale tops out at 3px and the default
is 2px, small enough to read as a cut edge rather than a curve. The `.entry`
field and the `.sheet` panel use `0`. There are **no circles in the system at
all**: the seal marks are struck squares, and `border-radius: 9999px` appears
nowhere. A round status light is the one shape that would read as borrowed from a
generic dashboard rather than from this world.

The form language is **ruled, not boxed**:

- A field is a bottom rule with an engraved label above it. On focus the rule
  thickens to 2px in seal red and the padding compensates so nothing shifts. A
  disabled field's rule becomes dashed — a form line you may not write on.
- A `<select>` is the same ruled line with `appearance: none` and its own 5px
  chevron built from two linear gradients, specifically so it does not read as a
  boxed field standing beside underlined ones.
- A multi-line or code input is the exception that *keeps* its frame: an
  `.exhibit` is a full 1px rule on the raised ground, because it is an exhibit
  attached to the document rather than a line within it.
- A table is a `.sheet` with column headings sitting on a rule and each row
  closed by a rule, `last:border-b-0`. Empty and unloaded states use a dashed
  rule instead of a solid one.
- The boolean is a 34×18px square rail with a 12px square knob. Roundness is not
  available to it; a pill switch would be the only curve in the console.

### Named Rules

**The No-Box Rule.** Do not draw a border around a group to indicate that it is
a group. Use a `Section` heading on its rule, vertical space, and — only if the
content is genuinely a separate instrument — a `.sheet`.

## Components

### Buttons — `.act`
Chrome-deleted and legible as a stamp block: square, bordered in ink, transparent
until touched.
- **Shape:** effectively square (2px radius), 1px `rule-strong` border.
- **Default:** transparent ground, ink text, 13px/500, `0.375rem 0.8rem`.
- **Primary (`.act-primary`):** filled carbon indigo with the per-theme
  on-carbon text at 600. Hover darkens by mixing 14% ink into carbon.
- **Seal (`.act-seal`):** seal-red border and text at 600; hover *fills* with
  seal and flips to on-seal. Only ever appears inside a `BreakSeal`.
- **Quiet (`.act-quiet`):** transparent border, muted text, tighter inline
  padding; hover raises to ink over a 7% ink wash.
- **Hover / Active / Focus:** 8% ink wash; `translateY(1px)` on press so every
  commit in the console shares one press; a 2px seal `:focus-visible` ring at
  2px offset, inherited from the base layer.
- **Disabled:** border drops to hairline, text to faint, `cursor: not-allowed`.
  A disabled `.act-primary` empties its fill rather than greying it.

### Inputs — `.entry`
A line on a form, filled in.
- **Style:** full-width, borderless except a 1px bottom rule, transparent ground,
  13.5px, zero radius.
- **Focus:** bottom rule goes 2px seal red with compensating padding.
- **Hover:** rule darkens to faint.
- **Disabled:** faint text, dashed rule. **Placeholder:** muted, never faint.
- **Invalid:** the rule and the hint text both go seal (`Entry` sets
  `aria-invalid` and wires `aria-describedby` to the hint).
- **Mono variant:** `mono` adds `.serial` for keys, ids and paths.

### Cards / Containers — `.sheet`
- **Corner:** square (0).
- **Background:** sheet, inside a 1px hairline rule.
- **Shadow:** none, ever. See Elevation & Depth.
- **Padding:** `1.25rem` block, `1.25rem → 1.75rem` inline at `sm`.

### Navigation — `IndexRail`
The index of a dossier, not a sidebar of icons.
- **Structure:** seven groups (Control, Self service, IAM, LDAP, Federation,
  System, Platform), each heading in engraved caps with a hairline running to a
  9px chevron. Groups collapse; a search always overrides what was folded away.
- **Typography:** group headings in `.stamp`; entries in sentence case at 13px,
  because they are read as text at reading size.
- **States:** inactive is muted; hover raises to ink on the raised ground; active
  is `bg-raised` + `font-semibold` + ink text + a 6px square seal mark.
  Deliberately not a coloured side stripe and not a filled pill — the mark is the
  same one used everywhere else in the console.
- **Capability gating:** admin surfaces are **removed** from the index for
  non-admins, never rendered disabled. A self-service visitor never learns the
  admin plane exists.
- **Mobile:** drawer over a scrim; `/` opens and focuses search, Escape closes.

### `Instrument` — the page template
Twenty of the console's page components render through one template: a title,
an optional note capped at 70ch, an optional `actions` snippet aligned right, an
optional `custody` snippet stating where this page's truth lives, a closing
`.rule-double`, then the body. This is the reason a page here cannot drift into
a private layout. The Overview certificate is the one deliberate exception — see
Not Yet Resolved.

### `Serial` — the one authored motion moment
A server-sourced value that can change without the operator acting. Wrapped in
`{#key}`, so on change it remounts and plays `stamp-down` — 420ms of opacity
0→1, `scale(1.05)→1` and `blur(2.5px)→0` on the `--ease-settle`
`cubic-bezier(0.16, 1, 0.3, 1)`. Ink arriving on the sheet. A change that
happened on another instance is something you watch *land*, not something that
silently swaps. Null, undefined and empty all render as an em dash. Everything
else in the system is a 90–150ms state transition.

### `Seal` — standing
`Seal` is a mark, not a badge: a struck square — a 1px ruled outer square with an
inner filled square, default 11px, sized in px to sit on a text baseline — with
an optional `.stamp` caption. Four states: endorsed (green), broken (seal), held
(caution), void (faint rule, no fill, muted caption). It is one component used at
one size everywhere; there is no large ceremonial variant. A struck mark is
explicitly *not* a rendering of wax — a faked physical object would contradict
the typographic world on sight.

### `BreakSeal` — the signature control
The console's answer to the confirmation dialog, used on every irreversible act:
rotating the signing key, revoking a credential, deleting a principal, breaking
a temporary grant.
- A tamper-evident `.hatch` band prints the actual consequence in plain 13px
  prose — not "Are you sure?".
- The action is **inert** until the seal is broken. Sealed state shows only
  `Break seal` plus the caption "Sealed — this action is inert".
- Breaking arms it for a fixed 10s window and announces the countdown through
  `role="status"` / `aria-live="polite"`. A `Re-seal` escape sits beside it.
- A 2px seal-red rule drains across the foot, driven off the countdown state
  with a `transition-[width]`, **not** a CSS animation. This is deliberate: the
  blanket `prefers-reduced-motion` override in the stylesheet crushes all
  animation to 1ms, which would take the timing affordance away from exactly the
  people most entitled to it. The bar carries `motion-reduce:transition-none`
  and still steps once per second.
- It re-seals itself, so an armed control never sits forgotten in a background
  tab, and clears its interval on destroy.

### `Docket` — result reporting
Bottom-right, `pointer-events-none` container with `pointer-events-auto`
entries, each on the sheet inside a 1px rule and entering with `stamp-in`. A
**commit** is endorsed-green and self-clears after 3.2s. A **rejection** is
seal-red, uses `role="alert"`, and persists until dismissed — losing an error
message loses the only statement of why the write did not happen.

## Do's and Don'ts

### Do:
- **Do** wrap every new page in `Instrument` and every group inside it in
  `Section`. A page that needs a layout the template cannot give it is a signal
  to extend the template, not to hand-roll a header.
- **Do** set every server-sourced value in `.serial`, and reach for `Serial`
  itself when the value can change under the operator without them acting.
- **Do** use `.stamp-raw` for any literal whose case matters (paths, namespaces,
  ids, DNs, hostnames) and `.stamp` for labels you authored.
- **Do** put every irreversible act behind `BreakSeal`, with the consequence
  written out as a sentence a stranger could act on.
- **Do** define a per-theme on-colour for any new filled accent surface.
- **Do** separate with rules, ground shifts and space; a `Section` heading on
  its own hairline is the default grouping device.
- **Do** put element defaults in `@layer base`. Unlayered element rules outrank
  every Tailwind utility — an unlayered `button { color: inherit }` silently
  defeats every text-colour utility applied to a button, which is a defect this
  build actually shipped and fixed.
- **Do** keep webfont files out. The bundle is `go:embed`-ed whole, while the
  system stacks add no binary payload and inherit native script coverage.

### Don't:
- **Don't** use seal red for anything reversible. Not for emphasis, not for a
  primary button, not for a required-field marker.
- **Don't** introduce a third text tier on faint. `--w-faint` is for disabled
  controls and marks; placeholders and hints use muted.
- **Don't** add a modal, a `<dialog>`, or `window.confirm`. There are none in
  this codebase and the absence is load-bearing: `BreakSeal` carries consequence
  in place, where the change is being made.
- **Don't** add a box-shadow. The system is flat; the single drawer shadow is a
  scrim affordance and must not be generalised into an elevation scale.
- **Don't** round a corner past 3px. No pills, no rounded switch rails, no
  rounded cards.
- **Don't** draw a border around a group to signal grouping.
- **Don't** apply `.guilloche` outside a genuinely sealed region or `.hatch`
  outside a genuinely dangerous one.
- **Don't** disable an admin surface for a non-admin — remove it from the index.
- **Don't** animate anything except a `Serial` re-stamp and short state
  transitions. There is one authored motion moment and it is spoken for.

## Not Yet Resolved

Recorded honestly from the finish review; all accepted and carried by the
shipped build.

- **The OWNING ELEMENT principle lands only once.** The Overview certificate is
  the sole page where a single element owns the view. The ~20 record pages are
  title + one sheet + a ruled table, with no owning element — correct for a
  register, but the thesis is carried by one screen.
- **`.rule-double` is nearly dormant.** It is defined in `style.css` and used in
  exactly one place: the `Instrument` masthead close. The "double legal rules"
  half of the world's stated material is effectively a single fixture.
- **No cancellation overprint.** Revoked and held rows are distinguished only by
  the seal mark. There is no void pantograph and no overprint, so a
  cancelled record does not *read* cancelled at a glance.
- **One state is unexercised.** The `void` seal ("Draft — not yet written") face
  is unreached by the seeded instance and unphotographed in `.impeccable/review/`.
