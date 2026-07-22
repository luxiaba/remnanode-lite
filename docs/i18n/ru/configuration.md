<!-- translation: locale=ru; source=docs/configuration.md; source-sha256=8413f3e87bdafa768f4e28d360fb369e781b1689cd197bf31e57ac759bf08693 -->

# Справочник конфигурации

[Английский оригинал](../../configuration.md) · [Индекс](README.md) · [Docker](deployment-docker.md) · [Native Linux](deployment-native.md) · [Эксплуатация](operations.md)

Большинству узлов нужны только два значения: порт, заданный в Panel, и полный Secret Key этого Node. Поддерживаемые шаблоны Docker и Native уже задают пути и профиль для малых серверов.

## Источники конфигурации

Daemon сначала читает один ограниченный файл данных в формате environment, затем применяет известные непустые переменные процесса:

1. путь из явно заданного `REMNANODE_ENV`;
2. `/etc/remnanode-lite/node.env`, если файл существует;
3. `.env` в рабочем каталоге.

Файл разбирается как строки `KEY=value` и никогда не исполняется shell. Неизвестные ключи не действуют и не экспортируются автоматически. Native-службы запускают процесс с чистым окружением, задавая только `REMNANODE_ENV=/etc/remnanode-lite/node.env` и необходимые параметры identity.

Файл `.env` Docker Compose относится к другому механизму: Compose использует его для подстановки до создания контейнера. Экспортированная переменная shell имеет приоритет; в контейнер попадают только ключи, явно перечисленные в `environment` Compose.

## Переменные runtime

| Переменная | Обязательна | По умолчанию | Назначение |
| --- | --- | --- | --- |
| `NODE_PORT` | Да | Шаблоны используют `2222` | HTTPS-порт, по которому Panel обращается к Node |
| `NODE_BIND_ADDR` | Нет | Пусто | Локальный IPv4/IPv6 адрес; пусто означает все адреса |
| `SECRET_KEY` | Условно | Пусто | Полный Secret Panel, в основном для Docker; имеет приоритет над `SECRET_KEY_FILE` |
| `SECRET_KEY_FILE` | Условно | Пусто | Читать Secret из обычного файла; Native использует `/etc/remnanode-lite/secret.key` |
| `XRAY_BIN` | Нет | `/usr/local/lib/remnanode-lite/current/lib/rw-core` | Управляемый бинарный файл rw-core |
| `GEO_DIR` | Нет | `/usr/local/lib/remnanode-lite/current/share/xray` | Каталог `geoip.dat` и `geosite.dat` |
| `LOG_DIR` | Нет | `/var/log/remnanode-lite` | Журналы rw-core |
| `ASN_DB_PATH` | Нет | `/usr/local/lib/remnanode-lite/current/share/asn/asn-prefixes.bin` | База ASN для плагина `asList` |
| `INTERNAL_SOCKET_PATH` | Нет | `/run/remnanode-lite/internal.sock` | Приватный Unix socket для rw-core и healthcheck |
| `INTERNAL_REST_TOKEN` | Нет | Случайный при запуске | Token приватного Unix HTTP; обычно оставляйте пустым |
| `DISABLE_HASHED_SET_CHECK` | Нет | `false` | Отладка: перезапускать rw-core при каждом start |
| `LOW_MEMORY` | Нет | В шаблонах `1` | Профиль 512 MiB: soft limit Go 180 MiB и бюджет запроса 16 MiB |
| `BODY_LIMIT_MB` | Нет | Автоматически | Общий бюджет request body |
| `GOMEMLIMIT` | Нет | Автоматически | Soft limit Go; поддерживает `KiB/MiB/GiB/TiB` и `off` |
| `NODE_CONTRACT_VERSION` | Нет | Скомпилированный `ContractVersion` | Версия контракта для контролируемой диагностики |
| `XRAY_CORE_VERSION` | Нет | Определяется по rw-core | Отладочное переопределение версии core |

Boolean принимает `true/false`, `1/0` и `yes/no`. Диапазон `NODE_PORT` — `1..65535`. `BODY_LIMIT_MB` принимает `1..1024`, но при `LOW_MEMORY=1` не может превышать `16`; пусто или `0` выбирает автоматическое значение.

`GOMEMLIMIT` ограничивает только память, управляемую Go runtime. Это не лимит RSS всего процесса или хоста; служба и контейнер по-прежнему ограничены `448 MiB`.

## Secret Panel

Secret — полное значение, выданное Panel одному Node. В нём находятся данные для mTLS и JWT. JWT, сертификат, ключ или укороченная строка не заменяют полный Secret.

### Docker

Поместите Secret в `.env` рядом с Compose и установите режим `0600`:

```env
NODE_PORT=38329
SECRET_KEY=PASTE_THE_COMPLETE_PANEL_SECRET_KEY
```

Используйте mapping:

```yaml
environment:
  SECRET_KEY: "${SECRET_KEY:?set SECRET_KEY in .env}"
```

Форма списка `- SECRET_KEY="..."` опасна: кавычки могут стать частью значения и вызвать ошибку base64. Docker хранит переменные в локальных metadata контейнера, поэтому защищайте каталог Compose и Docker socket.

### Native Linux

Native хранит Secret отдельно от `node.env`:

```env
SECRET_KEY=
SECRET_KEY_FILE=/etc/remnanode-lite/secret.key
```

После проверки installer пишет файл как `root:remnanode-lite 0640`. При установке или активации используйте `--secret-file`; не передавайте само значение аргументом.

## Переменные Compose

| Переменная | Fallback | Передаётся Node | Назначение |
| --- | --- | --- | --- |
| `REMNANODE_IMAGE` | В Release точная версия; single-file по умолчанию `latest` | Нет | Tag либо `name@sha256:...`; для fleet выбирайте точную версию или digest |
| `NODE_PORT` | `2222` | Да | Порт Panel-to-Node |
| `NODE_BIND_ADDR` | Пусто | Да | Необязательный bind address |
| `SECRET_KEY` | Нет | Да | При отсутствии Compose завершает подстановку с ошибкой |
| `LOW_MEMORY` | `1` | Да | Профиль малого сервера |
| `DISABLE_HASHED_SET_CHECK` | `false` | Да | Только диагностика |
| `BODY_LIMIT_MB` | Пусто | Да | Пусто выбирает значение daemon |
| `GOMEMLIMIT` | Пусто | Да | Пусто выбирает low-memory default |

Приоритет: shell, затем `.env`, затем `${NAME:-fallback}` в YAML. Для проверки без печати развёрнутого Secret выполните `docker compose config --quiet`.

Следующие пути находятся во внутренней файловой системе Docker-образа. Они используют то же имя проекта, что и Native, но не являются путями хоста:

```text
XRAY_BIN=/usr/local/lib/remnanode-lite/rw-core
GEO_DIR=/usr/local/share/remnanode-lite/xray
ASN_DB_PATH=/usr/local/share/remnanode-lite/asn/asn-prefixes.bin
LOG_DIR=/var/log/remnanode-lite
INTERNAL_SOCKET_PATH=/run/remnanode-lite/internal.sock
```

Они принадлежат опубликованному образу и не конфликтуют с Native-layout хоста. Поддерживаемые tmpfs Compose и команды журналов уже соответствуют им; при override сохраняйте это соответствие.

## Native `node.env`

Шаблон находится в [`deploy/node.env.example`](../../../deploy/node.env.example):

```env
NODE_PORT=2222
SECRET_KEY=
SECRET_KEY_FILE=/etc/remnanode-lite/secret.key
XRAY_BIN=/usr/local/lib/remnanode-lite/current/lib/rw-core
GEO_DIR=/usr/local/lib/remnanode-lite/current/share/xray
LOG_DIR=/var/log/remnanode-lite
ASN_DB_PATH=/usr/local/lib/remnanode-lite/current/share/asn/asn-prefixes.bin
INTERNAL_SOCKET_PATH=/run/remnanode-lite/internal.sock
LOW_MEMORY=1
```

`rnlctl` переписывает управляемые пути при установке и отклоняет их дубликаты. Администратор может задавать `NODE_BIND_ADDR`, `BODY_LIMIT_MB` и `GOMEMLIMIT`, но не должен направлять управляемые пути на общую установку Xray. `node.env` и Secret должны оставаться обычными файлами, а не symbolic links.

## Изменение параметров

Docker:

```bash
docker compose config --quiet
docker compose up -d --no-build --force-recreate
```

Native:

```bash
sudo rnlctl doctor
sudo rnlctl restart
```

При изменении `NODE_PORT` одновременно обновите Panel и firewall хоста. Host networking не выполняет трансляцию портов.

## Переменные сопровождающих

`REMNANODE_OFFICIAL_SOURCE`, `REMNANODE_CONTRACT_CA`, `REMNANODE_CONTRACT_CERT`, `REMNANODE_CONTRACT_KEY`, `RNL_ASSET_CACHE_DIR`, `RNL_OFFLINE_BUILD`, `SOURCE_REVISION` и `SOURCE_DATE_EPOCH` относятся к сборке, contract tests и CI, а не к production installer. Версии и digest runtime-ресурсов зафиксированы в [`release/runtime-assets.lock.json`](../../../release/runtime-assets.lock.json).
