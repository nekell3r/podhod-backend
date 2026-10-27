#!/usr/bin/env python3
"""Complete private-loopback Keycloak configuration without logging credentials."""
from pathlib import Path
import json
import urllib.request
import urllib.parse

base = "http://127.0.0.1:8088/auth"
password = Path("secrets/keycloak_admin_password").read_text().strip()
body = urllib.parse.urlencode({"grant_type": "password", "client_id": "admin-cli", "username": "podhod-admin", "password": password}).encode()
with urllib.request.urlopen(urllib.request.Request(base + "/realms/master/protocol/openid-connect/token", data=body), timeout=20) as response:
    token = json.load(response)["access_token"]

def call(path, method="GET", payload=None):
    data = None if payload is None else json.dumps(payload).encode()
    headers = {"Authorization": "Bearer " + token, "Content-Type": "application/json"}
    with urllib.request.urlopen(urllib.request.Request(base + "/admin/realms/podhod" + path, data=data, headers=headers, method=method), timeout=20) as response:
        content = response.read()
        return json.loads(content) if content else None

profile = call("/users/profile")
for attribute in profile["attributes"]:
    if attribute["name"] != "username":
        attribute.pop("required", None)
call("/users/profile", "PUT", profile)
realm = call("")
realm.update(offlineSessionIdleTimeout=7776000, offlineSessionMaxLifespanEnabled=True, offlineSessionMaxLifespan=31536000)
call("", "PUT", realm)
scopes = call("/client-scopes")
offline = next((scope for scope in scopes if scope["name"] == "offline_access"), None)
if offline is None:
    call("/client-scopes", "POST", {"name": "offline_access", "protocol": "openid-connect", "attributes": {"display.on.consent.screen": "true"}})
    offline = next(scope for scope in call("/client-scopes") if scope["name"] == "offline_access")
for client_name in ["podhod-ios", "podhod-chatgpt"]:
    client = call("/clients?clientId=" + client_name)[0]
    mappers = call("/clients/" + client["id"] + "/protocol-mappers/models")
    if not any(mapper["protocolMapper"] == "oidc-sub-mapper" for mapper in mappers):
        call("/clients/" + client["id"] + "/protocol-mappers/models", "POST", {
            "name": "subject", "protocol": "openid-connect", "protocolMapper": "oidc-sub-mapper",
            "config": {"access.token.claim": "true", "introspection.token.claim": "true"}})
    optional = call("/clients/" + client["id"] + "/optional-client-scopes")
    if not any(scope["name"] == "offline_access" for scope in optional):
        call("/clients/" + client["id"] + "/optional-client-scopes/" + offline["id"], "PUT")
for user in call("/users?username=owner&exact=true"):
    user["requiredActions"] = []
    call("/users/" + user["id"], "PUT", user)
    offline_role = call("/roles/offline_access")
    call("/users/" + user["id"] + "/role-mappings/realm", "POST", [offline_role])
print("Identity configured: username login, PKCE, offline scope; no personal profile fields required")
