# OpenID conformance

`./run.sh` runs the OpenID Foundation [conformance suite](https://gitlab.com/openid/conformance-suite)
(`release-v5.3.4`) against a local turna and prints one line per test.

It starts PostgreSQL and the suite in Docker, builds turna, seeds a user and
three OAuth clients through the admin API on `127.0.0.1:9081`, then runs the
Config OP and Basic OP certification plans. `run.py` plays the browser: it
follows redirects, signs in on the turna login page, posts the suite callback
and uploads a placeholder for screenshot steps.

The suite reaches turna as `https://turna.local:9443` (`host-gateway`), which
is the configured `oauth2.base_url`.

Results marked `REVIEW` need a human to look at screenshots and are expected
locally. For a certification submission, run the plans on
<https://www.certification.openid.net> against a public deployment.
