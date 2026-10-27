import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


class AndroidIdentityTest(unittest.TestCase):
    def test_imported_owner_has_standard_self_account_roles(self):
        script = Path(__file__).resolve().parents[1] / "prepare.py"
        with tempfile.TemporaryDirectory() as directory:
            subprocess.run([sys.executable, str(script), directory, "--base", "https://api.example.com"], check=True, capture_output=True)
            realm = json.loads((Path(directory) / "secrets/realm.json").read_text())
            owner = next(user for user in realm["users"] if user["username"] == "owner")
            self.assertEqual({"account": ["manage-account", "view-profile"]}, owner["clientRoles"])

    def test_generated_realm_has_separate_public_android_pkce_client(self):
        script = Path(__file__).resolve().parents[1] / "prepare.py"
        with tempfile.TemporaryDirectory() as directory:
            subprocess.run([sys.executable, str(script), directory, "--base", "https://api.example.com"], check=True, capture_output=True)
            realm = json.loads((Path(directory) / "secrets/realm.json").read_text())
            clients = {client["clientId"]: client for client in realm["clients"]}
            android = clients["podhod-android"]
            self.assertTrue(android["publicClient"])
            self.assertNotIn("secret", android)
            self.assertFalse(android["directAccessGrantsEnabled"])
            self.assertFalse(android["implicitFlowEnabled"])
            self.assertEqual(["dev.podhod.training://oauth/callback"], android["redirectUris"])
            self.assertEqual("S256", android["attributes"]["pkce.code.challenge.method"])
            self.assertEqual(["podhod://oauth/callback"], clients["podhod-ios"]["redirectUris"])
            self.assertIn("offline_access", android["optionalClientScopes"])
            self.assertTrue(any(mapper["config"].get("included.custom.audience") == "podhod-api" for mapper in android["protocolMappers"]))


if __name__ == "__main__":
    unittest.main()
