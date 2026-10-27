import importlib.util
from pathlib import Path
import unittest


spec = importlib.util.spec_from_file_location("mobile_identity", Path(__file__).resolve().parents[1] / "configure_mobile_identity.py")
mobile = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mobile)


class MobileIdentityTest(unittest.TestCase):
    def test_android_client_has_own_redirect_subject_and_audience(self):
        client = mobile.android_client()
        self.assertTrue(client["publicClient"])
        self.assertNotIn("secret", client)
        self.assertEqual(["dev.podhod.training://oauth/callback"], client["redirectUris"])
        self.assertEqual("S256", client["attributes"]["pkce.code.challenge.method"])
        self.assertTrue(any(item["protocolMapper"] == "oidc-sub-mapper" for item in client["protocolMappers"]))

    def test_android_configuration_only_changes_android_client(self):
        changes = []
        def call(path, method="GET", value=None):
            if method == "GET":
                self.assertEqual("/clients?clientId=podhod-android", path)
                return []
            changes.append((path, method, value))
        mobile.configure_android(call)
        self.assertEqual(1, len(changes))
        self.assertEqual("podhod-android", changes[0][2]["clientId"])

    def test_telegram_uses_signed_oidc_without_userinfo_or_phone_access(self):
        provider = mobile.telegram_provider({"clientID": "12345", "clientSecret": "test-fixture"})
        config = provider["config"]
        self.assertEqual("https://oauth.telegram.org", config["issuer"])
        self.assertEqual("true", config["validateSignature"])
        self.assertEqual("true", config["useJwksUrl"])
        self.assertEqual("true", config["disableUserInfo"])
        self.assertEqual("client_secret_basic", config["clientAuthMethod"])
        self.assertEqual("S256", config["pkceMethod"])
        self.assertEqual("openid profile", config["defaultScope"])
        self.assertEqual("sub", config["userIDClaim"])
        self.assertFalse(provider["trustEmail"])
        self.assertFalse(provider["storeToken"])
        self.assertEqual("first broker login", provider["firstBrokerLoginFlowAlias"])

    def test_account_console_maps_existing_account_roles_and_subject_without_granting_roles(self):
        changes = []
        def call(path, method="GET", value=None):
            if method == "GET":
                if path == "/clients?clientId=account-console":
                    return [{"id": "console", "fullScopeAllowed": False}]
                self.assertEqual("/clients/console/protocol-mappers/models", path)
                return [{"id": "audience", "name": "audience resolve", "protocolMapper": "oidc-audience-resolve-mapper"}]
            changes.append((path, method, value))
        mobile.configure_account_console(call)
        self.assertEqual(2, len(changes))
        self.assertTrue(all(path == "/clients/console/protocol-mappers/models" and method == "POST" for path, method, _ in changes))
        roles = next(value for _, _, value in changes if value["name"] == "account-roles")
        self.assertEqual("account", roles["config"]["usermodel.clientRoleMapping.clientId"])
        self.assertEqual("resource_access.account.roles", roles["config"]["claim.name"])
        self.assertTrue(any(value["protocolMapper"] == "oidc-sub-mapper" for _, _, value in changes))

    def test_account_console_configuration_is_idempotent(self):
        mappings = []
        def call(path, method="GET", value=None):
            if method == "GET":
                return [{"id": "console"}] if path == "/clients?clientId=account-console" else mappings.copy()
            self.assertEqual("POST", method)
            mappings.append({"id": str(len(mappings)), **value})
        mobile.configure_account_console(call)
        self.assertEqual(2, len(mappings))
        mobile.configure_account_console(call)
        self.assertEqual(2, len(mappings))

    def test_telegram_uses_stable_subject_without_username_account_matching(self):
        mapper = mobile.telegram_username_mapper()
        self.assertEqual("telegram.${CLAIM.sub}", mapper["config"]["template"])
        self.assertEqual("LOCAL", mapper["config"]["target"])

    def test_existing_telegram_provider_is_updated_to_short_state_with_validation_preserved(self):
        changes = []
        def call(path, method="GET", value=None):
            if method == "GET":
                if path == "/identity-provider/instances":
                    return [{"alias": "telegram", "providerId": "oidc", "config": {"requiresShortStateParameter": "false"}}]
                self.assertEqual("/identity-provider/instances/telegram/mappers", path)
                return []
            changes.append((path, method, value))
        mobile.configure_telegram(call, {"clientID": "123", "clientSecret": "fixture"})
        provider = next(value for path, method, value in changes if path == "/identity-provider/instances/telegram")
        self.assertEqual("true", provider["config"]["requiresShortStateParameter"])
        self.assertEqual("true", provider["config"]["validateSignature"])
        self.assertEqual("S256", provider["config"]["pkceMethod"])
        self.assertTrue(all(path.startswith("/identity-provider/instances/telegram") for path, _, _ in changes))

    def test_empty_settings_are_rejected_before_any_change(self):
        for settings in [{}, {"clientID": "name", "clientSecret": "fixture"}, {"clientID": "123", "clientSecret": ""}]:
            with self.assertRaises(ValueError):
                mobile.telegram_provider(settings)

    def test_provider_alias_collision_does_not_replace_different_provider(self):
        changes = []
        def call(path, method="GET", value=None):
            if method == "GET":
                return [{"alias": "telegram", "providerId": "custom"}]
            changes.append(path)
        with self.assertRaises(ValueError):
            mobile.configure_telegram(call, {"clientID": "123", "clientSecret": "fixture"})
        self.assertEqual([], changes)

    def test_telegram_configuration_does_not_mutate_users_or_realms(self):
        changes = []
        def call(path, method="GET", value=None):
            if method == "GET":
                return []
            changes.append((path, value))
        mobile.configure_telegram(call, {"clientID": "123", "clientSecret": "fixture"})
        self.assertEqual(2, len(changes))
        self.assertTrue(all(path.startswith("/identity-provider/instances") for path, _ in changes))


if __name__ == "__main__":
    unittest.main()
