module.exports = {
  content: ["./index.html", "./src/**/*.{svelte,ts}"],
  theme: {
    extend: {
      colors: {
        // Meridian Capital Portfolio palette.
        // Legacy token names (crt/panel/line/fg/dim/alert/phosphor) are kept so the
        // existing components re-skin without edits; the CSS variables behind them
        // now carry the Meridian colors.
        crt: "rgb(var(--color-crt) / <alpha-value>)", // background
        surface: "rgb(var(--color-surface) / <alpha-value>)", // surface
        panel: "rgb(var(--color-panel) / <alpha-value>)", // panel
        "panel-hover": "rgb(var(--color-panel-hover) / <alpha-value>)",
        line: "rgb(var(--color-line) / <alpha-value>)", // border
        "line-subtle": "rgb(var(--color-line-subtle) / <alpha-value>)", // border-subtle
        fg: "rgb(var(--color-fg) / <alpha-value>)", // text-bright
        dim: "rgb(var(--color-dim) / <alpha-value>)", // text-sub
        // semantic accents
        primary: "rgb(var(--color-primary) / <alpha-value>)",
        success: "rgb(var(--color-phosphor) / <alpha-value>)",
        phosphor: "rgb(var(--color-phosphor) / <alpha-value>)", // alias of success
        warning: "rgb(var(--color-warning) / <alpha-value>)",
        error: "rgb(var(--color-alert) / <alpha-value>)",
        alert: "rgb(var(--color-alert) / <alpha-value>)", // alias of error
      },
      fontFamily: {
        sans: ['"IBM Plex Sans"', "Inter", "system-ui", '"Helvetica Neue"', "Arial", "sans-serif"],
        mono: [
          '"IBM Plex Mono"',
          "ui-monospace",
          "SFMono-Regular",
          "Menlo",
          "Consolas",
          '"Courier New"',
          "monospace",
        ],
        // headings share the sans face; weight carries the hierarchy
        display: ['"IBM Plex Sans"', "Inter", "system-ui", '"Helvetica Neue"', "Arial", "sans-serif"],
      },
      borderRadius: {
        none: "0",
        sm: "6px",
        DEFAULT: "8px",
        md: "8px",
        lg: "10px",
        xl: "12px",
        full: "9999px",
      },
      boxShadow: {
        sm: "0 1px 2px 0 rgb(0 0 0 / 0.08)",
        DEFAULT: "0 1px 3px 0 rgb(0 0 0 / 0.10), 0 1px 2px -1px rgb(0 0 0 / 0.10)",
        md: "0 4px 10px -2px rgb(0 0 0 / 0.12), 0 2px 4px -2px rgb(0 0 0 / 0.08)",
      },
    },
  },
  plugins: [],
};
