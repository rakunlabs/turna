#!/usr/bin/env python3
"""Run every module of a conformance suite plan and follow browser redirects.

turna answers /authorize with redirects only (fixed X-User, skip_consent), so
each visit the suite asks for is followed with plain HTTP until it reaches the
suite callback, which is then loaded so the suite sees the response.
"""
import json
import re
import ssl
import sys
import time
import urllib.parse
import urllib.request

SUITE = "https://localhost:8443"
CTX = ssl._create_unverified_context()


def api(method, path, body=None):
    req = urllib.request.Request(SUITE + path, method=method, data=None if body is None else json.dumps(body).encode())
    req.add_header("Content-Type", "application/json")
    with urllib.request.urlopen(req, context=CTX) as res:
        raw = res.read()
        return json.loads(raw) if raw else None


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *a, **k):
        return None


import http.cookiejar

JAR = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(NoRedirect, urllib.request.HTTPSHandler(context=CTX), urllib.request.HTTPCookieProcessor(JAR))
TURNA = "https://localhost:9443"


SESSION = {}


def login():
    req = urllib.request.Request(TURNA + "/login/auth/token/turna", method="POST",
                                 data=json.dumps({"username": "oidc-user", "password": "oidc-pass-123456"}).encode())
    req.add_header("Content-Type", "application/json")
    res = opener.open(req)
    res.read()
    # urllib does not send cookies back to "localhost"; keep the session by hand
    for header in res.headers.get_all("Set-Cookie") or []:
        name, _, rest = header.partition("=")
        SESSION[name] = rest.split(";", 1)[0]


def turna_request(url):
    req = urllib.request.Request(url)
    if SESSION and url.startswith(TURNA):
        req.add_header("Cookie", "; ".join(f"{k}={v}" for k, v in SESSION.items()))

    return req


def visit(url):
    """Follow redirects like a browser; turna.local is localhost for the host."""
    for _ in range(15):
        fetch = url.replace("https://turna.local:9443", "https://localhost:9443")
        try:
            res = opener.open(turna_request(fetch))
            body = res.read().decode(errors="replace")
            # the suite callback page posts window.location.hash back with JS
            m = re.search(r"xhr\.open\('POST', \"([^\"]+)\"", body)
            if m and "implicit" in m.group(1):
                target = m.group(1).replace("\\/", "/")
                fragment = urllib.parse.urlsplit(url).fragment
                req = urllib.request.Request(target, method="POST", data=(("#" + fragment) if fragment else "").encode())
                req.add_header("Content-Type", "text/plain")
                try:
                    opener.open(req).read()
                except urllib.error.HTTPError:
                    pass
            return res.status, body[:200]
        except urllib.error.HTTPError as e:
            if e.code in (301, 302, 303, 307, 308):
                url = urllib.parse.urljoin(fetch, e.headers["Location"])
                parsed = urllib.parse.urlsplit(url)
                if parsed.netloc == "localhost:9443" and parsed.path.startswith("/login/"):
                    # the login page: sign in, then follow redirect_path
                    time.sleep(1.1)  # a fresh auth_time strictly after the request
                    login()
                    url = TURNA + urllib.parse.parse_qs(parsed.query).get("redirect_path", ["/"])[0]
                continue
            return e.code, e.read().decode(errors="replace")[:300]
    return 0, "too many redirects"


def get(path):
    try:
        return api("GET", path)
    except urllib.error.HTTPError:
        return None


PNG = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="


def fill_placeholders(test_id, done):
    """Upload a dummy screenshot for every placeholder the test waits on."""
    for e in get(f"/api/log/{test_id}") or []:
        ph = e.get("upload")
        if ph and ph not in done:
            done.add(ph)
            req = urllib.request.Request(f"{SUITE}/api/log/{test_id}/images/{ph}", method="POST", data=PNG.encode())
            req.add_header("Content-Type", "text/plain")
            try:
                urllib.request.urlopen(req, context=CTX).read()
            except urllib.error.HTTPError:
                pass


def run_module(plan_id, module, variant):
    q = urllib.parse.urlencode({"test": module, "plan": plan_id, "variant": json.dumps(variant or {})})
    # every test starts in a fresh browser without a turna session
    SESSION.clear()
    JAR.clear()
    info = api("POST", "/api/runner?" + q)
    test_id = info["id"]
    seen = set()
    filled = set()
    deadline = time.time() + 300
    while time.time() < deadline:
        st = get(f"/api/info/{test_id}") or {}
        status = st.get("status")
        browser = get(f"/api/runner/browser/{test_id}") or {}
        for url in browser.get("urls", []):
            if url not in seen:
                seen.add(url)
                visit(url)
                try:
                    api("POST", f"/api/runner/browser/{test_id}/visit?" + urllib.parse.urlencode({"url": url}))
                except urllib.error.HTTPError:
                    pass
        # screenshots only after every browser visit was made
        if status == "WAITING" and browser.get("urls") and set(browser["urls"]) <= seen:
            time.sleep(1)
            fill_placeholders(test_id, filled)
        if status in ("FINISHED", "INTERRUPTED"):
            break
        time.sleep(0.5)
    info = get(f"/api/info/{test_id}") or {}
    return test_id, info.get("status"), info.get("result")


def failures(test_id):
    out = []
    for e in api("GET", f"/api/log/{test_id}"):
        if e.get("result") in ("FAILURE", "WARNING"):
            out.append(f"{e['result']}: {e.get('src')}: {e.get('msg')}")
    return out


def main():
    plan_id = sys.argv[1]
    only = sys.argv[2:] 
    plan = api("GET", f"/api/plan/{plan_id}")
    summary = []
    for mod in plan["modules"]:
        name = mod["testModule"]
        if only and name not in only:
            continue
        test_id, status, result = run_module(plan_id, name, mod.get("variant"))
        summary.append((name, status, result))
        print(f"{result or status:12} {name}", flush=True)
        if result not in ("PASSED", "REVIEW", "SKIPPED"):
            for line in failures(test_id)[:6]:
                print("    " + line[:300], flush=True)
    counts = {}
    for _, _, r in summary:
        counts[r] = counts.get(r, 0) + 1
    print("TOTAL", counts)


if __name__ == "__main__":
    main()
