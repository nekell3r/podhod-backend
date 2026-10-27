#!/usr/bin/env python3
"""Configure mobile OAuth clients and optional Telegram brokering on private loopback."""
import argparse
import json
from pathlib import Path
import urllib.error
import urllib.parse
import urllib.request


def mobile_profile_mappers():
    return [
        {"name": "account-profile-audience", "protocol": "openid-connect", "protocolMapper": "oidc-audience-mapper",
         "config": {"included.custom.audience": "account", "id.token.claim": "false", "access.token.claim": "true"}},
        {"name": "account-profile-role", "protocol": "openid-connect", "protocolMapper": "oidc-usermodel-client-role-mapper",
         "config": {"usermodel.clientRoleMapping.clientId": "account", "claim.name": "resource_access.account.roles",
                    "multivalued": "true", "jsonType.label": "String", "access.token.claim": "true",
                    "introspection.token.claim": "true", "id.token.claim": "false", "userinfo.token.claim": "false"}},
    ]


def android_client():
    return {
        "clientId": "podhod-android", "enabled": True, "protocol": "openid-connect",
        "publicClient": True, "standardFlowEnabled": True, "implicitFlowEnabled": False,
        "directAccessGrantsEnabled": False, "serviceAccountsEnabled": False,
        "fullScopeAllowed": False, "redirectUris": ["dev.podhod.training://oauth/callback"],
        "webOrigins": [], "attributes": {"pkce.code.challenge.method": "S256", "use.refresh.tokens": "true"},
        "defaultClientScopes": ["profile"],
        "optionalClientScopes": ["schedule:read", "schedule:write", "workout:read", "workout:write", "offline_access"],
        "protocolMappers": [
            {"name": "podhod-api", "protocol": "openid-connect", "protocolMapper": "oidc-audience-mapper",
             "config": {"included.custom.audience": "podhod-api", "id.token.claim": "false", "access.token.claim": "true"}},
            {"name": "subject", "protocol": "openid-connect", "protocolMapper": "oidc-sub-mapper",
             "config": {"access.token.claim": "true", "introspection.token.claim": "true"}},
        ] + mobile_profile_mappers(),
    }


def telegram_provider(settings):
    client_id = str(settings.get("clientID", "")).strip()
    secret = settings.get("clientSecret", "").strip()
    if not client_id.isdecimal() or not secret or any(char.isspace() for char in secret):
        raise ValueError("Telegram Client ID/Client Secret are missing or invalid; use BotFather Web Login settings")
    return {
        "alias": "telegram", "displayName": "Telegram", "providerId": "oidc", "enabled": True,
        "trustEmail": False, "storeToken": False, "addReadTokenRoleOnCreate": False,
        "linkOnly": False, "firstBrokerLoginFlowAlias": "first broker login",
        "config": {
            "issuer": "https://oauth.telegram.org", "authorizationUrl": "https://oauth.telegram.org/auth",
            "tokenUrl": "https://oauth.telegram.org/token", "jwksUrl": "https://oauth.telegram.org/.well-known/jwks.json",
            "clientId": client_id, "clientSecret": secret, "clientAuthMethod": "client_secret_basic",
            "defaultScope": "openid profile", "validateSignature": "true", "useJwksUrl": "true",
            "disableUserInfo": "true", "pkceEnabled": "true", "pkceMethod": "S256",
            "requiresShortStateParameter": "true",
            "syncMode": "IMPORT", "userIDClaim": "sub", "userNameClaim": "sub",
            "hideOnLoginPage": "false", "backchannelSupported": "false",
        },
    }


def telegram_username_mapper():
    return {
        "name": "telegram-stable-username", "identityProviderAlias": "telegram",
        "identityProviderMapper": "oidc-username-idp-mapper",
        "config": {"template": "telegram.${CLAIM.sub}", "target": "LOCAL", "syncMode": "IMPORT"},
    }


def configure_android(call):
    clients = call("/clients?clientId=podhod-android")
    expected = android_client()
    if not clients:
        call("/clients", "POST", expected)
        return
    if len(clients) != 1:
        raise ValueError("Android client identity is ambiguous")
    current = clients[0]
    expected["id"] = current["id"]
    call("/clients/" + current["id"], "PUT", {**current, **expected})


def configure_account_console(call):
    clients = call("/clients?clientId=account-console")
    if len(clients) != 1:
        raise ValueError("Built-in account console identity is missing or ambiguous")
    path = "/clients/" + clients[0]["id"] + "/protocol-mappers/models"
    current = call(path)
    mappers = [
        {"name": "subject", "protocol": "openid-connect", "protocolMapper": "oidc-sub-mapper",
         "config": {"access.token.claim": "true", "introspection.token.claim": "true"}},
        {"name": "account-roles", "protocol": "openid-connect", "protocolMapper": "oidc-usermodel-client-role-mapper",
         "config": {"usermodel.clientRoleMapping.clientId": "account", "claim.name": "resource_access.account.roles",
                    "multivalued": "true", "jsonType.label": "String", "access.token.claim": "true",
                    "introspection.token.claim": "true", "id.token.claim": "false", "userinfo.token.claim": "false"}},
    ]
    for mapper in mappers:
        previous = next((item for item in current if item["name"] == mapper["name"]), None)
        if previous is None:
            call(path, "POST", mapper)
        elif previous["protocolMapper"] != mapper["protocolMapper"]:
            raise ValueError("Account console mapper name belongs to a different mapper type")
        elif any(previous.get("config", {}).get(key) != value for key, value in mapper["config"].items()):
            call(path + "/" + previous["id"], "PUT",
                 {**previous, **mapper, "config": {**previous.get("config", {}), **mapper["config"]}})


def configure_mobile_profiles(call):
    account = call("/clients?clientId=account")
    if len(account) != 1:
        raise ValueError("Built-in account client identity is missing or ambiguous")
    role = call("/clients/" + account[0]["id"] + "/roles/view-profile")
    for client_name in ["podhod-ios", "podhod-android"]:
        clients = call("/clients?clientId=" + client_name)
        if len(clients) != 1 or clients[0].get("fullScopeAllowed") is not False:
            raise ValueError("Mobile profile requires an existing client with restricted role scope")
        root = "/clients/" + clients[0]["id"]
        role_path = root + "/scope-mappings/clients/" + account[0]["id"]
        current_roles = call(role_path)
        if any(item["name"] != "view-profile" for item in current_roles):
            raise ValueError("Mobile account role scope is broader than read-only profile access")
        if not current_roles:
            call(role_path, "POST", [role])
        path = root + "/protocol-mappers/models"
        current = call(path)
        for mapper in mobile_profile_mappers():
            previous = next((item for item in current if item["name"] == mapper["name"]), None)
            if previous is None:
                call(path, "POST", mapper)
            elif previous["protocolMapper"] != mapper["protocolMapper"]:
                raise ValueError("Mobile profile mapper name belongs to a different mapper type")
            elif any(previous.get("config", {}).get(key) != value for key, value in mapper["config"].items()):
                call(path + "/" + previous["id"], "PUT",
                     {**previous, **mapper, "config": {**previous.get("config", {}), **mapper["config"]}})


def configure_telegram(call, settings):
    provider = telegram_provider(settings)
    providers = call("/identity-provider/instances")
    existing = next((item for item in providers if item["alias"] == "telegram"), None)
    if existing is not None and existing.get("providerId") != "oidc":
        raise ValueError("Existing telegram alias belongs to a different provider; refusing to replace it")
    path = "/identity-provider/instances"
    if existing is None:
        call(path, "POST", provider)
    else:
        call(path + "/telegram", "PUT", {**existing, **provider})
    mappers = call(path + "/telegram/mappers")
    mapper = telegram_username_mapper()
    previous = next((item for item in mappers if item["name"] == mapper["name"]), None)
    if previous is None:
        call(path + "/telegram/mappers", "POST", mapper)
    else:
        mapper["id"] = previous["id"]
        call(path + "/telegram/mappers/" + previous["id"], "PUT", mapper)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--telegram-config", type=Path, help="Private JSON with Telegram Web Login clientID/clientSecret")
    args = parser.parse_args()
    settings = None
    if args.telegram_config is not None:
        if args.telegram_config.stat().st_mode & 0o077:
            raise SystemExit("Telegram settings must have mode 600")
        settings = json.loads(args.telegram_config.read_text())
        telegram_provider(settings)
    base = "http://127.0.0.1:8088/auth"
    password = Path("secrets/keycloak_admin_password").read_text().strip()
    payload = urllib.parse.urlencode({"grant_type": "password", "client_id": "admin-cli", "username": "podhod-admin", "password": password}).encode()
    try:
        with urllib.request.urlopen(urllib.request.Request(base + "/realms/master/protocol/openid-connect/token", data=payload), timeout=20) as response:
            token = json.load(response)["access_token"]

        def call(path, method="GET", value=None):
            body = None if value is None else json.dumps(value).encode()
            request = urllib.request.Request(base + "/admin/realms/podhod" + path, data=body, method=method,
                headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"})
            with urllib.request.urlopen(request, timeout=20) as response:
                content = response.read()
                return json.loads(content) if content else None

        configure_android(call)
        configure_account_console(call)
        configure_mobile_profiles(call)
        if settings is not None:
            configure_telegram(call, settings)
    except urllib.error.HTTPError as error:
        raise SystemExit("Identity configuration failed: HTTP " + str(error.code)) from None
    print("Android OAuth client and account console configured; Telegram " + ("configured" if settings is not None else "not changed"))


if __name__ == "__main__":
    main()
