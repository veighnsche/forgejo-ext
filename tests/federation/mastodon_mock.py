#!/usr/bin/env python3
"""
Live protocol-mismatch mock: a minimal "Mastodon-like" fediverse instance.

It advertises NodeInfo with software name "mastodon" (i.e. NOT a forge) and
exposes a fake repository actor endpoint. It records any POSTs to its
repository inbox so we can prove that a proper Forgejo peer will NOT send
ForgeFed repository activities (Like/Undo Like) to a non-forge host.
"""
import json
import re
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

POSTS = []


class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        sys.stderr.write("mock: %s\n" % (fmt % args))

    def _send(self, code, body, ctype="application/json"):
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        path = self.path
        if path == "/.well-known/nodeinfo":
            body = json.dumps({"links": [{
                "href": "http://localhost:%d/api/v1/nodeinfo" % self.server.server_port,
                "rel": "http://nodeinfo.diaspora.software/ns/schema/2.1",
            }]}).encode()
            self._send(200, body)
        elif path == "/api/v1/nodeinfo":
            # Advertise as Mastodon: NOT a forge, does NOT support ForgeFed
            body = json.dumps({
                "version": "2.1",
                "software": {"name": "mastodon", "version": "4.2.10"},
                "protocols": ["activitypub"],
                "openRegistrations": True,
                "usage": {"users": {"total": 42}},
            }).encode()
            self._send(200, body)
        elif re.match(r"^/api/v1/activitypub/repository-id/\d+$", path):
            # Fake repository actor
            actor = {
                "@context": ["https://www.w3.org/ns/activitystreams", "https://w3id.org/security/v1"],
                "id": "http://localhost:%d%s" % (self.server.server_port, path),
                "type": "Repository",
                "inbox": "http://localhost:%d%s/inbox" % (self.server.server_port, path),
                "outbox": "http://localhost:%d%s/outbox" % (self.server.server_port, path),
                "name": "fake-repo",
            }
            self._send(200, json.dumps(actor).encode(), ctype="application/activity+json")
        else:
            self._send(404, json.dumps({"error": "not found"}).encode())

    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(length)
        POSTS.append({"path": self.path, "body": body.decode(errors="replace")})
        print("MOCK_POST: %s %s" % (self.path, body[:200].decode(errors="replace")), flush=True)
        self._send(204, b"")


if __name__ == "__main__":
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 4000
    server = HTTPServer(("127.0.0.1", port), Handler)
    print("mastodon-mock listening on %d" % port, flush=True)
    server.serve_forever()