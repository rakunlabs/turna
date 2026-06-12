module.exports = {
  content: ["./index.html", "./src/**/*.{svelte,ts}"],
  theme: {
    extend: {
      colors: {
        crt: "rgb(var(--color-crt) / <alpha-value>)",
        panel: "rgb(var(--color-panel) / <alpha-value>)",
        line: "rgb(var(--color-line) / <alpha-value>)",
        fg: "rgb(var(--color-fg) / <alpha-value>)",
        dim: "rgb(var(--color-dim) / <alpha-value>)",
        alert: "rgb(var(--color-alert) / <alpha-value>)",
        phosphor: "rgb(var(--color-phosphor) / <alpha-value>)",
      },
      fontFamily: {
        mono: [
          '"JetBrains Mono"',
          "ui-monospace",
          "SFMono-Regular",
          "Menlo",
          "Consolas",
          '"Courier New"',
          "monospace",
        ],
        display: ['"Archivo Black"', "Inter", '"Helvetica Neue"', "Arial", "sans-serif"],
      },
    },
  },
  plugins: [],
};
