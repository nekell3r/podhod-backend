#!/usr/bin/env python3
"""Generate initial deployment secrets locally; never prints credentials."""
import argparse
import json
import os
from pathlib import Path
import secrets
import uuid

parser = argparse.ArgumentParser()
parser.add_argument("directory", type=Path)
parser.add_argument("--base", required=True, help="Public HTTPS origin, for example https://api.example.com")
args = parser.parse_args()
root = args.directory
root.mkdir(parents=True, exist_ok=True, mode=0o700)
secret_dir = root / "secrets"
secret_dir.mkdir(mode=0o700)

def save(name, value):
    path = secret_dir / name
    if path.exists():
        raise SystemExit("Refusing to replace existing deployment secrets")
    path.write_text(value)
    path.chmod(0o600)

passwords = {name: secrets.token_urlsafe(30) for name in [
    "postgres_password", "app_db_password", "keycloak_db_password", "keycloak_admin_password"
]}
for name, value in passwords.items():
    save(name, value)
save("init_app_db_password", passwords["app_db_password"])
save("init_keycloak_db_password", passwords["keycloak_db_password"])
save("database_url", "postgres://podhod:" + passwords["app_db_password"] + "@db:5432/podhod?sslmode=disable")
owner_password = secrets.token_urlsafe(24)
chatgpt_secret = secrets.token_urlsafe(36)
scopes = ["schedule:read", "schedule:write", "workout:read", "workout:write"]
def audience(name, value):
    return {"name": name, "protocol": "openid-connect", "protocolMapper": "oidc-audience-mapper",
            "config": {"included.custom.audience": value, "id.token.claim": "false", "access.token.claim": "true"}}
def client(client_id, redirect, public):
    result = {"clientId": client_id, "enabled": True, "protocol": "openid-connect",
              "publicClient": public, "standardFlowEnabled": True, "implicitFlowEnabled": False,
              "directAccessGrantsEnabled": False, "serviceAccountsEnabled": False,
              "fullScopeAllowed": False, "redirectUris": [redirect], "webOrigins": [],
              "attributes": {"pkce.code.challenge.method": "S256", "use.refresh.tokens": "true"},
              "defaultClientScopes": ["profile"], "optionalClientScopes": scopes + ["offline_access"],
              "protocolMappers": [audience("podhod-api", "podhod-api"),
                  {"name": "subject", "protocol": "openid-connect", "protocolMapper": "oidc-sub-mapper",
                   "config": {"access.token.claim": "true", "introspection.token.claim": "true"}}]}
    if not public:
        result["secret"] = chatgpt_secret
        result["protocolMappers"].append(audience("podhod-mcp", args.base + "/mcp"))
    return result
realm = {"realm": "podhod", "enabled": True, "displayName": "Подход", "sslRequired": "external",
         "registrationAllowed": False, "resetPasswordAllowed": False, "rememberMe": True,
         "loginWithEmailAllowed": False, "duplicateEmailsAllowed": True,
         "bruteForceProtected": True, "failureFactor": 5, "waitIncrementSeconds": 60,
         "maxFailureWaitSeconds": 900, "accessTokenLifespan": 300,
         "ssoSessionIdleTimeout": 2592000, "ssoSessionMaxLifespan": 7776000,
         "offlineSessionIdleTimeout": 7776000, "offlineSessionMaxLifespanEnabled": True, "offlineSessionMaxLifespan": 31536000,
         "revokeRefreshToken": True, "refreshTokenMaxReuse": 0, "passwordPolicy": "length(14)",
         "clientScopes": [{"name": scope, "protocol": "openid-connect",
                           "attributes": {"include.in.token.scope": "true", "display.on.consent.screen": "true"}}
                          for scope in scopes] + [{"name": "profile", "protocol": "openid-connect"}, {"name": "offline_access", "protocol": "openid-connect"}],
         "clients": [client("podhod-ios", "podhod://oauth/callback", True),
                     client("podhod-chatgpt", "https://chatgpt.com/connector_platform_oauth_redirect", False)],
         "users": [{"id": str(uuid.uuid4()), "username": "owner", "enabled": True,
                    "credentials": [{"type": "password", "value": owner_password, "temporary": False}]}]}
save("realm.json", json.dumps(realm, ensure_ascii=False))
(root / ".env").write_text("PUBLIC_BASE_URL=" + args.base + "\n")
(root / ".env").chmod(0o600)
access = {"appURL": args.base, "username": "owner", "password": owner_password,
          "keycloakAdminUsername": "podhod-admin", "keycloakAdminPassword": passwords["keycloak_admin_password"],
          "chatgptMCPURL": args.base + "/mcp", "chatgptClientID": "podhod-chatgpt",
          "chatgptClientSecret": chatgpt_secret}
(root / "access.json").write_text(json.dumps(access, indent=2, ensure_ascii=False))
(root / "access.json").chmod(0o600)
print("Deployment material prepared; credentials are in protected access.json")
