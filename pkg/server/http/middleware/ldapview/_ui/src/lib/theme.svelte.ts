type Mode = "light" | "dark" | "system";

const KEY = "turna-lv-theme";

class Theme {
  mode = $state<Mode>((localStorage.getItem(KEY) as Mode | null) ?? "system");

  constructor() {
    const mq = matchMedia("(prefers-color-scheme: dark)");
    $effect.root(() => {
      $effect(() => {
        const apply = () => {
          const dark = this.mode === "dark" || (this.mode === "system" && mq.matches);
          document.documentElement.dataset.theme = dark ? "dark" : "light";
        };
        apply();
        if (this.mode === "system") localStorage.removeItem(KEY);
        else localStorage.setItem(KEY, this.mode);
        mq.addEventListener("change", apply);
        return () => mq.removeEventListener("change", apply);
      });
    });
  }

  cycle() {
    this.mode = this.mode === "system" ? "light" : this.mode === "light" ? "dark" : "system";
  }
}

export const theme = new Theme();
