// Semantic color tokens. Every value is a space-separated RGB triplet so
// Tailwind opacity modifiers (`bg-brand/10`) keep working.
const token = (name) => `rgb(var(--${name}) / <alpha-value>)`;

module.exports = {
  mode: "jit",
  content: ["./index.html", "./src/**/*.{svelte,js,ts,jsx,tsx}"],
  darkMode: "class", // or 'media' or 'class'
  theme: {
    extend: {
      colors: {
        canvas: token("canvas"),
        surface: token("surface"),
        field: token("field"),
        hover: token("hover"),
        line: token("line"),
        divider: token("divider"),
        ink: token("ink"),
        muted: token("muted"),
        brand: {
          DEFAULT: token("brand"),
          hover: token("brand-hover"),
          on: token("brand-on"),
          link: token("brand-link"),
        },
        success: { DEFAULT: token("success"), bg: token("success-bg"), line: token("success-line") },
        danger: { DEFAULT: token("danger"), bg: token("danger-bg"), line: token("danger-line") },
      },
    },
  },
  variants: {
    extend: {},
  },
  plugins: [],
};
