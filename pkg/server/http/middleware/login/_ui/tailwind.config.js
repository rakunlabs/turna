module.exports = {
  mode: "jit",
  content: ["./index.html", "./src/**/*.{svelte,js,ts,jsx,tsx}"],
  darkMode: "class", // or 'media' or 'class'
  theme: {
    extend: {
      colors: {
        night: {
          canvas: "#252422",
          surface: "#403D39",
          hover: "#514D47",
          border: "#918B80",
          divider: "#625D55",
          text: "#FFFCF2",
          muted: "#C5C1B7",
          accent: "#EF233C",
          "accent-hover": "#F3475C",
          "on-accent": "#FFFFFF",
          sage: "#81B29A",
          success: "#2B3A32",
          error: "#492C2D",
          "error-text": "#FFADB5",
        },
      },
    },
  },
  variants: {
    extend: {},
  },
  plugins: [],
};
