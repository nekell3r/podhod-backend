# Самостоятельное развёртывание

Все домены в примерах фиктивные. Нужны собственный HTTPS-домен, Docker Compose и настроенный TLS-прокси. Подготовка исходников не развёртывает сервер и не создаёт сертификат автоматически.

Ожидаемая структура на сервере:

```text
/opt/podhod/
  app/          checkout podhod-backend
  compose.yaml  копия app/deploy/compose.yaml
  .env          создаёт prepare.py
  secrets/      создаёт prepare.py
  backups/      закрытые резервные копии
  releases/     только публичные артефакты приложения
```

Подготовка новых локальных настроек:

```sh
python3 app/deploy/prepare.py /opt/podhod --base https://api.example.com
cp app/deploy/compose.yaml /opt/podhod/compose.yaml
```

Генератор создаёт случайные пароли PostgreSQL/Keycloak, первоначальную учётную запись и секрет confidential MCP-клиента. `access.json`, `.env`, realm import и каталог `secrets` не должны попадать в Git или публичные релизы. Повторный запуск поверх существующих секретов запрещён.

Перед запуском настрой владельцев файлов bind mounts: `init_app_db_password` и `init_keycloak_db_password` — UID 999; секреты Keycloak и realm import — UID 1000; `database_url` — UID 10001. Файлы — mode 600, каталог секретов — mode 700. Значения должны соответствовать пользователям выбранных контейнерных образов.

```sh
cd /opt/podhod
docker compose up -d --build
python3 app/deploy/configure_identity.py
```

Keycloak работает с контекстом `/auth`; PUBLIC_BASE_URL должен совпадать с внешним HTTPS origin. `configure_identity.py` обращается только к loopback admin endpoint и не печатает credentials. Регистрация по умолчанию закрыта; восстановление пароля по почте не настроено.

Для Android обнови API и выполни `python3 app/deploy/configure_mobile_identity.py` из `/opt/podhod`. Скрипт создаёт public client `podhod-android` с точным redirect `dev.podhod.training://oauth/callback`, PKCE S256 и audience `podhod-api`; iOS сохраняет прежний redirect. Проверка: `/v1/config?platform=android` возвращает `clientID: podhod-android`, `/v1/config` — `podhod-ios`. Повторно запускать `prepare.py` на действующих секретах нельзя.

Этот же шаг нужен для отображения профиля в обоих клиентах: он добавляет им audience `account` и ограниченный role scope `account:view-profile` для чтения собственного Account API Keycloak. Mappers не назначают новые роли пользователям и не разрешают изменение профиля или связей. При уже расширенном role scope клиента команда останавливается; существующие права нельзя молча расширять или удалять.

Telegram Web Login подключается командой `python3 app/deploy/configure_mobile_identity.py --telegram-config secrets/telegram-login.json`. Файл mode 600 содержит `clientID` и `clientSecret` из BotFather Web Login, не Bot API Token. В BotFather → Login Widget → Redirect URIs → Add a Redirect URI сохрани `https://api.example.com/auth/realms/podhod/broker/telegram/endpoint` со своим origin; Signing Algorithm оставь RS256. Команда настраивает Android-клиент, необходимые claims Account Console, Telegram-провайдер и его mapper; не меняет существующих пользователей или режим обычной регистрации. Проверяй `sub`, audience `account`, собственные роли в токене кабинета и HTTP 200 для Account API профиля и привязок. Для старого `owner` отдельно проверь стандартные собственные роли клиента `account`. Политика входа и привязки — [IDENTITY.md](IDENTITY.md).

Адаптируй домен и пути TLS-сертификатов в `deploy/nginx.conf`, затем установи конфигурацию в собственный Nginx и проверь её командой `nginx -t`. Пример закрывает публичный доступ к Keycloak admin/master, проксирует REST/MCP, сохраняет proxy headers и отключает буферизацию MCP. Контейнерные порты API и Keycloak привязаны только к loopback; PostgreSQL не публикуется наружу.

`deploy/backup.sh` создаёт закрытые дампы обеих БД в `/opt/podhod/backups` и проверяет архивы. Запуск и расписание резервного копирования настраивает оператор. Нужна отдельная копия за пределами сервера; скрипт сам её не создаёт. В `releases` допустимы только файлы приложения, предназначенные для скачивания, без сертификатов, профилей, дампов или настроек.
