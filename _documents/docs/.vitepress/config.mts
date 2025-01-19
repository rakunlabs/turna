import { defineConfig } from 'vitepress'

// https://vitepress.dev/reference/site-config
export default defineConfig({
  title: "turna",
  description: "turna documentation",
  base: "/turna/",
  themeConfig: {
    // https://vitepress.dev/reference/default-theme-config
    nav: [
      { text: 'Home', link: '/' },
      { text: 'Documents', link: '/introduction/getting-started.md' }
    ],

    sidebar: [
      {
        text: 'Introduction',
        items: [
          { text: 'Getting Started', link: '/introduction/getting-started' },
          { text: 'Command Line Interface', link: '/introduction/cli' },
          { text: 'Features', link: '/introduction/features' },
        ]
      },
      {
        text: 'Reference',
        items: [
          { text: 'Config', link: '/reference/config.md' },
          { text: 'Loads', link: '/reference/loads.md' },
          { text: 'Preprocess',
            items: [
              {text: 'Preprocess', link: '/reference/preprocess/preprocess.md' },
              {
                text: 'Modules',
                collapsed: true,
                items: [
                  {text: 'replace', link: '/reference/preprocess/modules/replace.md' },
                ],
              },
            ],
          },
          {
            text: 'Server',
            items: [
              { text: 'Server', link: '/reference/server/server.md' },
              {
                text: 'HTTP',
                collapsed: true,
                items: [
                  {
                    text: "Middlewares",
                    collapsed: true,
                    items: [
                      { text: 'add_prefix', link: '/reference/server/http/middlewares/add_prefix.md' },
                      { text: 'auth', link: '/reference/server/http/middlewares/auth.md' },
                      { text: 'basic_auth', link: '/reference/server/http/middlewares/basic_auth.md' },
                      { text: 'block', link: '/reference/server/http/middlewares/block.md' },
                      { text: 'cors', link: '/reference/server/http/middlewares/cors.md' },
                      { text: 'decompress', link: '/reference/server/http/middlewares/decompress.md' },
                      { text: 'dns_path', link: '/reference/server/http/middlewares/dns_path.md' },
                      { text: 'folder', link: '/reference/server/http/middlewares/folder.md' },
                      { text: 'forward', link: '/reference/server/http/middlewares/forward.md' },
                      { text: 'grpc_ui', link: '/reference/server/http/middlewares/grpc_ui.md' },
                      { text: 'gzip', link: '/reference/server/http/middlewares/gzip.md' },
                      { text: 'headers', link: '/reference/server/http/middlewares/headers.md' },
                      { text: 'hello', link: '/reference/server/http/middlewares/hello.md' },
                      { text: 'info', link: '/reference/server/http/middlewares/info.md' },
                      { text: 'inject', link: '/reference/server/http/middlewares/inject.md' },
                      { text: 'log', link: '/reference/server/http/middlewares/log.md' },
                      { text: 'login', link: '/reference/server/http/middlewares/login.md' },
                      { text: 'regex_path', link: '/reference/server/http/middlewares/regex_path.md' },
                      { text: 'role', link: '/reference/server/http/middlewares/role.md' },
                      { text: 'role_check', link: '/reference/server/http/middlewares/role_check.md' },
                      { text: 'role_data', link: '/reference/server/http/middlewares/role_data.md' },
                      { text: 'scope', link: '/reference/server/http/middlewares/scope.md' },
                      { text: 'service', link: '/reference/server/http/middlewares/service.md' },
                      { text: 'session', link: '/reference/server/http/middlewares/session.md'},
                      { text: 'session_info', link: '/reference/server/http/middlewares/session_info.md' },
                      { text: 'set', link: '/reference/server/http/middlewares/set.md' },
                      { text: 'strip_prefix', link: '/reference/server/http/middlewares/strip_prefix.md' },
                      { text: 'template', link: '/reference/server/http/middlewares/template.md' },
                      { text: 'view', link: '/reference/server/http/middlewares/view.md' },
                    ],
                  },
                ],
              },
              {
                text: "TCP",
                collapsed: true,
                items: [
                  {
                    text: "Middlewares",
                    collapsed: true,
                    items: [
                      { text: 'ip_allow_list', link: '/reference/server/tcp/middlewares/ip_allow_list.md' },
                      { text: 'redirect', link: '/reference/server/tcp/middlewares/redirect.md' },
                      { text: 'socks5', link: '/reference/server/tcp/middlewares/socks5.md' },
                    ],
                  },
                ]
              },
            ],
          },
          { text: 'Services', link: '/reference/services.md' },
        ]
      },
      {
        text: 'Examples',
        collapsed: true,
        items: [
          { text: 'Basic Auth', link: '/examples/basic_auth.md' },
          { text: 'Env', link: '/examples/env.md' },
          { text: 'Folder', link: '/examples/folder.md' },
          { text: 'Login', link: '/examples/login.md' },
          { text: 'Oauth2', link: '/examples/oauth2.md' },
          { text: 'Preprocess', link: '/examples/preprocess.md' },
          { text: 'TLS', link: '/examples/tls.md' },
          { text: 'View', link: '/examples/view.md' },
        ],
      }
    ],

    socialLinks: [
      { icon: 'github', link: 'https://github.com/worldline-go/turna' }
    ]
  }
})
