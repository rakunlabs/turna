# Security Policy

## Supported versions

Security fixes are released for the latest minor version. Upgrade to the newest release before reporting an issue.

| Version | Supported |
| --- | --- |
| latest `0.x` minor | yes |
| older | no |

## Reporting a vulnerability

Report vulnerabilities privately through
[GitHub private vulnerability reporting](https://github.com/rakunlabs/turna/security/advisories/new).
Do not open a public issue, pull request or discussion for a security problem.

Include as much of this as you can:

- affected version or commit, and the middleware or component (`auth`, `session`, `login`, `basic_auth`, ...)
- the relevant configuration with secrets removed
- steps to reproduce, a proof of concept, or the request and response
- the impact you expect (authentication bypass, privilege escalation, data exposure, ...)

## What to expect

| Step | Target |
| --- | --- |
| Acknowledge the report | 3 business days |
| Initial assessment and severity | 10 business days |
| Fix released for critical and high severity | 30 days |
| Fix released for medium and low severity | 90 days |

We keep you informed while we work on the fix, agree on a disclosure date with you, and publish a GitHub security advisory (with a CVE when it applies) together with the fixed release. Reporters are credited in the advisory unless they prefer otherwise.

If a report is not accepted, we explain why.

## Scope

In scope:

- the `turna` binary and container images published from this repository
- the HTTP middlewares, in particular the identity stack: `auth`, `session`, `login`, `jwt`, `basic_auth`, `forward_auth`, `iam_check`, `iam_forward_auth`, `oauth2_resource`, `role`, `scope`, `csrf`, `signed_url`
- the embedded admin and login UIs and the login SDK (`/auth/sdk.js`)

Out of scope:

- deployments that disable a documented protection (`insecure_skip_verify`, `allow_legacy_hashes`, `skip_audience_check`, `secure: false`, an empty `admin.permission`, ...)
- services reachable directly instead of through the `session` middleware, which trust a forged `X-User` header
- missing rate limiting on public endpoints (see [hardening notes](pkg/server/http/middleware/auth/SECURITY-CHANGES.md))
- denial of service through resource exhaustion without an amplification
- vulnerabilities in dependencies without a working exploit path in turna

## Safe harbor

We will not take legal action against research that follows this policy: act in good faith, test only against systems you own or are allowed to test, avoid privacy violations and service disruption, and give us reasonable time to fix the issue before disclosure.

## Hardening

Operational guidance and behavior changes made for security are kept in
[pkg/server/http/middleware/auth/SECURITY-CHANGES.md](pkg/server/http/middleware/auth/SECURITY-CHANGES.md).
