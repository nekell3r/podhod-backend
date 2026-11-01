# MCP для «Подхода»

Адаптер использует официальный Go MCP SDK и те же операции `service.Service`, что HTTP API приложения. Он не обращается к SQL напрямую и не проверяет JWT самостоятельно. `cmd/podhod` оборачивает `/mcp` в bearer middleware с OIDC-проверкой подписи, issuer, audience и срока действия. Вызов инструмента дополнительно проверяет identity и требуемый scope.

## Подключение ChatGPT

- Endpoint: `https://api.example.com/mcp`.
- Protected resource metadata: `https://api.example.com/.well-known/oauth-protected-resource`.
- OAuth issuer: `https://api.example.com/auth/realms/podhod`.
- OAuth client: `podhod-chatgpt`, authorization code + PKCE S256.
- Audience для MCP: полный URL endpoint `/mcp`. Audience API приложения: `podhod-api`.
- Scopes: `schedule:read`, `schedule:write`, `workout:read`, `workout:write`.

В ChatGPT открой Plugins → «+» → Add custom MCP server, укажи название «Подход» и endpoint. Выбери OAuth и существующий client ID. Секрет клиента передаётся пользователю отдельно; его нельзя сохранять в исходниках, README или логах.

Перед подключением проверь фактическую OIDC discovery metadata и redirect URI, показанный ChatGPT. Текущая официальная документация определяет два режима:

- Если сервер объявляет `authorization_response_iss_parameter_supported: true` и возвращает точный `iss` в успешных и ошибочных authorization callbacks, стабильный redirect URI: `https://chatgpt.com/connector_platform_oauth_redirect`.
- Иначе ChatGPT показывает URI с идентификатором подключения: `https://chatgpt.com/connector/oauth/{callback_id}`.

В Keycloak allowlist должен стоять только точный отображённый URI, без wildcard. После подключения авторизуйся своим аккаунтом «Подхода», проверь обнаруженные инструменты и начни новый чат с выбранным плагином. Доступность пользовательских MCP-подключений зависит от политики аккаунта/рабочего пространства.

Источники: [аутентификация OpenAI](https://developers.openai.com/plugins/build/auth), [подключение и тестирование в ChatGPT](https://developers.openai.com/plugins/deploy/connect-chatgpt), [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk).

## Инструменты

| Инструмент | Scope | Назначение |
| --- | --- | --- |
| `get_profile` | Авторизованная identity | Стабильный непрозрачный ID подключённого пользователя |
| `get_training_template` | `schedule:read` | Повторяемый шаблон недели |
| `replace_training_template` | `schedule:write` | Замена шаблона без изменения сохранённых датированных недель |
| `get_training_week` | `schedule:read` | Чтение конкретной недели без записи |
| `replace_training_week` | `schedule:write` | Замена только указанной недели |
| `move_scheduled_workout` | `schedule:write` | Перенос внутри выбранной недели; занятый день даёт конфликт |
| `update_planned_exercise_rest` | `schedule:write` | Отдых в конкретной запланированной тренировке |
| `get_active_workout` | `workout:read` | Последний серверный snapshot активной тренировки |
| `update_active_exercise_rest` | `workout:write` | Отдых в активной тренировке; запущенный таймер сохраняет deadline |
| `skip_active_exercise` | `workout:write` | Пропуск упражнения с сохранением записанных подходов |
| `unskip_active_exercise` | `workout:write` | Отмена пропуска |
| `get_workout_history` | `workout:read` | Последние завершённые тренировки; limit 1–100, по умолчанию 20 |
| `get_nutrition_catalog` | `workout:read` | Продукты и общие рецепты с КБЖУ, точными ID, состоянием приготовления и источниками |
| `get_nutrition_recipes` | `workout:read` | Свои рецепты подключённого аккаунта и revision коллекции |
| `create_nutrition_recipe` | `workout:write` | Добавление одного своего рецепта без изменения остальных |
| `delete_nutrition_recipe` | `workout:write` | Удаление одного своего рецепта по UUID; снимки дневника остаются |
| `get_nutrition_day` | `workout:read` | Конкретный день, сохранённая цель/timezone, итоги КБЖУ и revision |
| `get_nutrition_goals` | `workout:read` | История ручных целей для первого снимка дня |
| `log_nutrition_entry` | `workout:write` | Добавление одной съеденной позиции без замены других записей дня |

Identity берётся только из проверенного `auth.TokenInfo.UserID`. В инструментах нет аргументов для выбора пользователя. Входные JSON schemas закрытые, включая `get_profile`. Профиль возвращает только `{ "id": "…" }`, помечен `_meta["openai/profile"]: true` и не добавляет выдуманные display-поля.

Верхний уровень аргументов MCP использует snake_case. Вложенные объекты программы используют общий camelCase JSON-контракт `service/model.go`, совпадающий с iOS API. Например, `between_sets_seconds` и `after_exercise_seconds` адаптируются к `restSeconds` и `restAfterExerciseSeconds`.

Каждая запись требует `expected_revision` и UUID `request_id`. При повторе точно того же действия используй прежний `request_id`; при новом действии — новый. При `revision_conflict` результат содержит `error.current_revision`; нужно прочитать свежий ресурс и согласовать изменения. При `idempotency_conflict` ID ранее использован для другого запроса; текущая revision семантически не определяется.

Команды для активной тренировки дополнительно требуют конкретные `session_id` и `exercise_id`. Это идентификатор упражнения внутри сессии, а не идентификатор упражнения в каталоге. Пример структуры команды пропуска:

```json
{
  "session_id": "30000000-0000-4000-8000-000000000001",
  "exercise_id": "20000000-0000-4000-8000-000000000001",
  "expected_revision": 12,
  "request_id": "60000000-0000-4000-8000-000000000001"
}
```

Подставляй реальные идентификаторы и revision из последнего `get_active_workout`.

## Рецепты и дневник питания

Питание использует прежние `workout:read/write` по выбору пользователя. Новые роли Keycloak, разрешения мобильных клиентов и SQL-схема не требуются. Инструменты не принимают owner/user ID: изменения относятся только к проверенной identity подключения.

Перед созданием или удалением рецепта прочитай `get_nutrition_recipes` и используй revision всей коллекции. Создание принимает один `recipe` по общему camelCase-контракту `NutritionOwnRecipe`: UUID, название, category (`breakfast/main/salad/snack`), minutes, servings, preparedWeightGrams, ingredients и steps. У ингредиента собственный UUID, grams и food snapshot. Нужны фактическая готовая масса и явные КБЖУ на 100 г: отсутствие значения не означает ноль. Общий каталог читается отдельно и не меняется этими командами.

`delete_nutrition_recipe` требует точный `recipe_id` своего рецепта. Удаление помечено destructive; создание и добавление еды — additive. Удаление не меняет другие рецепты, дневник, цели и избранное. Неизвестный UUID даёт `not_found`, занятый UUID при создании — `recipe_exists`.

Чтобы записать еду:

1. Определи дату, IANA timezone, приём (`breakfast/lunch/dinner/snack`) и съеденную массу в граммах. Не выдумывай недостающие количества или КБЖУ; используй каталог, рассчитанный состав рецепта или явные значения с этикетки. Сырой и готовый продукт различаются.
2. Прочитай `get_nutrition_day`. Чтение отсутствующего дня возвращает revision 0, но не фиксирует цель/timezone и ничего не записывает.
3. Для revision 0 прочитай `get_nutrition_goals`: `initial_goal` — снимок с самой поздней effectiveFrom, не позже дня, либо явный null при отсутствии цели. У существующего дня `initial_goal` игнорируется; сохранённая цель, включая её отсутствие, остаётся прежней. Передай его сохранённый timezone.
4. Вызови `log_nutrition_entry` с новой записью `entry` (UUID, meal, grams, food), `expected_revision` и UUID `request_id`. Возвращаются день, новая revision и рассчитанные сервером totals. Первоначальные снимки состава остаются после изменения или удаления рецепта.

Повтор идентичного `request_id` не добавляет еду ещё раз. После неизвестного результата повтори тот же запрос; не создавай новый UUID запроса или записи вслепую. После `revision_conflict` прочитай ресурс и сверяй записи до нового действия. Receipt старого создания после удаления возвращает исторический ответ, но не восстанавливает рецепт и не означает актуальную revision. Мобильные изменения могут оставаться несинхронизированными; `pending_sync` не подтверждает применение на iPhone.

## Синхронизация и ошибки

Запись активной тренировки возвращает `delivery_status: "pending_sync"`: обновлено состояние сервера, но применение на iPhone ещё не подтверждено. Офлайн-телефон получит изменение при следующей успешной синхронизации. Ответ инструмента не обещает немедленного переключения экрана.

Пропуск не удаляет завершённые или черновые подходы и не помечает незавершённые подходы выполненными. Изменение программы не переписывает историю или snapshot уже начатой тренировки. Новые параметры отдыха не меняют deadline уже запущенного таймера.

Циклы и новые контексты/суперсеты описаны в `docs/TRAINING-CYCLES-CONTRACT.ru.md`. Чтение и узкие команды отдыха/переноса/пропуска сохраняют их. Текущие полные MCP-записи шаблона и недели не передают `structureVersion:1`; если ресурс уже использует новую структуру, сервер отклоняет такую запись с `upgrade_required`. Не обходи защиту обнулением данных: редактирование структуры выполняется через поддерживающий её клиент/REST. Пропуск закрывает суперсет, когда остаётся меньше двух непропущенных участников, сохраняет выполненные связи и черновики; снятие пропуска не открывает группу автоматически. Отдельных MCP-инструментов для управления циклом этот выпуск не добавляет.

Доменные ошибки возвращаются с `isError: true` и структурированным `error`. Внутренние ошибки дают обезличенное сообщение, а технические детали пишутся на сервере. Недостаточная авторизация возвращает `_meta["mcp/www_authenticate"]`; токены и приватные результаты в сообщение об ошибке не включаются.

## Проверка

Из корня `server/`:

```sh
go test -race ./internal/mcp
```

Тесты проходят через настоящий Streamable HTTP transport SDK, bearer middleware с тестовым verifier, concrete `service.Service` и изолированный in-memory repository. Они проверяют контракт инструментов, identity и scopes, изоляцию пользователей, ревизии и повторы, занятые даты, отдых, сохранение подходов при skip/unskip и неизменность active snapshot при смене шаблона. PostgreSQL и подпись настоящих JWT проверяются отдельными тестами соответствующих пакетов.

## Timing measurement

Optional workout timing is recorded by the iPhone from exercise-screen selection, rest timers and other elapsed UI time. Physical set start/end times are not measured. An absent timing ledger in old history is unknown and must not be reconstructed from configured rest durations.

MCP commands preserve timing fields unchanged and remain pending until phone synchronization. They do not end, start or advance intervals using server time. The phone settles its old observation context before applying a downloaded command. Open rest intervals carry their own captured `restDeadline` so a pending skip can clear the operational timer without deleting the observation boundary. Completed history remains immutable.
