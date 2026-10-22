<div align="center">

<img src="web/public/favicon.svg" width="72" alt="">

# Remna Control

**Админка VPN-сервиса поверх панели [Remnawave](https://github.com/remnawave/panel)**

Клиенты, подписки, платежи с автопродлением, тарифы, аддоны, рефералы и расходы на инфраструктуру — в одном месте и в синхронизации с панелью.

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](go.mod)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black)](web/package.json)
[![Mantine](https://img.shields.io/badge/Mantine-9-339AF0?logo=mantine&logoColor=white)](web/package.json)
[![SQLite](https://img.shields.io/badge/SQLite-без%20CGO-003B57?logo=sqlite&logoColor=white)](internal/store)
[![License: AGPL-3.0](https://img.shields.io/badge/License-AGPL--3.0-blue)](LICENSE)
[![Release](https://img.shields.io/github/v/release/lentryd/remna-control)](https://github.com/lentryd/remna-control/releases)
[![Test](https://github.com/lentryd/remna-control/actions/workflows/test.yaml/badge.svg)](https://github.com/lentryd/remna-control/actions/workflows/test.yaml)

[English](README.md) · **Русский**

<img src="docs/screenshots/ru/dashboard.webp" alt="Главная: MRR, расходы, прибыль, доходы и расходы по месяцам, трафик пользователей" width="100%">

</div>

## Зачем

Remnawave хорошо управляет пользователями VPN, но не ведёт учёт денег. Кто сколько заплатил, до какого числа, кто должен, кто кого привёл и сколько стоят серверы — обычно всё это живёт в таблице. Remna Control ведёт этот учёт и сам отправляет результат в панель: записанный платёж продлевает нужных пользователей, а смена тарифа меняет их лимиты и сквады.

## Возможности

- **Клиенты и подписки.** У клиента-плательщика может быть несколько подписок (пользователей Remnawave), у каждой подписки — аддоны.
- **Сроки на виду.** Дашборд, фильтры по сроку, статус и трафик из панели показывают, у кого и что заканчивается.
- **Платежи с автопродлением.** Платёж зачисляется на баланс клиента. Предпросмотр показывает, сколько месяцев и на какие подписки и аддоны он покрывает. После подтверждения новые `expireAt` уходят в панель, остаток остаётся на балансе.
- **Тарифы.** Базовые тарифы и тарифы аддонов: цена, скидки за срок (3/6/12 месяцев, пробные дни), лимит трафика, стратегия сброса, HWID, сквады. Подписке можно задать индивидуальную цену. Смена тарифа считает доплату за остаток срока.
- **Аддоны.** Аддон — отдельный пользователь панели `prefix + <username> + suffix`, чьи конфиги добавляются в основную подписку (через [subpage](#аддоны-и-api) или похожий сервис). Аддоны можно вести в интерфейсе или загружать из файла, а другие сервисы получают каталог по API.
- **Тарифы со включёнными аддонами.** В базовый тариф можно включить аддоны. Они подключаются бесплатно и продлеваются, включаются и отключаются вместе с подпиской.
- **Рефералы.** Дерево «кто кого привёл» и учётное начисление X% от платежей приглашённых. Баланс начисления не меняют.
- **Траты в любой валюте.** Сумма переводится в базовую валюту по курсу на дату (ЦБ РФ, ЕЦБ). Можно добавить комиссию банка и долю. Сумма фиксируется при записи, возвраты хранятся отдельно.
- **Тарификация по трафику.** Для CDN и транзита с оплатой за ГБ: стоимость по трафику ноды, прогноз на конец периода и список тех, кто больше всего грузит. См. [Тарификация по трафику](#тарификация-по-трафику).
- **Резервные копии.** Экспорт и импорт по категориям, снапшоты по расписанию, выдача снапшотов по API.
- **Интерфейс на русском и английском.** Устанавливается как PWA. Без сети показывает закэшированные данные, а изменения блокирует.

## Скриншоты

<table>
  <tr>
    <td width="50%"><img src="docs/screenshots/ru/customer.webp" alt="Карточка клиента"><br><sub><b>Клиент:</b> баланс, подписки, аддоны, трафик и история платежей</sub></td>
    <td width="50%"><img src="docs/screenshots/ru/payment.webp" alt="Окно платежа"><br><sub><b>Платёж:</b> предпросмотр — что и до какого числа продлится</sub></td>
  </tr>
  <tr>
    <td><img src="docs/screenshots/ru/subscriptions.webp" alt="Подписки"><br><sub><b>Подписки:</b> статус, сроки и трафик из панели</sub></td>
    <td><img src="docs/screenshots/ru/customers.webp" alt="Клиенты"><br><sub><b>Клиенты:</b> ближайшее истечение, доход в месяц, баланс и долги</sub></td>
  </tr>
  <tr>
    <td><img src="docs/screenshots/ru/tariffs.webp" alt="Тарифы и аддоны"><br><sub><b>Тарифы:</b> скидки за срок, лимиты, сквады и включённые аддоны</sub></td>
    <td><img src="docs/screenshots/ru/expense-items.webp" alt="Статьи расходов"><br><sub><b>Статьи расходов:</b> фиксированные траты и оплата за ГБ в любой валюте</sub></td>
  </tr>
  <tr>
    <td colspan="2"><img src="docs/screenshots/ru/referrals.webp" alt="Дерево рефералов"><br><sub><b>Рефералы:</b> кто кого привёл и сколько приносит</sub></td>
  </tr>
</table>

<sub>На скриншотах вымышленные демо-данные.</sub>

## Быстрый старт

Нужны панель Remnawave и Docker. Remna Control работает отдельным контейнером в docker-сети панели. Готовый образ публикуется в `ghcr.io/lentryd/vpn-control` для `linux/amd64` и `linux/arm64`, поэтому клонировать и собирать ничего не нужно.

1. Создайте в панели API-токен (**API tokens**) со scope `users:*`, `internal-squads:read`, `nodes:read`, `bandwidth-stats:read`, `infra-billing:read` или просто `*`.
2. На сервере создайте папку и скачайте в неё пример [`compose.yml`](compose.yml) и [`.env.example`](.env.example):

   ```bash
   mkdir remna-control && cd remna-control
   curl -fsSL -o compose.yml https://raw.githubusercontent.com/lentryd/remna-control/main/compose.yml
   curl -fsSL -o .env https://raw.githubusercontent.com/lentryd/remna-control/main/.env.example
   ```

3. Заполните `.env`: `REMNAWAVE_TOKEN`, `APP_DOMAIN` и `JWT_SECRET` (`openssl rand -hex 32`). Сверьте `REMNAWAVE_NETWORK`, `TRAEFIK_CERTRESOLVER` и `TRAEFIK_ENTRYPOINTS` с настройками своей панели.
4. Запустите:

   ```bash
   docker compose up -d
   ```

5. Откройте `https://$APP_DOMAIN/` и войдите логином и паролем администратора панели.

`compose.yml` — только пример. Подстройте его под свой сервер или перенесите сервис в compose-файл самой панели.

### Обновление

```bash
docker compose pull && docker compose up -d
```

Данные хранятся в volume `vpn-control-data` и переживают обновления. Миграции применяются при старте, снапшоты делаются по расписанию (см. [Резервные копии](#резервные-копии)). Чтобы зафиксировать версию, укажите вместо `latest` тег релиза, например `image: ghcr.io/lentryd/vpn-control:1.0.0`. Список версий — на странице [Releases](https://github.com/lentryd/remna-control/releases).

### Детали развёртывания

- Контейнер подключается к docker-сети панели (`REMNAWAVE_NETWORK`, по умолчанию `remnawave-network`) и обращается к бэкенду напрямую: `http://remnawave:3000`. Заголовки `X-Forwarded-*`, которые требует панель, сервис выставляет сам.
- Лейблы Traefik отдают приложение по адресу `https://$APP_DOMAIN/`. Ему нужен собственный домен, например поддомен панели.
- **Без Traefik:** приложение слушает `HOST_PORT`, перед ним можно поставить любой обратный прокси, а `labels` убрать. Работайте по HTTPS или задайте `SECURE_COOKIE=false` для локального запуска по HTTP.
- **Traefik на другом хосте через frp:** пробросьте `HOST_PORT` на сторону Traefik и укажите в лейбле сервиса проброшенный порт. Например, для [frp](https://github.com/fatedier/frp) с docker-лейблами:

  ```yaml
  labels:
      # ...лейблы роутера выше, затем:
      - 'traefik.http.services.vpn-control.loadbalancer.server.port=1${HOST_PORT:-8080}'
      - 'frp.enable=true'
      - 'frp.name=vpn-control'
      - 'frp.local_port=${HOST_PORT:-8080}'
      - 'frp.remote_port=1${HOST_PORT:-8080}'
  ```

- **Вход.** Администраторы входят с учётными данными панели: сервис проверяет их через `POST /api/auth/login` и выдаёт свою сессию. Если в панели вход только через OAuth или passkey, задайте `ADMIN_PASSWORD` для локального входа.
- **Файл аддонов (по желанию).** Положите `addons.yml` рядом с `compose.yml` или укажите в `ADDONS_FILE` путь к другому, например к файлу subpage. Без него аддоны ведутся в интерфейсе.

### Вебхуки (по желанию)

Вебхуки мгновенно обновляют статусы. В `.env` панели укажите:

- `WEBHOOK_ENABLED=true`;
- `WEBHOOK_URL=https://<APP_DOMAIN>/api/webhooks/remnawave`. Панель принимает только `https://`, поэтому вебхук идёт через её Traefik;
- тот же секрет в `WEBHOOK_SECRET_HEADER` панели и в `WEBHOOK_SECRET` здесь.

Без вебхуков данные синхронизируются раз в `SYNC_INTERVAL` (по умолчанию 10 минут) и по кнопке ⟳.

## Настройка

Всё задаётся переменными окружения, полный список — в [`.env.example`](.env.example). Основные:

| Переменная | По умолчанию | Назначение |
|---|---|---|
| `REMNAWAVE_URL`, `REMNAWAVE_TOKEN` | — | Бэкенд панели и API-токен (обязательно) |
| `JWT_SECRET` | — | Подпись сессий администратора (обязательно) |
| `APP_DOMAIN` | — | Домен, на котором Traefik отдаёт приложение |
| `ADMIN_USERNAME`, `ADMIN_PASSWORD` | `admin`, пусто | Запасной локальный вход; пустой пароль его отключает |
| `TZ` | `UTC` | Часовой пояс для отображения дат и границ периодов |
| `SYNC_INTERVAL` | `10m` | Синхронизация пользователей панели |
| `TRAFFIC_SYNC_INTERVAL` | `1h` | Синхронизация трафика нод для статей по трафику |
| `PANEL_SYNC_INTERVAL` | `5m` | Передача параметров тарифа пользователям панели и сбросы трафика «по подписке» |
| `ADDONS_FILE` / `ADDONS_CONFIG` | `./addons.yml` | Необязательный файл аддонов (путь на хосте / внутри приложения) |
| `DB_PATH` | `./data/vpn-control.db` | База SQLite |

В интерфейсе (**Настройки**) задаются:

- **Базовая валюта.** В ней ведутся платежи, балансы и итоги трат. Курсы берутся из ЦБ РФ для RUB и из ЕЦБ для EUR, USD, GBP, CHF, PLN и других. Сменить её можно, только пока в учёте нет платежей и трат.
- **Реферальный процент**, **окно «скоро истекает»** и **комиссия за валюту по умолчанию**.
- **Расписание бэкапов**: как часто делать снапшоты и сколько хранить.

## Тарификация по трафику

Статья «по трафику» берёт трафик ноды панели (по желанию — только участников одного внутреннего сквада на ней) и считает его так, как выставляет счёт провайдер:

- **Единица ГБ:** двоичная (1 ГБ = 1024³ байт) или десятичная (10⁹ байт).
- **Минимум:** либо *минимальный платёж* — стоимость = `max(минимум, ГБ × цена)`, либо *абонплата + бесплатный объём* — стоимость = `абонплата + (ГБ − бесплатные ГБ) × цена`.
- **Цена:** одна за ГБ или ступенчатая («до 10 000 ГБ по X, дальше по Y»).
- **День начала периода:** 1–28, если расчётный период начинается не с первого числа.

Примеры:

| Провайдер | Единица ГБ | Минимум | Цена |
|---|---|---|---|
| Yandex Cloud CDN | двоичная | минимальный платёж | одна за ГБ |
| Bunny, Gcore | десятичная | минимальный платёж (или нет) | ступени по объёму |
| Тарифы с включённым трафиком | десятичная | абонплата + бесплатный объём | превышение за ГБ |

**Закрыть период** записывает рассчитанную стоимость как трату. Когда придёт счёт, исправьте сумму на выставленную — расчёт сохранится для сравнения.

## Аддоны и API

Аддоны берутся из двух источников:

- **Интерфейс** (**Тарифы → Аддоны**): название, префикс и суффикс, а также названия конфигов для subpage (`remark`, `remarkUnlimited`) и заглушки.
- **Файл аддонов** в формате `addons.yml` сервиса subpage (`ADDONS_FILE`). Аддоны из файла в интерфейсе только для чтения.

Базовый тариф может **включать аддоны** — выберите у тарифа их тарифы аддонов. При изменении списка его можно сразу применить к текущим подписчикам, иначе он применится при следующей смене тарифа. Если подписка переходит на тариф без какого-то из включённых аддонов, вы выбираете: отключить его или оставить платным.

Публичный API покрывает всё, что умеет интерфейс: клиенты, платежи и баланс, подписки и аддоны, продления и смена тарифа, тарифы, рефералы, расходы, данные Remnawave и статистика — на его основе можно делать ботов и другие интеграции. Создайте токен в **Настройки → API-токены** — как в панели: название, срок действия в днях и права по ресурсам (Read/Write или отдельные эндпоинты; пресеты: только чтение, полный доступ, subpage). Токен показывается один раз, хранится только его хеш. Затем обращайтесь к API:

```bash
curl -H "Authorization: Bearer vpc_…" https://control.example.com/api/v1/addons              # JSON
curl -H "Authorization: Bearer vpc_…" "https://control.example.com/api/v1/addons?format=yaml" # addons.yml
```

| Эндпоинт | Ключ эндпоинта | |
|---|---|---|
| `GET /api/v1/customers` | `customers:list` | Список клиентов |
| `GET /api/v1/customers/lookup?telegram_id=…` или `?short_uuid=…` | `customers:lookup` | Найти клиента по Telegram ID или short id подписки; ответ как у `customers:get` |
| `GET /api/v1/customers/{id}` | `customers:get` | Клиент: баланс, история, подписки и платежи |
| `POST /api/v1/customers` | `customers:create` | Создать клиента |
| `PUT /api/v1/customers/{id}` | `customers:update` | Изменить клиента |
| `DELETE /api/v1/customers/{id}` | `customers:delete` | Удалить (архивировать) клиента |
| `POST /api/v1/customers/{id}/payments/preview` | `customers:payment_preview` | Предпросмотр платежа: разбивка, реферал, итоговый баланс |
| `POST /api/v1/customers/{id}/payments` | `customers:pay` | Записать платёж (на баланс и/или продления) |
| `POST /api/v1/customers/{id}/adjust` | `customers:adjust` | Скорректировать баланс |
| `GET /api/v1/payments` | `payments:list` | Список платежей |
| `PUT /api/v1/payments/{id}` | `payments:update` | Изменить платёж |
| `DELETE /api/v1/payments/{id}` | `payments:delete` | Удалить платёж |
| `GET /api/v1/subscriptions` | `subscriptions:list` | Список подписок |
| `POST /api/v1/subscriptions` | `subscriptions:create` | Привязать пользователя Remnawave как подписку |
| `POST /api/v1/subscriptions/provision` | `subscriptions:provision` | Создать пользователя в Remnawave и подписку |
| `PUT /api/v1/subscriptions/{id}` | `subscriptions:update` | Изменить подписку |
| `DELETE /api/v1/subscriptions/{id}` | `subscriptions:delete` | Отвязать подписку |
| `POST /api/v1/subscriptions/{id}/addons` | `subscriptions:connect_addon` | Подключить аддон |
| `PUT /api/v1/subscription-addons/{id}` | `subscriptions:update_addon` | Изменить подключённый аддон |
| `PUT /api/v1/subscriptions/{id}/traffic-anchor` | `subscriptions:traffic_anchor` | Задать якорь сброса трафика подписки |
| `PUT /api/v1/subscription-addons/{id}/traffic-anchor` | `subscriptions:addon_traffic_anchor` | Задать якорь сброса трафика аддона |
| `DELETE /api/v1/subscription-addons/{id}` | `subscriptions:delete_addon` | Отвязать подключённый аддон |
| `GET /api/v1/items/{kind}/{id}/quote` | `items:quote` | Расчёт продления (kind: subscription | addon) |
| `POST /api/v1/items/{kind}/{id}/extend` | `items:extend` | Продлить с баланса |
| `GET /api/v1/items/{kind}/{id}/tariff-quote` | `items:tariff_quote` | Расчёт смены тарифа |
| `POST /api/v1/items/{kind}/{id}/tariff` | `items:tariff` | Сменить тариф |
| `POST /api/v1/items/{kind}/{id}/enable` | `items:enable` | Включить в Remnawave |
| `POST /api/v1/items/{kind}/{id}/disable` | `items:disable` | Отключить в Remnawave |
| `GET /api/v1/tariffs` | `tariffs:list` | Список тарифов |
| `POST /api/v1/tariffs` | `tariffs:create` | Создать тариф |
| `PUT /api/v1/tariffs/{id}` | `tariffs:update` | Изменить тариф |
| `DELETE /api/v1/tariffs/{id}` | `tariffs:delete` | Удалить тариф |
| `POST /api/v1/tariffs/{id}/sync-included` | `tariffs:sync_included` | Синхронизировать включённые аддоны в подписках |
| `GET /api/v1/addons` | `addons:list` | Каталог аддонов; `?format=yaml` отдаёт файл в формате subpage |
| `GET /api/v1/addons/full` | `addons:full` | Аддоны со всеми полями |
| `POST /api/v1/addons` | `addons:create` | Создать аддон |
| `PUT /api/v1/addons/{id}` | `addons:update` | Изменить аддон |
| `DELETE /api/v1/addons/{id}` | `addons:delete` | Удалить аддон |
| `GET /api/v1/referrals/tree` | `referrals:tree` | Дерево рефералов |
| `GET /api/v1/referrals/accruals` | `referrals:accruals` | Реферальные начисления |
| `GET /api/v1/expenses` | `expenses:list` | Список расходов |
| `POST /api/v1/expenses` | `expenses:create` | Создать расход |
| `PUT /api/v1/expenses/{id}` | `expenses:update` | Изменить расход |
| `DELETE /api/v1/expenses/{id}` | `expenses:delete` | Удалить расход |
| `GET /api/v1/expenses/providers` | `expenses:providers` | Траты по провайдерам |
| `GET /api/v1/expense-items` | `expenses:items` | Статьи расходов |
| `POST /api/v1/expense-items` | `expenses:item_create` | Создать статью |
| `PUT /api/v1/expense-items/{id}` | `expenses:item_update` | Изменить статью |
| `DELETE /api/v1/expense-items/{id}` | `expenses:item_delete` | Удалить статью |
| `GET /api/v1/expense-items/{id}/metered` | `expenses:item_metered` | Потребление по статье |
| `POST /api/v1/expense-items/{id}/close-period` | `expenses:item_close_period` | Закрыть период |
| `POST /api/v1/traffic/sync` | `expenses:traffic_sync` | Синхронизировать трафик |
| `GET /api/v1/rw/users` | `remnawave:users` | Пользователи Remnawave |
| `GET /api/v1/rw/squads` | `remnawave:squads` | Сквады |
| `GET /api/v1/rw/nodes` | `remnawave:nodes` | Ноды |
| `GET /api/v1/rw/infra` | `remnawave:infra` | Инфраструктура |
| `GET /api/v1/rw/sync` | `remnawave:sync_status` | Статус синхронизации |
| `POST /api/v1/rw/sync` | `remnawave:sync` | Синхронизировать сейчас |
| `GET /api/v1/dashboard` | `stats:dashboard` | Показатели дашборда |
| `GET /api/v1/fx/rate` | `stats:fx_rate` | Курс валют |
| `GET /api/v1/settings` | `stats:settings` | Настройки (только чтение) |
| `GET /api/v1/audit` | `stats:audit` | Журнал действий |
| `GET /api/v1/backups` | `backups:list` | Список снапшотов |
| `GET /api/v1/backups/{name}` | `backups:download` | Скачать снапшот |
| `POST /api/v1/backups` | `backups:create` | Сделать снапшот и сразу скачать его |

Права записываются как в панели: `*`, `<ресурс>:*`, `<ресурс>:read`, `<ресурс>:write` или ключ эндпоинта из таблицы.

Запросы и ответы — тот же JSON, что использует интерфейс (суммы в основных единицах валюты). Ошибки приходят как `{"message", "code", "params"}`; без токена или с истёкшим — `401`, без нужного права — `403`. Действия через токен попадают в журнал как `token:<название>`.

## Резервные копии

**Настройки → Резервные копии:**

- **Снапшоты** — полные копии базы в `data/backups`. Они создаются по расписанию, вручную и автоматически перед каждым импортом. Их можно скачать, восстановить по категориям или забрать по API.
- **Экспорт** записывает выбранные категории в zip-архив: `manifest.json` и по JSON-файлу на таблицу. ID и даты сохраняются как есть.
- **Импорт** заменяет выбранные категории одной транзакцией. Если в итоге останутся ссылки на отсутствующие записи (например, подписки без клиентов), импорт откатывается. Перед импортом текущие данные сохраняются снапшотом.

## Разработка

Нужны Go 1.26, [Bun](https://bun.sh) и [Task](https://taskfile.dev).

```bash
task init      # .env, зависимости фронта
task dev       # API на :8080 и dev-сервер Bun на :5173 (проксирует /api)
task test      # go test, проверка типов фронта и ключей локалей
task build     # bin/vpn-control со встроенным интерфейсом
task lint      # go mod tidy, go fmt, go vet и golangci-lint
task docker    # локальный образ ko.local/vpn-control:dev (нужны ko и Docker)
```

Для локального запуска задайте в `.env` `SECURE_COOKIE=false` и `ADMIN_PASSWORD`. Панель по `REMNAWAVE_URL` всё равно должна быть доступна. `go run . --no-sync` отключает фоновую синхронизацию.

- Стек: Go, Fiber, Ent и SQLite (без CGO) на бэкенде; Mantine 9, mantine-react-table, TanStack Query и i18next на фронте. Собранный интерфейс встраивается в бинарник.
- После правки `ent/schema` запустите `task ent:generate`. Миграции применяются автоматически при старте.
- Строки интерфейса лежат в `web/public/locales/{en,ru}/vpn-control.json`. Ключи проверяются типами по английскому файлу, а `bun run i18n:check` проверяет, что у всех языков одинаковый набор ключей. Чтобы добавить язык, создайте там папку и добавьте язык в `web/src/app/i18n/i18n.ts`.
- Ошибки API содержат `code` (и `params`) — интерфейс переводит их по ключам `errors.*`, а английский `message` служит запасным вариантом.

### Релизы

CI ([`.github/workflows`](.github/workflows)) на каждый push и pull request гоняет тесты, линтеры и проверку типов фронта. Тег `v*` запускает [GoReleaser](https://goreleaser.com) ([`.goreleaser.yaml`](.goreleaser.yaml)), который:

- собирает бинарники для `linux/amd64` и `linux/arm64` со встроенным интерфейсом и прикладывает их к GitHub-релизу вместе с контрольными суммами, подписью Sigstore и SBOM;
- собирает образ через [ko](https://ko.build) и публикует его в `ghcr.io/lentryd/vpn-control` с тегами `<версия>` и `latest`.

```bash
git tag v1.0.0 && git push origin v1.0.0
```

`task snapshot` собирает то же самое локально без публикации.

## Лицензия

[AGPL-3.0](LICENSE). Тема, раскладка и часть UI-компонентов адаптированы из [remnawave/frontend](https://github.com/remnawave/frontend) (AGPL-3.0), подробнее — в [`web/THIRD_PARTY.md`](web/THIRD_PARTY.md).

Remna Control — независимый проект, не связанный с командой Remnawave.
