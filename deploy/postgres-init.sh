#!/bin/bash
set -euo pipefail
psql --username "${POSTGRES_USER:-postgres}" --dbname postgres \
  --set=app_password="$(</run/secrets/init_app_db_password)" \
  --set=keycloak_password="$(</run/secrets/init_keycloak_db_password)" <<'SQL'
CREATE ROLE podhod LOGIN PASSWORD :'app_password';
CREATE DATABASE podhod OWNER podhod;
CREATE ROLE keycloak LOGIN PASSWORD :'keycloak_password';
CREATE DATABASE keycloak OWNER keycloak;
SQL
