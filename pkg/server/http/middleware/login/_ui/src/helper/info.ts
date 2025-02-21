type AuthInfo = {
  title: string;
  provider: Provider;
  error?: string;
};

type Provider = {
  password: Link[];
  code: Link[];
}

type Link = {
  name: string;
  url: string;
}

export type { AuthInfo, Provider, Link };
