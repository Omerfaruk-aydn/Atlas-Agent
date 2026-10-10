"""End-to-end tests for the generated app.

Each run starts the real server on a free port with an empty temporary
data folder, then exercises every collection over HTTP. Standard library
only: run with `python -m unittest discover -s tests`.
"""

import json
import os
import socket
import subprocess
import sys
import tempfile
import time
import unittest
import urllib.error
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SCHEMA = json.loads((ROOT / "schema.json").read_text(encoding="utf-8"))
SAMPLES = {
    "text": "Sample",
    "longtext": "Sample note",
    "integer": 7,
    "number": 3.5,
    "date": "2026-02-01",
    "boolean": True,
}


def sample(field):
    if field["type"] == "choice":
        return field["choices"][0]
    return SAMPLES[field["type"]]


def free_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


class AppTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.data = tempfile.TemporaryDirectory()
        cls.port = free_port()
        env = dict(os.environ, PORT=str(cls.port), ATLAS_APP_DATA=cls.data.name, PYTHONDONTWRITEBYTECODE="1")
        cls.proc = subprocess.Popen(
            [sys.executable, str(ROOT / "server.py")],
            cwd=ROOT,
            env=env,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.PIPE,
        )
        deadline = time.time() + 20
        while time.time() < deadline:
            try:
                if cls.request("GET", "/healthz")[0] == 200:
                    return
            except OSError:
                pass
            if cls.proc.poll() is not None:
                raise RuntimeError("server exited: " + cls.proc.stderr.read().decode(errors="replace"))
            time.sleep(0.1)
        raise RuntimeError("server did not become healthy")

    @classmethod
    def tearDownClass(cls):
        cls.proc.terminate()
        try:
            cls.proc.wait(timeout=10)
        except subprocess.TimeoutExpired:
            cls.proc.kill()
        cls.proc.stderr.close()
        cls.data.cleanup()

    @classmethod
    def request(cls, method, path, body=None, headers=None):
        data = json.dumps(body).encode() if body is not None else None
        req = urllib.request.Request(f"http://127.0.0.1:{cls.port}{path}", data=data, method=method)
        if body is not None:
            req.add_header("Content-Type", "application/json")
        for k, v in (headers or {}).items():
            req.add_header(k, v)
        try:
            with urllib.request.urlopen(req, timeout=10) as res:
                raw = res.read()
                return res.status, json.loads(raw) if raw and "json" in res.headers.get("Content-Type", "") else raw
        except urllib.error.HTTPError as err:
            raw = err.read()
            return err.code, json.loads(raw) if raw else None

    def test_health(self):
        status, body = self.request("GET", "/healthz")
        self.assertEqual(status, 200)
        self.assertEqual(body["status"], "ok")

    def test_page_is_served(self):
        status, body = self.request("GET", "/")
        self.assertEqual(status, 200)
        self.assertIn(b"<html", body.lower())

    def test_crud_for_every_collection(self):
        for entity in SCHEMA["entities"]:
            with self.subTest(entity=entity["name"]):
                payload = {f["name"]: sample(f) for f in entity["fields"]}
                status, created = self.request("POST", f"/api/{entity['name']}", payload)
                self.assertEqual(status, 201, created)
                record_id = created["id"]
                status, listing = self.request("GET", f"/api/{entity['name']}")
                self.assertEqual(status, 200)
                self.assertIn(record_id, [item["id"] for item in listing["items"]])
                first = entity["fields"][0]
                change = {first["name"]: sample(first)}
                status, updated = self.request("PUT", f"/api/{entity['name']}/{record_id}", change)
                self.assertEqual(status, 200, updated)
                status, _ = self.request("DELETE", f"/api/{entity['name']}/{record_id}")
                self.assertEqual(status, 204)
                status, _ = self.request("GET", f"/api/{entity['name']}/{record_id}")
                self.assertEqual(status, 404)

    def test_required_fields_are_enforced(self):
        for entity in SCHEMA["entities"]:
            required = [f for f in entity["fields"] if f.get("required")]
            if not required:
                continue
            with self.subTest(entity=entity["name"]):
                status, body = self.request("POST", f"/api/{entity['name']}", {})
                self.assertEqual(status, 400)
                self.assertEqual(body["field"], required[0]["name"])

    def test_foreign_host_is_refused(self):
        status, _ = self.request("GET", "/api/_schema", headers={"Host": "attacker.example"})
        self.assertEqual(status, 403)

    def test_summary(self):
        status, body = self.request("GET", "/api/_summary")
        self.assertEqual(status, 200)
        for entity in SCHEMA["entities"]:
            self.assertIn(entity["name"], body)


if __name__ == "__main__":
    unittest.main()
