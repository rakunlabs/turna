export type ApiResponse<T> = {
  payload: T;
  meta?: {
    total_item_count?: number;
    version?: number;
  };
  message?: { text?: string; error?: string };
};

export type Row = {
  id: string;
  sub: string;
  enabled: boolean;
  updated: string;
};

export type AnyRecord = Record<string, unknown>;

export type InfoPayload = {
  prefix_path: string;
  version: number;
  storage: string;
};

export type Dashboard = {
  total_roles: number;
  total_permissions: number;
  total_users: number;
  total_service_accounts: number;
};

export type ResourceKind =
  | "settings"
  | "clients"
  | "providers"
  | "saml"
  | "ldap"
  | "users"
  | "service-accounts"
  | "roles"
  | "permissions"
  | "lmaps";

export type KindSpec = {
  title: string;
  description: string;
  cta: string;
  primaryLabel: string;
  secondaryLabel: string;
  listPath: string;
  idField: string;
  // settings wrap {value}, config wraps {enabled, config}, iam sends the raw JSON document
  body: "value" | "config" | "raw";
  canCreate: boolean;
  example: unknown;
  // per-namespace JSON templates (settings only)
  namespaceExamples?: Record<string, unknown>;
};

export const settingTemplates = {
  token: {
    token_lifetime: "15m",
    refresh_lifetime: "24h",
  },
  admin: {
    permission: "",
    allow_missing_x_user: true,
  },
  oauth2: {
    base_url: "",
    schema: "https",
    insecure_skip_verify: false,
  },
  check: {
    default_hosts: [],
    no_host_check: false,
  },
  password: {
    disabled: false,
    local_disabled: false,
    ldap_disabled: false,
    ldap_register_disabled: false,
  },
  passkey: {
    disabled: false,
    rp_id: "",
    rp_display_name: "",
    origins: [],
    user_verification: "preferred",
  },
  jwt: {
    kid: "",
    private_key: "",
  },
  cache: {
    poll_interval: "5s",
    code_store: {
      active: "memory",
      redis: {
        address: ["127.0.0.1:6379"],
        username: "",
        password: "",
        client_name: "turna-auth",
        tls: {
          enabled: false,
          cert_file: "",
          key_file: "",
          ca_file: "",
        },
      },
    },
  },
  api_key: {
    disabled: false,
    max_lifetime: "",
  },
  device: {
    disabled: false,
    code_lifetime: "10m",
    interval: 5,
    verification_uri: "",
  },
  token_exchange: {
    disabled: false,
  },
  totp: {
    disabled: false,
    issuer: "Turna Auth",
    skew: 1,
  },
  signup: {
    enabled: false,
    email_verification: true,
    password_reset: false,
    default_role_ids: [],
    password_min_length: 8,
    code_lifetime: "1h",
    verify_subject: "",
    verify_body_template: "",
    reset_subject: "",
    reset_body_template: "",
  },
  email: {
    disabled: false,
    magic_link: true,
    from: "",
    subject: "",
    body_template: "",
    magic_link_subject: "",
    magic_link_body_template: "",
    code_lifetime: "15m",
    smtp: {
      host: "",
      port: 587,
      username: "",
      password: "",
      no_auth: false,
      starttls: true,
      tls: false,
      insecure_skip_verify: false,
    },
  },
  mtls: {
    enabled: false,
    cert_header: "",
  },
} as const;

// settings namespaces listed and editable on the Settings tab
export const editableSettingNamespaces = [
  "admin",
  "cache",
  "device",
  "token_exchange",
  "totp",
] as const;

export type SettingNamespace = keyof typeof settingTemplates;

export const kindSpecs: Record<ResourceKind, KindSpec> = {
  settings: {
    title: "Runtime Settings",
    description:
      "Encrypted system namespaces. OAuth2, access check, API key, email and mTLS settings live on their own pages.",
    cta: "Reserved setting",
    primaryLabel: "Namespace",
    secondaryLabel: "Updated by",
    listPath: "settings",
    idField: "namespace",
    body: "value",
    canCreate: false,
    example: settingTemplates.cache,
    namespaceExamples: {
      admin: settingTemplates.admin,
      cache: settingTemplates.cache,
      device: settingTemplates.device,
      token_exchange: settingTemplates.token_exchange,
      totp: settingTemplates.totp,
    },
  },
  clients: {
    title: "OAuth Server Clients",
    description:
      "OAuth2 server-side clients accepted by the token endpoint (password / authorization_code / refresh grants).",
    cta: "New server client",
    primaryLabel: "Client ID",
    secondaryLabel: "Updated by",
    listPath: "oauth/clients",
    idField: "id",
    body: "config",
    canCreate: true,
    example: {
      client_secret: "change-me",
      scope: ["openid", "profile"],
      whitelist_urls: ["https://app.example.com/callback"],
    },
  },
  providers: {
    title: "OAuth Providers",
    description:
      "Encrypted upstream identity providers used by /oauth2/auth/{provider} and /oauth2/code/{provider}.",
    cta: "New provider",
    primaryLabel: "Provider ID",
    secondaryLabel: "Updated by",
    listPath: "oauth/providers",
    idField: "id",
    body: "config",
    canCreate: true,
    example: {
      client_id: "turna",
      client_secret: "change-me",
      scopes: ["openid", "profile", "email"],
      auth_url: "https://idp.example.com/auth",
      token_url: "https://idp.example.com/token",
      cert_url: "https://idp.example.com/certs",
      claim_mapping: {
        roles_claim: "",
        use_lmap: false,
        role_map: {},
        register: false,
      },
    },
  },
  saml: {
    title: "SAML Providers",
    description:
      "SAML 2.0 identity providers. SP metadata lives at /saml/{id}/metadata; logins start at /saml/{id}/login and end with a standard authorization code.",
    cta: "New SAML provider",
    primaryLabel: "Provider ID",
    secondaryLabel: "Updated by",
    listPath: "saml/providers",
    idField: "id",
    body: "config",
    canCreate: true,
    example: {
      metadata_url: "https://idp.example.com/metadata",
      metadata_xml: "",
      entity_id: "",
      alias_attribute: "",
      sign_requests: false,
      claim_mapping: {
        roles_claim: "",
        use_lmap: false,
        role_map: {},
        register: false,
      },
    },
  },
  ldap: {
    title: "LDAP Configs",
    description:
      "Encrypted LDAP connection used for password checks and group sync. The first enabled config is active.",
    cta: "New LDAP config",
    primaryLabel: "Config ID",
    secondaryLabel: "Updated by",
    listPath: "ldap/configs",
    idField: "id",
    body: "config",
    canCreate: true,
    example: {
      addr: "ldap://ldap.example.com:389",
      bind: { username: "cn=readonly,dc=example,dc=com", password: "change-me" },
      user_base_dn: "ou=people,dc=example,dc=com",
      groups: [
        {
          base_dn: "ou=groups,dc=example,dc=com",
          filter: "(objectClass=groupOfUniqueNames)",
          attributes: ["cn", "uniqueMember", "description"],
        },
      ],
      sync_duration: "10m",
      disable_sync: false,
    },
  },
  users: {
    title: "Users",
    description: "IAM users stored in PostgreSQL. Details are encrypted at rest; passwords are bcrypt hashed.",
    cta: "New user",
    primaryLabel: "User ID",
    secondaryLabel: "Aliases",
    listPath: "users",
    idField: "id",
    body: "raw",
    canCreate: true,
    example: {
      id: "generated-on-create",
      alias: ["user@example.com", "user"],
      details: { name: "User Name", email: "user@example.com", uid: "user", password: "change-me" },
      role_ids: [],
      sync_role_ids: [],
      permission_ids: [],
      tmp_role_ids: [],
      tmp_permission_ids: [],
      local: true,
      is_active: true,
    },
  },
  "service-accounts": {
    title: "Service Accounts",
    description: "Machine identities for client_credentials. The `name` detail identifies the account; `secret` acts as the client secret.",
    cta: "New service account",
    primaryLabel: "Account ID",
    secondaryLabel: "Aliases",
    listPath: "service-accounts",
    idField: "id",
    body: "raw",
    canCreate: true,
    example: {
      id: "generated-on-create",
      alias: ["my-service"],
      details: { name: "my-service", secret: "change-me", scope: "openid" },
      role_ids: [],
      sync_role_ids: [],
      permission_ids: [],
      tmp_role_ids: [],
      tmp_permission_ids: [],
      is_active: true,
    },
  },
  roles: {
    title: "Roles",
    description: "Roles bundle permissions and can contain other roles (virtual roles).",
    cta: "New role",
    primaryLabel: "Role ID",
    secondaryLabel: "Name",
    listPath: "roles",
    idField: "id",
    body: "raw",
    canCreate: true,
    example: {
      name: "my-role",
      description: "",
      permission_ids: [],
      role_ids: [],
      data: {},
    },
  },
  permissions: {
    title: "Permissions",
    description: "Permissions match host/path/method resources, optionally carrying data and scope-role mappings.",
    cta: "New permission",
    primaryLabel: "Permission ID",
    secondaryLabel: "Name",
    listPath: "permissions",
    idField: "id",
    body: "raw",
    canCreate: true,
    example: {
      name: "my-permission",
      description: "",
      resources: [
        {
          hosts: [],
          paths: ["/api/**"],
          methods: ["GET", "POST"],
        },
      ],
      data: {},
      scope: {},
    },
  },
  lmaps: {
    title: "LDAP Maps",
    description:
      "Maps LDAP group names to role IDs. Sync auto-creates a role named after each LDAP group and links it here; group members receive the mapped roles as sync roles.",
    cta: "New LDAP map",
    primaryLabel: "LDAP Group",
    secondaryLabel: "Role IDs",
    listPath: "lmaps",
    idField: "name",
    body: "raw",
    canCreate: true,
    example: {
      name: "ldap-group",
      role_ids: [],
    },
  },
};

// permissionPresets returns ready-to-edit permission documents whose resources
// are scoped to the live auth prefix (derived from the API base). auth_admin
// grants full management access; auth_user grants only self-service surfaces.
export function permissionPresets(prefix: string): Record<string, AnyRecord> {
  const p = prefix.replace(/\/+$/, "");

  return {
    auth_admin: {
      name: "auth_admin",
      description: "Full administrative access to the Turna auth API and UI.",
      resources: [
        { hosts: [], paths: [`${p}/v1/**`], methods: ["GET", "POST", "PUT", "PATCH", "DELETE"] },
        { hosts: [], paths: [`${p}/ui/**`, `${p}/swagger/**`], methods: ["GET"] },
      ],
      data: {},
      scope: {},
    },
    auth_user: {
      name: "auth_user",
      description: "Self-service account access: profile, password, passkey, TOTP and device approval.",
      resources: [
        { hosts: [], paths: [`${p}/v1/me`, `${p}/v1/me/password`], methods: ["GET", "POST"] },
        { hosts: [], paths: [`${p}/v1/passkey/**`, `${p}/v1/totp`, `${p}/v1/totp/**`, `${p}/v1/device`, `${p}/v1/device/**`], methods: ["GET", "POST", "DELETE"] },
        { hosts: [], paths: [`${p}/info`, `${p}/ui/**`, `${p}/oauth2/**`], methods: ["GET", "POST"] },
      ],
      data: {},
      scope: {},
    },
  };
}

export function rowFromItem(kind: ResourceKind, item: Record<string, unknown>): Row {
  switch (kind) {
    case "settings":
      return {
        id: String(item.namespace ?? ""),
        sub: `by ${item.updated_by ?? "unknown"}`,
        enabled: true,
        updated: String(item.updated_at ?? ""),
      };
    case "clients":
    case "providers":
    case "saml":
    case "ldap":
      return {
        id: String(item.id ?? ""),
        sub: `by ${item.updated_by ?? "unknown"}`,
        enabled: Boolean(item.enabled),
        updated: String(item.updated_at ?? ""),
      };
    case "users":
    case "service-accounts": {
      const alias = Array.isArray(item.alias) ? (item.alias as string[]).join(", ") : "";
      return {
        id: String(item.id ?? ""),
        sub: alias,
        enabled: Boolean(item.is_active),
        updated: String(item.updated_at ?? ""),
      };
    }
    case "roles":
    case "permissions":
      return {
        id: String(item.id ?? ""),
        sub: String(item.name ?? ""),
        enabled: true,
        updated: String(item.updated_at ?? ""),
      };
    case "lmaps":
      return {
        id: String(item.name ?? ""),
        sub: Array.isArray(item.role_ids) ? (item.role_ids as string[]).join(", ") : "",
        enabled: true,
        updated: String(item.updated_at ?? ""),
      };
  }
}
