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
                text: 'Middlewares',
                collapsed: true,
                items: [
                  { text: 'add_prefix', link: '/reference/server/middlewares/add_prefix.md' },
                  { text: 'auth', link: '/reference/server/middlewares/auth.md' },
                  { text: 'basic_auth', link: '/reference/server/middlewares/basic_auth.md' },
                  { text: 'block', link: '/reference/server/middlewares/block.md' },
                  { text: 'cors', link: '/reference/server/middlewares/cors.md' },
                  { text: 'decompress', link: '/reference/server/middlewares/decompress.md' },
                  { text: 'folder', link: '/reference/server/middlewares/folder.md' },
                  { text: 'gzip', link: '/reference/server/middlewares/gzip.md' },
                  { text: 'headers', link: '/reference/server/middlewares/headers.md' },
                  { text: 'hello', link: '/reference/server/middlewares/hello.md' },
                  { text: 'info', link: '/reference/server/middlewares/info.md' },
                  { text: 'inject', link: '/reference/server/middlewares/inject.md' },
                  { text: 'log', link: '/reference/server/middlewares/log.md' },
                  { text: 'login', link: '/reference/server/middlewares/login.md' },
                  { text: 'regex_path', link: '/reference/server/middlewares/regex_path.md' },
                  { text: 'role', link: '/reference/server/middlewares/role.md' },
                  { text: 'role_check', link: '/reference/server/middlewares/role_check.md' },
                  { text: 'role_data', link: '/reference/server/middlewares/role_data.md' },
                  { text: 'scope', link: '/reference/server/middlewares/scope.md' },
                  { text: 'service', link: '/reference/server/middlewares/service.md' },
                  { text: 'session', link: '/reference/server/middlewares/session.md'},
                  { text: 'session_info', link: '/reference/server/middlewares/session_info.md' },
                  { text: 'set', link: '/reference/server/middlewares/set.md' },
                  { text: 'strip_prefix', link: '/reference/server/middlewares/strip_prefix.md' },
                  { text: 'template', link: '/reference/server/middlewares/template.md' },
                  { text: 'openfga', link: '/reference/server/middlewares/openfga.md' },
                  { text: 'openfga_check', link: '/reference/server/middlewares/openfga_check.md' },
                  { text: 'view', link: '/reference/server/middlewares/view.md' },
                ],
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
          { text: 'Service OpenFga', link: '/examples/service_openfga.md' },
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
