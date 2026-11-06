# proview-banner-service

Сервис выдачи рекламного баннера платформы **ProView**. Выбирает креатив из пула по преднастройке рекламного места (placement), таргетингу (страна, устройство, период показа) и весу. Выдаёт подписанный токен показа, по которому сборщик событий принимает статистику.

Часть платформы: [документация](https://github.com/DemonIngvi/proview-docs) · [развёртывание](https://github.com/DemonIngvi/proview-deploy).

## Как работает

```
Postgres (пул, источник истины) ──раз в 10 с──▶ снимок в памяти (atomic swap) ──▶ GET /v1/banner
```

- Горячий путь **не ходит в БД**: выбор идёт по неизменяемому снимку пула в памяти, около 230 нс на выбор и 0 аллокаций.
- Если Postgres недоступен, сервис продолжает отдавать последний удачный снимок. Readiness выставляется только после первой загрузки.
- Выбор взвешенный случайный среди подходящих баннеров. Пустой список таргетинга означает «без ограничений».
- Токен: `base64url(payload).base64url(HMAC-SHA256[:16])`, payload `1|kid|impression_id|banner_id|placement|issued_unix`. Сборщик проверяет его без общей БД. Контрольный вектор зафиксирован тестом в обоих сервисах.

## API

### Публичный, порт 8080

`GET /v1/banner?placement=home_top`. Страна берётся из заголовка `COUNTRY_HEADER` (от CDN или ingress), устройство — из `User-Agent`.

| Код | Когда |
|---|---|
| 200 | `{"impression_id","token","placement","banner":{"id","image_url","click_url","alt","width","height"}}` |
| 204 | нет подходящего баннера (no fill) |
| 400 / 404 | неверный или неизвестный placement |
| 503 | пул ещё не загружен (`Retry-After: 1`) |

### Управление пулом, порт 8081

Порт внутренний, наружу не публикуется. Каждый запрос требует `Authorization: Bearer $ADMIN_TOKEN`.

| Метод | Путь | |
|---|---|---|
| GET | `/admin/v1/placements` | список мест |
| PUT | `/admin/v1/placements/{id}` | создать или изменить место (`width`, `height`, `description`) |
| GET | `/admin/v1/banners` | все баннеры, включая неактивные |
| GET | `/admin/v1/banners/{id}` | один баннер |
| POST | `/admin/v1/banners` | создать баннер |
| PUT | `/admin/v1/banners/{id}` | заменить поля |
| DELETE | `/admin/v1/banners/{id}` | деактивировать (мягкое удаление, история в аналитике сохраняется) |

```bash
curl -H "Authorization: Bearer $ADMIN_TOKEN" -d '{"name":"Осень","image_url":"https://cdn.example.com/a.png",
  "click_url":"https://example.com","width":300,"height":250,"weight":200,"placements":["sidebar"],
  "countries":["RU","KZ"],"devices":["mobile"],"ends_at":"2026-12-31T23:59:59Z"}' localhost:8081/admin/v1/banners
```

После записи экземпляр перезагружает свой снимок сразу. Остальные реплики подхватывают изменение в течение `REFRESH_INTERVAL`.

### Служебный, порт 9090

`/healthz` (liveness), `/readyz` (readiness), `/metrics` (Prometheus).

## Конфигурация

| Переменная | По умолчанию | |
|---|---|---|
| `POSTGRES_DSN` | — (обязательна) | строка подключения pgx |
| `TOKEN_SECRET` | — (≥ 32 символа) | ключ подписи токенов |
| `TOKEN_KEY_ID` | `k1` | идентификатор ключа (для ротации) |
| `ADMIN_TOKEN` | пусто = API управления закрыт | bearer-токен |
| `REFRESH_INTERVAL` | `10s` | период обновления снимка (+ джиттер до 20 %) |
| `COUNTRY_HEADER` | `X-Country` | например `CF-IPCountry` |
| `CORS_ALLOWED_ORIGINS` | `*` | список через запятую |
| `HTTP_ADDR` / `ADMIN_ADDR` / `OPS_ADDR` | `:8080` / `:8081` / `:9090` | |
| `SHUTDOWN_DELAY` / `SHUTDOWN_TIMEOUT` | `5s` / `15s` | вывод из балансировки и дренаж при остановке |
| `LOG_LEVEL` | `info` | JSON-логи в stdout |

Миграции: `banner-service migrate` (в Helm запускается Job-хуком). Нужен только `POSTGRES_DSN`, миграции защищены advisory lock.

## Метрики

`proview_banner_selections_total{placement,result}` (filled / no_fill / unknown_placement), `proview_banner_pool_banners`, `proview_banner_pool_loaded_timestamp_seconds` (алерт на устаревший снимок), `proview_banner_pool_reloads_total{result}`, `proview_banner_http_request_duration_seconds`, а также метрики Go runtime.

## Разработка

```bash
make test                # unit-тесты с -race
PROVIEW_TEST_PG_DSN=postgres://postgres:pw@localhost:5432/postgres?sslmode=disable make test-integration
make lint                # golangci-lint v2
make docker              # distroless/static, nonroot, ~24 МБ
make helm-lint
go test -bench . ./internal/banner ./internal/httpapi
```

Структура: `cmd/banner-service`, `internal/banner` (модель и алгоритм выбора, без I/O), `internal/pool` (снимок и обновление), `internal/store` (Postgres и миграции), `internal/httpapi`, `internal/token`, `internal/platform` (логи, метрики, graceful shutdown). Чарт лежит в `charts/proview-banner-service`.

CI: lint → unit и интеграционные тесты (Postgres) → helm lint и kubeconform → multi-arch образ в GHCR с SBOM и provenance, проверка Trivy → по тегу `v*` публикация чарта в `oci://ghcr.io/demoningvi/charts`.

Лицензия: Apache-2.0.
