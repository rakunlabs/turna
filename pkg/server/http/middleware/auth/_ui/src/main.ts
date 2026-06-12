import "@fontsource/jetbrains-mono/latin-400.css";
import "@fontsource/jetbrains-mono/latin-ext-400.css";
import "@fontsource/jetbrains-mono/latin-500.css";
import "@fontsource/jetbrains-mono/latin-700.css";
import "@fontsource/jetbrains-mono/latin-ext-700.css";
import "@fontsource/archivo-black/latin-400.css";
import "@fontsource/archivo-black/latin-ext-400.css";
import "./style.css";
import App from "./App.svelte";

const app = new App({
  target: document.getElementById("app")!,
});

export default app;
