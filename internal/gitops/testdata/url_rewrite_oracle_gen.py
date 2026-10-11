"""Regenerate url_rewrite_oracle.json from the Oracle (v0.33.0 when written).

Run with the Oracle checkout's interpreter:
    <oracle>/.venv/bin/python url_rewrite_oracle_gen.py > url_rewrite_oracle.json
"""
import json
from urllib.parse import urlsplit

from apm_cli.utils import git_env as g

POLICY = [
    # (remote, effective, has_authorization)
    ("https://github.com/o/r.git", "ext::sh -c evil", False),
    ("https://github.com/o/r.git", "https://user:pw@github.com/o/r.git", False),
    ("https://github.com/o/r.git", "https://token@github.com/o/r.git", False),
    ("https://github.com/o/r.git", "ssh://git:pw@github.com/o/r.git", False),
    ("https://github.com/o/r.git", "http://github.com/o/r.git", False),
    ("git@github.com:o/r.git", "http://github.com/o/r.git", False),
    ("ssh://git@github.com/o/r.git", "http://github.com/o/r.git", False),
    ("http://github.com/o/r.git", "http://mirror.example/o/r.git", False),
    ("https://github.com/o/r.git", "git://github.com/o/r.git", False),
    ("https://github.com/o/r.git", "ftp://github.com/o/r.git", False),
    ("https://github.com/o/r.git", "github.com:o/r.git", False),
    ("https://user:pw@github.com/o/r.git", "https://github.com/x/r.git", False),
    ("https://token@github.com/o/r.git", "ssh://git@github.com/o/r.git", False),
    ("https://github.com/o/r.git", "https://github.com:8443/o/r.git", True),
    ("https://github.com/o/r.git", "https://github.com:8443/o/r.git", False),
    ("http://github.com/o/r.git", "http://github.com:8080/o/r.git", True),
    ("https://github.com/o/r.git", "https://mirror.example/o/r.git", True),
    ("https://github.com/o/r.git", "https://mirror.example/o/r.git", False),
    ("https://github.com/o/r.git", "ssh://git@gitlab.com/o/r.git", False),
    ("https://github.com/o/r.git", "git@gitlab.com:o/r.git", False),
    ("git@github.com:o/r.git", "git@gitlab.com:o/r.git", False),
    ("https://github.com/o/r.git", "ssh://git@github.com/o/r.git", False),
    ("https://github.com/o/r.git", "git@github.com:o/r.git", False),
    ("https://github.com/o/r.git", "ssh://git@GitHub.com:22/o/r.git", False),
    ("https://github.com/o/r.git", "file:///srv/mirror/o/r.git", False),
    ("https://github.com/o/r.git", "/srv/mirror/o/r.git", False),
    ("https://github.com/o/r.git", "https://github.com/other/r.git", True),
    ("https://github.com/o/r.git", "https://github.com:443/o/r.git", True),
    ("./local/repo", "/srv/mirror/repo", False),
    ("/srv/a/repo", "https://github.com/o/r.git", False),
    ("https://github.com/o/r.git", "https://github.com:notaport/o/r.git", False),
    ("https://github.com/o/r.git", "https://[bad/o/r.git", False),
    ("https://github.com/o/r.git", "https://github.com:70000/o/r.git", False),
    ("https://github.com/o/r.git", "file://user@host/srv/r.git", False),
]

SPLIT = [
    "https://github.com/o/r.git",
    "HTTPS://User:Pw@GitHub.COM:8443/o/r.git?x#y",
    "git@github.com:o/r.git",
    "github.com:o/r.git",
    "ssh://git@github.com/o/r.git",
    "/srv/mirror/r.git",
    "./rel/path",
    "file:///srv/r.git",
    "C:\\repo",
    " \thttps://github.com/o",
    "https://[::1]:8080/o",
    "https://[fe80::1%eth0]/o",
    "https://[v1.x]/o",
    "https://[1.2.3.4]/o",
    "https://x[::1]/o",
    "https://[::1]x/o",
    "https://[::1/o",
    "https://a@b@host/o",
    "https://host:/o",
    "https://host:0/o",
    "1http://host/o",
    "ext::sh -c x",
]

HEADERS = [
    "",
    "   ",
    "Authorization: Bearer abc",
    "authorization: basic Zm9v",
    "X-Custom: hello",
    "X-Api-Key: hello",
    "X-Custom: token abc",
    "X-Custom: ghp_abcdef123456",
    "X-Custom: xghp_abcdef123456",
    "X-Custom: glpat-abcdef-more",
    "X-Custom: " + "a" * 52,
    "X-Custom: " + "a" * 53,
    "Cookie: a=b",
    "no-colon-here",
    "Bad Name: x",
    "X-Custom: a\rb",
    "X-Custom: a\x7fb",
    "X-Custom:\ttab ok",
    "Proxy-Authorization: x",
    "X-Secretive: x",
    "X-My_Token: x",
]

out = {"policy": [], "split": [], "headers": []}
for remote, effective, auth in POLICY:
    case = {"remote": remote, "effective": effective, "has_authorization": auth}
    try:
        g.validate_resolved_git_url_rewrite(remote, effective, has_authorization=auth)
    except g.GitUrlRewriteError as exc:
        case["reason"] = exc.reason
        case["message"] = str(exc)
    except ValueError as exc:
        case["message"] = str(exc)
    out["policy"].append(case)

for url in SPLIT:
    case = {"url": url}
    try:
        p = urlsplit(url)
        _scheme, _host, port = g._url_origin(url)
        case.update(
            scheme=p.scheme,
            netloc=p.netloc,
            hostname=p.hostname or "",
            port=port,
            username=p.username is not None,
            password=p.password is not None,
        )
    except ValueError:
        case["error"] = True
    out["split"].append(case)

for value in HEADERS:
    out["headers"].append(
        {
            "value": value,
            "valid": g._is_valid_http_extraheader_value(value),
            "credential": g._is_credential_bearing_http_header(value),
        }
    )

print(json.dumps(out, indent=1))
