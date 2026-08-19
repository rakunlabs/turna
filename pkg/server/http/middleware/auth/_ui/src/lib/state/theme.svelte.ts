export type ThemeMode = "system" | "dark" | "light";

const STORAGE_KEY = "turna-auth-theme";

function isThemeMode(value: string | null): value is ThemeMode {
  return value === "system" || value === "dark" || value === "light";
}

/**
 * Two shipped surfaces, not one theme with a fallback: Instrument (the document
 * under office light) and Vault (the same instruments in the archive). The
 * resolved value is written to `data-theme` before first paint by an inline
 * script in index.html, so this only has to keep it in sync afterwards.
 */
class Theme {
  mode = $state<ThemeMode>("system");

  resolved(mode: ThemeMode = this.mode) {
    if (mode !== "system") return mode;
    return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
  }

  #apply(mode: ThemeMode) {
    document.documentElement.dataset.themeMode = mode;
    document.documentElement.dataset.theme = this.resolved(mode);
  }

  set(mode: ThemeMode) {
    this.mode = mode;
    try {
      localStorage.setItem(STORAGE_KEY, mode);
    } catch {
      // private mode: the choice simply does not persist
    }
    this.#apply(mode);
  }

  /** Returns a teardown for the system-preference listener. */
  init() {
    let stored: string | null = null;
    try {
      stored = localStorage.getItem(STORAGE_KEY);
    } catch {
      stored = null;
    }

    this.mode = isThemeMode(stored) ? stored : "system";
    this.#apply(this.mode);

    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const onChange = () => {
      if (this.mode === "system") this.#apply("system");
    };

    media.addEventListener("change", onChange);
    return () => media.removeEventListener("change", onChange);
  }
}

export const theme = new Theme();
