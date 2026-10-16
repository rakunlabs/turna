type PromptOpts = {
  title: string;
  description?: string;
  label?: string;
  value?: string;
  placeholder?: string;
  confirm: string;
  danger?: boolean;
  /** Select this range of the value on open, default: whole value. */
  select?: [number, number];
  /** Return an error message to block submit. */
  validate?: (v: string) => string | null;
};

type Pending = PromptOpts & { input: boolean; resolve: (v: string | null) => void };

class DialogState {
  current = $state<Pending | null>(null);

  prompt(opts: PromptOpts): Promise<string | null> {
    return new Promise((resolve) => {
      this.current?.resolve(null);
      this.current = { ...opts, input: true, resolve };
    });
  }

  async confirm(opts: Omit<PromptOpts, "label" | "value" | "placeholder" | "select" | "validate">): Promise<boolean> {
    return new Promise((resolve) => {
      this.current?.resolve(null);
      this.current = { ...opts, input: false, resolve: (v) => resolve(v !== null) };
    });
  }

  close(value: string | null) {
    const c = this.current;
    this.current = null;
    c?.resolve(value);
  }
}

export const dialog = new DialogState();
