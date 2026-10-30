import { EditorState, Compartment, Transaction, type Extension, type Text } from "@codemirror/state";
import {
  EditorView,
  keymap,
  lineNumbers,
  highlightActiveLine,
  highlightActiveLineGutter,
  drawSelection,
  highlightSpecialChars,
  rectangularSelection,
  crosshairCursor,
} from "@codemirror/view";
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands";
import {
  HighlightStyle,
  syntaxHighlighting,
  indentOnInput,
  bracketMatching,
  foldGutter,
  foldKeymap,
} from "@codemirror/language";
import { searchKeymap, highlightSelectionMatches } from "@codemirror/search";
import { linter, lintGutter, lintKeymap } from "@codemirror/lint";
import { tags as t } from "@lezer/highlight";
import { diagnose, type Problem } from "./lint";

export { languageLabels, detectLanguage, loadLanguage } from "./languages";
import { loadLanguage } from "./languages";

// Vesper, the theme the turna docs use for code blocks. Code stays dark in both page themes.
const highlight = HighlightStyle.define([
  { tag: [t.keyword, t.operatorKeyword, t.modifier, t.controlKeyword, t.definitionKeyword, t.moduleKeyword], color: "var(--code-keyword)" },
  { tag: [t.self, t.special(t.variableName)], color: "var(--code-keyword)" },
  { tag: [t.string, t.special(t.string), t.character, t.attributeValue], color: "var(--code-string)" },
  { tag: [t.number, t.bool, t.null, t.atom, t.unit, t.constant(t.variableName)], color: "var(--code-accent)" },
  { tag: [t.function(t.variableName), t.function(t.propertyName), t.macroName], color: "var(--code-accent)" },
  { tag: [t.typeName, t.className, t.namespace, t.tagName, t.labelName], color: "var(--code-accent)" },
  { tag: [t.propertyName, t.definition(t.propertyName)], color: "var(--code-accent)" },
  { tag: [t.attributeName], color: "var(--code-keyword)" },
  { tag: [t.variableName, t.definition(t.variableName)], color: "var(--code-text)" },
  { tag: [t.regexp, t.escape], color: "var(--code-keyword)" },
  { tag: [t.punctuation, t.bracket, t.separator, t.operator, t.angleBracket], color: "var(--code-keyword)" },
  { tag: [t.comment, t.lineComment, t.blockComment, t.docComment], color: "var(--code-comment)", fontStyle: "italic" },
  { tag: [t.meta, t.processingInstruction, t.documentMeta], color: "var(--code-comment)" },
  { tag: t.heading, color: "var(--code-accent)", fontWeight: "600" },
  { tag: t.strong, fontWeight: "700" },
  { tag: t.emphasis, fontStyle: "italic" },
  { tag: t.strikethrough, textDecoration: "line-through" },
  { tag: [t.link, t.url], color: "var(--code-string)", textDecoration: "underline" },
  { tag: t.monospace, color: "var(--code-keyword)" },
  { tag: t.inserted, color: "var(--code-string)" },
  { tag: t.deleted, color: "var(--code-error)" },
  { tag: t.changed, color: "var(--code-keyword)" },
  { tag: t.invalid, color: "var(--code-error)" },
]);

const theme = EditorView.theme(
  {
    "&": {
      height: "100%",
      fontSize: "13px",
      backgroundColor: "var(--code-bg)",
      color: "var(--code-text)",
    },
    "&.cm-focused": { outline: "none" },
    ".cm-scroller": {
      fontFamily: "var(--font-mono)",
      lineHeight: "1.65",
    },
    ".cm-content": { padding: "12px 0", caretColor: "var(--code-caret)" },
    ".cm-line": { padding: "0 16px 0 12px" },
    ".cm-gutters": {
      backgroundColor: "var(--code-bg)",
      color: "var(--code-line-number)",
      border: "none",
      borderRight: "1px solid var(--code-divider)",
    },
    ".cm-lineNumbers .cm-gutterElement": { padding: "0 10px 0 16px", minWidth: "44px" },
    ".cm-activeLine": { backgroundColor: "var(--code-line-highlight)" },
    ".cm-activeLineGutter": { backgroundColor: "var(--code-line-highlight)", color: "var(--code-keyword)" },
    ".cm-cursor, .cm-dropCursor": { borderLeftColor: "var(--code-caret)", borderLeftWidth: "2px" },
    "&.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection":
      { backgroundColor: "var(--code-selection) !important" },
    ".cm-selectionMatch": { backgroundColor: "var(--code-match)" },
    ".cm-matchingBracket, &.cm-focused .cm-matchingBracket": {
      backgroundColor: "var(--code-match)",
      color: "var(--code-text)",
      outline: "1px solid var(--code-divider-strong)",
    },
    ".cm-nonmatchingBracket": { color: "var(--code-error)" },
    ".cm-foldGutter .cm-gutterElement": { padding: "0 4px", color: "var(--code-line-number)" },
    ".cm-foldPlaceholder": {
      backgroundColor: "var(--code-raised)",
      border: "1px solid var(--code-divider-strong)",
      color: "var(--code-keyword)",
    },
    ".cm-panels": { backgroundColor: "var(--code-raised)", color: "var(--code-text)" },
    ".cm-panels.cm-panels-top": { borderBottom: "1px solid var(--code-divider)" },
    ".cm-panels.cm-panels-bottom": { borderTop: "1px solid var(--code-divider)" },
    ".cm-search": { fontFamily: "var(--font-sans)", fontSize: "12px", padding: "6px 10px" },
    ".cm-search label": { color: "var(--code-keyword)" },
    ".cm-search input, .cm-search button": { fontFamily: "inherit", fontSize: "12px" },
    ".cm-textfield": {
      backgroundColor: "var(--code-bg)",
      color: "var(--code-text)",
      border: "1px solid var(--code-divider-strong)",
      borderRadius: "6px",
      padding: "3px 6px",
    },
    ".cm-button": {
      backgroundImage: "none",
      backgroundColor: "var(--code-raised)",
      color: "var(--code-text)",
      border: "1px solid var(--code-divider-strong)",
      borderRadius: "6px",
      padding: "3px 8px",
    },
    ".cm-panel.cm-search [name=close]": { color: "var(--code-keyword)" },
    ".cm-searchMatch": { backgroundColor: "var(--code-match)", outline: "1px solid var(--code-divider-strong)" },
    ".cm-searchMatch-selected": { backgroundColor: "var(--code-selection)" },
    ".cm-tooltip": {
      backgroundColor: "var(--code-raised)",
      color: "var(--code-text)",
      border: "1px solid var(--code-divider-strong)",
      borderRadius: "6px",
    },
    ".cm-specialChar": { color: "var(--code-error)" },
    ".cm-gutter-lint": { width: "14px" },
    ".cm-gutter-lint .cm-gutterElement": { padding: "0 0 0 4px" },
    ".cm-lint-marker": { width: "8px", height: "8px", marginTop: "7px", borderRadius: "50%", content: "none" },
    ".cm-lint-marker-error": { content: "none", backgroundColor: "var(--code-error)" },
    ".cm-lint-marker-warning": { content: "none", backgroundColor: "var(--code-accent)" },
    ".cm-lintRange-error": {
      backgroundImage: "none",
      textDecoration: "underline wavy var(--code-error)",
      textDecorationSkipInk: "none",
      textUnderlineOffset: "3px",
    },
    ".cm-lintRange-warning": {
      backgroundImage: "none",
      textDecoration: "underline wavy var(--code-accent)",
      textDecorationSkipInk: "none",
      textUnderlineOffset: "3px",
    },
    ".cm-lintPoint-error:after": { borderBottomColor: "var(--code-error)" },
    ".cm-lintPoint-warning:after": { borderBottomColor: "var(--code-accent)" },
    ".cm-tooltip-lint": { padding: "2px 0", maxWidth: "520px" },
    ".cm-diagnostic": { fontFamily: "var(--font-sans)", fontSize: "12px", padding: "4px 10px", borderLeftWidth: "3px" },
    ".cm-diagnostic-error": { borderLeftColor: "var(--code-error)" },
    ".cm-diagnostic-warning": { borderLeftColor: "var(--code-accent)" },
    ".cm-panel.cm-panel-lint ul [aria-selected]": { backgroundColor: "var(--code-selection)" },
  },
  { dark: true },
);

export type EditorHandle = {
  view: EditorView;
  /** Opens a new document: resets undo history, selection and scroll. */
  setDoc(text: string, lang: string): Promise<void>;
  /** Replaces the text in place, keeping undo history; the cursor stays as close as it can. */
  replaceText(text: string): void;
  setLanguage(lang: string): Promise<void>;
  /** Language whose parser checks the text; null turns linting off. */
  setLint(lang: string | null): void;
  setReadOnly(readOnly: boolean): void;
  setWrap(wrap: boolean): void;
  getText(): string;
  /** Remembers the current text as saved; isDirty compares against it. */
  markClean(): void;
  /** Remembers doc, an earlier snapshot, as saved; edits made since stay dirty. */
  markCleanAt(doc: Text): void;
  isDirty(): boolean;
  destroy(): void;
};

export function createEditor(
  parent: HTMLElement,
  opts: {
    /** Called on every edit; read the text lazily with getText, it can be megabytes. */
    onChange: () => void;
    onSave: () => void;
    onFormat: () => void;
    onCursor: (line: number, col: number) => void;
    onProblems: (problems: Problem[]) => void;
  },
): EditorHandle {
  const language = new Compartment();
  const readOnly = new Compartment();
  const wrapping = new Compartment();
  const linting = new Compartment();
  let wrap = false;
  let ro = false;
  let lintLang: string | null = null;
  let langToken = 0;

  const lintExt = (lang: string | null): Extension =>
    lang
      ? [
          lintGutter(),
          linter(
            async (view) => {
              const doc = view.state.doc;
              const problems = await diagnose(lang, doc.toString(), doc);
              if (view.state.doc === doc) opts.onProblems(problems);
              return problems;
            },
            { delay: 300 },
          ),
        ]
      : [];

  const base = (): Extension[] => [
    lineNumbers(),
    foldGutter({ openText: "▾", closedText: "▸" }),
    highlightActiveLineGutter(),
    highlightSpecialChars(),
    history(),
    drawSelection(),
    EditorState.allowMultipleSelections.of(true),
    indentOnInput(),
    bracketMatching(),
    rectangularSelection(),
    crosshairCursor(),
    highlightActiveLine(),
    highlightSelectionMatches(),
    syntaxHighlighting(highlight),
    keymap.of([
      { key: "Mod-s", preventDefault: true, run: () => (opts.onSave(), true) },
      { key: "Shift-Alt-f", preventDefault: true, run: () => (opts.onFormat(), true) },
      indentWithTab,
      ...defaultKeymap,
      ...historyKeymap,
      ...foldKeymap,
      ...searchKeymap,
      ...lintKeymap,
    ]),
    theme,
    language.of([]),
    readOnly.of(ro ? [EditorState.readOnly.of(true)] : []),
    wrapping.of(wrap ? EditorView.lineWrapping : []),
    linting.of(lintExt(lintLang)),
    EditorView.updateListener.of((u) => {
      if (u.docChanged) opts.onChange();
      if (u.selectionSet || u.docChanged) {
        const head = u.state.selection.main.head;
        const line = u.state.doc.lineAt(head);
        opts.onCursor(line.number, head - line.from + 1);
      }
    }),
  ];

  const view = new EditorView({ parent, state: EditorState.create({ doc: "", extensions: base() }) });
  let clean: Text = view.state.doc;

  async function setLanguage(lang: string) {
    const my = ++langToken;
    const ext = await loadLanguage(lang);
    if (my !== langToken) return;
    view.dispatch({ effects: language.reconfigure(ext) });
  }

  return {
    view,
    async setDoc(text, lang) {
      const my = ++langToken;
      const ext = await loadLanguage(lang);
      if (my !== langToken) return;
      view.setState(EditorState.create({ doc: text, extensions: base() }));
      view.dispatch({ effects: language.reconfigure(ext) });
      clean = view.state.doc;
      opts.onCursor(1, 1);
    },
    replaceText(text) {
      const state = view.state;
      const old = state.doc.toString();
      if (old === text) return;
      // touch only the changed middle so the cursor, scroll and folds stay put
      let start = 0;
      const max = Math.min(old.length, text.length);
      while (start < max && old.charCodeAt(start) === text.charCodeAt(start)) start++;
      let endOld = old.length;
      let endNew = text.length;
      while (endOld > start && endNew > start && old.charCodeAt(endOld - 1) === text.charCodeAt(endNew - 1)) {
        endOld--;
        endNew--;
      }
      view.dispatch({
        changes: { from: start, to: endOld, insert: text.slice(start, endNew) },
        annotations: Transaction.userEvent.of("input.format"),
        scrollIntoView: false,
      });
    },
    setLanguage,
    setLint(lang) {
      if (lang === lintLang) return;
      lintLang = lang;
      view.dispatch({ effects: linting.reconfigure(lintExt(lang)) });
      if (!lang) opts.onProblems([]);
    },
    setReadOnly(value) {
      ro = value;
      view.dispatch({ effects: readOnly.reconfigure(value ? [EditorState.readOnly.of(true)] : []) });
    },
    setWrap(value) {
      wrap = value;
      view.dispatch({ effects: wrapping.reconfigure(value ? EditorView.lineWrapping : []) });
    },
    getText: () => view.state.doc.toString(),
    markClean() {
      clean = view.state.doc;
    },
    markCleanAt(doc) {
      clean = doc;
    },
    isDirty() {
      const doc = view.state.doc;
      return doc !== clean && (doc.length !== clean.length || !doc.eq(clean));
    },
    destroy: () => view.destroy(),
  };
}
