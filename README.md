# Team Task Service

REST API для управления задачами внутри команд. Сервис реализует JWT-аутентификацию, роли `owner/admin/member`, изоляцию данных команд, историю изменений, комментарии, оптимистическую блокировку, Redis-кеш списка задач и SQL-аналитику.

## Запуск

Требуются Docker и Docker Compose.

```bash
cp .env.example .env
docker compose up --build
```

API: `http://localhost:8080`, OpenAPI: `http://localhost:8080/openapi.yaml`, health-check: `GET /health`.

## Конфигурация

| Переменная | Назначение | Значение по умолчанию |
| --- | --- | --- |
| `HTTP_ADDR` | адрес HTTP-сервера | `:8080` |
| `MYSQL_DSN` | MySQL DSN | DSN контейнера Compose |
| `REDIS_ADDR` | адрес Redis | `redis:6379` |
| `REDIS_PASSWORD` | пароль Redis | пусто |
| `JWT_SECRET` | ключ подписи JWT | обязателен |

Миграция выполняется идемпотентно при старте приложения. Полная схема находится в `cmd/api/migrations/001_init.sql`.

## Пример работы

```bash
curl -X POST localhost:8080/api/v1/register -H 'Content-Type: application/json' \
  -d '{"email":"owner@example.com","password":"password123","name":"Owner"}'

TOKEN=$(curl -s -X POST localhost:8080/api/v1/login -H 'Content-Type: application/json' \
  -d '{"email":"owner@example.com","password":"password123"}' | jq -r .token)

curl -X POST localhost:8080/api/v1/teams -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"Platform"}'

curl -X POST localhost:8080/api/v1/tasks -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"team_id":1,"title":"Prepare release","description":"Release checklist"}'

curl 'localhost:8080/api/v1/tasks?team_id=1&status=todo&limit=20&offset=0' \
  -H "Authorization: Bearer $TOKEN"

curl localhost:8080/api/v1/teams/1/stats -H "Authorization: Bearer $TOKEN"
```

При обновлении задачи клиент передает текущее поле `version`. SQL-условие по версии и `SELECT ... FOR UPDATE` не дают двум конкурентным запросам незаметно перезаписать изменения друг друга.

Ключ кеша содержит `team_id`, фильтры, пагинацию и версию кеша. После изменения задачи версия команды увеличивается, поэтому устаревшие результаты больше не читаются. TTL кеша - 5 минут.

## Проверки

```bash
make test
make test-integration
```

Интеграционный тест поднимает MySQL, создает данные и проверяет SQL-отчет: статусы, топ исполнителей, среднее время закрытия и комментарии.
