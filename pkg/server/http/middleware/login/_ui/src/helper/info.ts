type AuthInfo = {
  title: string;
  provider: Provider;
  error?: string;
};

type Provider = {
  password: Link[];
  code: Link[];
  passkey?: Link[];
}

type Link = {
  name: string;
  url: string;
  signup_url?: string;
  signup_verify_url?: string;
  password_reset_url?: string;
  password_reset_confirm_url?: string;
  password_min_length?: number;
}

export type { AuthInfo, Provider, Link };
