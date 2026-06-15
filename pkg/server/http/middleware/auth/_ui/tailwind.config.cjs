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
        sans: ['"IBM Plex Sans"', "Inter", '"Helvetica Neue"', "Arial", "sans-serif"],
        mono: [
          '"IBM Plex Mono"',
          "ui-monospace",
          "SFMono-Regular",
          "Menlo",
          "Consolas",
          '"Courier New"',
          "monospace",
        ],
        // headings remain on the IBM Plex superfamily (weight carries the hierarchy)
        display: ['"IBM Plex Sans"', "Inter", '"Helvetica Neue"', "Arial", "sans-serif"],
      },
      borderRadius: {
        none: "0",
        sm: "2px",
        DEFAULT: "3px",
        md: "3px",
        lg: "3px",
        full: "9999px",
      },
    },
  },
  plugins: [],
};
