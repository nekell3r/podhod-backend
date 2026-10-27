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

## Синхронизация и ошибки

Запись активной тренировки возвращает `delivery_status: "pending_sync"`: обновлено состояние сервера, но применение на iPhone ещё не подтверждено. Офлайн-телефон получит изменение при следующей успешной синхронизации. Ответ инструмента не обещает немедленного переключения экрана.

Пропуск не удаляет завершённые или черновые подходы и не помечает незавершённые подходы выполненными. Изменение программы не переписывает историю или snapshot уже начатой тренировки. Новые параметры отдыха не меняют deadline уже запущенного таймера.

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
