/** One-time credentials live only in this tab's memory, never Web Storage.
 * Keeping them above page components prevents navigation from losing them. */
class IssuedSecrets {
  adminKey = $state("");
  ownKey = $state("");
  recoveryCodes = $state<string[]>([]);
  totpSecret = $state("");
  totpURL = $state("");
  #principal: string | undefined;
  #generation = 0;

  get generation() { return this.#generation; }

  get pending() {
    return Boolean(this.adminKey || this.ownKey || this.recoveryCodes.length || this.totpSecret);
  }

  bindPrincipal(principal: string) {
    if (this.#principal !== undefined && this.#principal !== principal) { this.#generation++; this.clear(); }
    this.#principal = principal;
  }

  clear() {
    this.adminKey = "";
    this.ownKey = "";
    this.recoveryCodes = [];
    this.totpSecret = "";
    this.totpURL = "";
  }
}

export const issuedSecrets = new IssuedSecrets();
