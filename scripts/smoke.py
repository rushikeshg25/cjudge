#!/usr/bin/env python3
"""Exercise a running stack. Set CJUDGE_ADMIN_TOKEN; no external dependencies."""
import json
import os
import time
import urllib.request
import uuid

base = os.environ.get("CJUDGE_URL", "http://localhost:8080")
token = os.environ["CJUDGE_ADMIN_TOKEN"]


def call(path, body=None, key=None):
    headers = {"Authorization": f"Bearer {token}", "Content-Type": "application/json"}
    if key:
        headers["Idempotency-Key"] = key
    req = urllib.request.Request(base + path, data=json.dumps(body).encode() if body is not None else None, headers=headers)
    with urllib.request.urlopen(req, timeout=20) as response:
        return json.load(response)


problem = call("/v1/problems", {"title": "A + B", "statement": "Print the sum of two integers.", "checker": "tokens", "limits": {"time_ms": 3000, "memory_mb": 128, "output_kb": 64}, "tests": [{"input": "2 3\n", "expected": "5\n"}, {"input": "-4 7\n", "expected": "3\n"}]})
assert "tests" not in problem
sources = {
    "cpp20": '#include <iostream>\nint main(){long long a,b;std::cin>>a>>b;std::cout<<a+b<<"\\n";}',
    "python3": "a,b=map(int,input().split());print(a+b)",
    "go": 'package main\nimport "fmt"\nfunc main(){var a,b int;fmt.Scan(&a,&b);fmt.Println(a+b)}',
}
for language, source in sources.items():
    body = {"problem_id": problem["id"], "language": language, "source": source}
    key = str(uuid.uuid4())
    submission = call("/v1/submissions", body, key)
    assert call("/v1/submissions", body, key)["id"] == submission["id"]
    deadline = time.monotonic() + 240
    while time.monotonic() < deadline:
        submission = call("/v1/submissions/" + submission["id"])
        if submission["state"] == "finished":
            break
        time.sleep(2)
    assert submission.get("result", {}).get("verdict") == "accepted", submission
    print(language, submission["id"], submission["result"]["verdict"])
