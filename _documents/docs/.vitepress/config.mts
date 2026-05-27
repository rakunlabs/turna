import { defineConfig } from 'vitepress'

const httpMiddlewares = [
  ['Overview', '/reference/server/http/middlewares/'],
  ['access_log', '/reference/server/http/middlewares/access_log'],
  ['add_prefix', '/reference/server/http/middlewares/add_prefix'],
  ['basic_auth', '/reference/server/http/middlewares/basic_auth'],
  ['block', '/reference/server/http/middlewares/block'],
  ['cors', '/reference/server/http/middlewares/cors'],
  ['decompress', '/reference/server/http/middlewares/decompress'],
  ['dns_path', '/reference/server/http/middlewares/dns_path'],
  ['folder', '/reference/server/http/middlewares/folder'],
  ['forward', '/reference/server/http/middlewares/forward'],
  ['grpcui', '/reference/server/http/middlewares/grpc_ui'],
  ['gzip', '/reference/server/http/middlewares/gzip'],
  ['headers', '/reference/server/http/middlewares/headers'],
  ['hello', '/reference/server/http/middlewares/hello'],
  ['iam', '/reference/server/http/middlewares/iam'],
  ['iam_check', '/reference/server/http/middlewares/iam_check'],
  ['iam_forward_auth', '/reference/server/http/middlewares/iam_forward_auth'],
  ['info', '/reference/server/http/middlewares/info'],
  ['inject', '/reference/server/http/middlewares/inject'],
  ['log', '/reference/server/http/middlewares/log'],
  ['login', '/reference/server/http/middlewares/login'],
  ['oauth2', '/reference/server/http/middlewares/oauth2'],
  ['path', '/reference/server/http/middlewares/path'],
  ['print', '/reference/server/http/middlewares/print'],
  ['rate_limit', '/reference/server/http/middlewares/rate_limit'],
  ['redirect_continue', '/reference/server/http/middlewares/redirect_continue'],
  ['redirection', '/reference/server/http/middlewares/redirection'],
  ['regex_path', '/reference/server/http/middlewares/regex_path'],
  ['request', '/reference/server/http/middlewares/request'],
  ['request_id', '/reference/server/http/middlewares/request_id'],
  ['role', '/reference/server/http/middlewares/role'],
  ['role_check', '/reference/server/http/middlewares/role_check'],
  ['role_data', '/reference/server/http/middlewares/role_data'],
  ['scope', '/reference/server/http/middlewares/scope'],
  ['service', '/reference/server/http/middlewares/service'],
  ['session', '/reference/server/http/middlewares/session'],
  ['session_info', '/reference/server/http/middlewares/session_info'],
  ['set', '/reference/server/http/middlewares/set'],
  ['splitter', '/reference/server/http/middlewares/splitter'],
  ['strip_prefix', '/reference/server/http/middlewares/strip_prefix'],
  ['template', '/reference/server/http/middlewares/template'],
  ['token_pass', '/reference/server/http/middlewares/token_pass'],
  ['try', '/reference/server/http/middlewares/try'],
  ['url', '/reference/server/http/middlewares/url'],
  ['view', '/reference/server/http/middlewares/view'],
].map(([text, link]) => ({ text, link }))

export default defineConfig({
  title: 'turna',
  description: 'turna documentation',
  base: '/turna/',
  themeConfig: {
    nav: [
      { text: 'Home', link: '/' },
      { text: 'Docs', link: '/introduction/getting-started' },
      { text: 'Reference', link: '/reference/config' },
    ],

    sidebar: [
      {
        text: 'Introduction',
        items: [
          { text: 'Getting Started', link: '/introduction/getting-started' },
          { text: 'Command Line Interface', link: '/introduction/cli' },
          { text: 'Features', link: '/introduction/features' },
        ],
      },
      {
        text: 'Reference',
        items: [
          { text: 'Config', link: '/reference/config' },
          { text: 'Loads', link: '/reference/loads' },
          {
            text: 'Preprocess',
            items: [
              { text: 'Preprocess', link: '/reference/preprocess/preprocess' },
              {
                text: 'Modules',
                collapsed: true,
                items: [
                  { text: 'replace', link: '/reference/preprocess/modules/replace' },
                ],
              },
            ],
          },
          {
            text: 'Server',
            items: [
              { text: 'Server', link: '/reference/server/server' },
              {
                text: 'HTTP',
                collapsed: true,
                items: [
                  {
                    text: 'Middlewares',
                    collapsed: true,
                    items: httpMiddlewares,
                  },
                ],
              },
              {
                text: 'TCP',
                collapsed: true,
                items: [
                  {
                    text: 'Middlewares',
                    collapsed: true,
                    items: [
                      { text: 'ip_allow_list', link: '/reference/server/tcp/middlewares/ip_allow_list' },
                      { text: 'redirect', link: '/reference/server/tcp/middlewares/redirect' },
                      { text: 'socks5', link: '/reference/server/tcp/middlewares/socks5' },
                    ],
                  },
                ],
              },
            ],
          },
          { text: 'Services', link: '/reference/services' },
        ],
      },
      {
        text: 'Examples',
        collapsed: true,
        items: [
          { text: 'Basic Auth', link: '/examples/basic_auth' },
          { text: 'Env', link: '/examples/env' },
          { text: 'Folder', link: '/examples/folder' },
          { text: 'Login', link: '/examples/login' },
          { text: 'OAuth2', link: '/examples/oauth2' },
          { text: 'Preprocess', link: '/examples/preprocess' },
          { text: 'TLS', link: '/examples/tls' },
          { text: 'View', link: '/examples/view' },
        ],
      },
    ],

    socialLinks: [
      { icon: 'github', link: 'https://github.com/rakunlabs/turna' },
    ],
  },
})
