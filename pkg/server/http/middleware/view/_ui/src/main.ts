import "@/style.css";
import "github-markdown-css/github-markdown.css";
import "lineicons/dist/lineicons.css";
import { mount } from "svelte";
import App from "@/App.svelte";

export default mount(App, { target: document.getElementById("app")! });
