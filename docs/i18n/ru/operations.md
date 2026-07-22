<!-- translation: locale=ru; source=docs/operations.md; source-sha256=5016f5e4ec1b7e7e3197d194941dea06af2efa3b719be96dbe6e3aa17aeb2e68 -->

# Эксплуатация и диагностика

[Английский оригинал](../../operations.md) · [Индекс](README.md) · [Docker](deployment-docker.md) · [Native Linux](deployment-native.md) · [Конфигурация](configuration.md)

У Remnanode Lite небольшой постоянный footprint. Источником proxy-конфигурации остаётся Panel, поэтому в работе проверяются четыре элемента: процесс Node, связь с Panel, состояние rw-core и настоящий proxy-трафик.

## Что доказывают проверки

| Проверка | Доказывает | Не доказывает |
| --- | --- | --- |
| Контейнер или служба работает | Supervisor видит процесс Node | Внутренний health доступен |
| Docker health или `rnlctl status --json` успешно | Приватный Unix socket отвечает, managed state согласован | Panel достигает публичного порта |
| Panel показывает Node online | Работают mTLS/JWT и путь Panel-to-Node | rw-core получил рабочую конфигурацию |
| Panel показывает rw-core online | Core запустился и internal gRPC работает | Каждый proxy route передаёт трафик |
| Клиент передаёт трафик | Проверенный путь работает end-to-end | Работают все протоколы и address families |

Публичный `/node/xray/healthcheck` требует mTLS и JWT и не является анонимным endpoint мониторинга.

## Обычные проверки

Docker:

```bash
docker compose ps
docker compose logs --tail=100 remnanode-lite
docker inspect remnanode-lite --format \
  'image={{.Config.Image}} status={{.State.Status}} health={{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}} oom={{.State.OOMKilled}} restarts={{.RestartCount}}'
docker exec remnanode-lite remnanode-lite version
ss -H -lntp 'sport = :38329'
```

Native:

```bash
sudo rnlctl status --json
sudo rnlctl doctor
sudo rnlctl logs node --lines 100
sudo rnlctl logs core-errors --lines 100
ss -H -lntp 'sport = :38329'
```

`status --json` сообщает current/previous generation, версию, manager службы, enabled/active, возможность repair и незавершённую операцию. При degraded или recovery-required он возвращает ненулевой код. `doctor` проверяет manifests, digest файлов, links, конфигурацию, Secret, службу, internal health и cache восстановления, но не связывается с Panel и не генерирует трафик.

Низкоуровневые команды:

```bash
sudo systemctl --no-pager --full status remnanode-lite.service
sudo systemctl show remnanode-lite.service \
  --property=ActiveState,SubState,MainPID,MemoryCurrent,MemoryPeak,TasksCurrent

# OpenRC (экспериментально)
sudo rc-service remnanode-lite status
```

## Журналы

| Развёртывание | Журнал Node | Хранилище |
| --- | --- | --- |
| Docker | `docker compose logs -f remnanode-lite` | Docker `json-file`, в шаблоне `2 MiB x 2` |
| Native systemd | `sudo rnlctl logs node --follow` | Политика journald хоста |
| Native OpenRC | `sudo rnlctl logs node --follow` | `/var/log/remnanode-lite/openrc.log` и `.err.log` |

На малом systemd-хосте настройте разумную общую квоту journald и контролируйте `journalctl --disk-usage` и `df -h`.

Docker использует приватные контейнерные пути rw-core:

```bash
docker exec -it remnanode-lite \
  sh -c 'tail -n 50 -F "$LOG_DIR/xray.out.log" "$LOG_DIR/xray.err.log"'
```

Native:

```bash
sudo rnlctl logs core --follow
sudo rnlctl logs core-errors --follow
```

Native-файлы находятся в `/var/log/remnanode-lite/xray.out.log` и `xray.err.log`. Для каждого потока хранится текущий файл и один `.1` с порогом 4 MiB. Docker держит каталог core logs в tmpfs 28 MiB, поэтому recreate очищает его.

## Запуск и остановка

Docker:

```bash
docker compose restart remnanode-lite
docker compose stop remnanode-lite
docker compose up -d --no-build
docker compose down
```

Native:

```bash
sudo rnlctl restart
sudo rnlctl stop
sudo rnlctl start
```

Установка с `--prepare-only` сначала требует `rnlctl activate`. Не используйте `kill -9` для обычных операций: он обходится без HTTP drain, завершения process group rw-core и очистки nftables.

## Обновление и откат Docker

| Ссылка | Назначение |
| --- | --- |
| `name@sha256:<digest>` | Самая строгая production-фиксация и rollback identity |
| `X.Y.Z` | Точный stable Release |
| `X.Y.Z-rnl.N` | Точный preview Release |
| `latest` | Добровольный движущийся stable channel |
| `preview` | Движущийся preview channel, не rollback identity |
| `sha-<40-character-commit>` | Кандидат main для проверки |
| `edge` | Краткосрочный main build для разработки |

Порядок контролируемого обновления:

1. Запишите текущий точный tag или manifest digest.
2. Прочитайте Release notes.
3. Измените `REMNANODE_IMAGE` в `.env` или намеренно inline `image:`.
4. Выполните pull и recreate.
5. Проверьте health, Panel и representative traffic.

```bash
docker compose config --quiet
docker compose pull
docker compose up -d --no-build --force-recreate
docker compose ps
docker compose logs --tail=100 remnanode-lite
```

`latest` и `preview` не обновляют работающий контейнер, а `docker compose restart` не выполняет pull. Для отката восстановите сохранённый exact tag/digest и снова выполните pull/recreate.

## Native: update, rollback, repair

Native принимает только точную версию:

```bash
sudo rnlctl upgrade --to 2.8.0-rnl.2
sudo rnlctl rollback
```

Полный bundle Node/runtime становится новым generation, прежний сохраняется как previous. Если состояние показывает `recovery-required`:

```bash
sudo rnlctl status --json
sudo rnlctl repair
sudo rnlctl doctor
```

Repair использует проверенный cache и никогда не обновляет версию. Все mutation lifecycle используют `/run/remnanode-lite-installer/operation.lock`; дождитесь текущей операции и не удаляйте lock или `/var/lib/remnanode-lite-installer/journal.json`.

## Изменение конфигурации

После изменения Docker `.env` или Compose mapping проверьте модель и recreate контейнер. Для Native сохраняйте `/etc/remnanode-lite/node.env` и `secret.key` как `root:remnanode-lite`, недоступные для записи службе:

```bash
sudo rnlctl doctor
sudo rnlctl restart
```

Secret меняется атомарной заменой `/etc/remnanode-lite/secret.key`; процедура приведена в [Native guide](deployment-native.md#порт-и-secret). При изменении `NODE_PORT` обновите Panel и firewall хоста. Оба способа используют host networking без трансляции портов.

## Ресурсы

Профили Docker и Native задают `448 MiB RAM`, без дополнительного swap, `1 CPU`, `256 PIDs/tasks`. Цель полного хоста `512 MiB / 1 vCPU / 2 GB` не является гарантией для любого числа пользователей и protocol mix.

Docker:

```bash
docker stats --no-stream remnanode-lite
docker inspect remnanode-lite --format \
  'oom={{.State.OOMKilled}} restarts={{.RestartCount}}'
docker system df
df -h
```

systemd:

```bash
systemctl show remnanode-lite.service \
  --property=MemoryCurrent,MemoryPeak,TasksCurrent,CPUUsageNSec
journalctl --disk-usage
df -h
```

OpenRC использует `/sys/fs/cgroup/openrc.remnanode-lite` под обнаруженным корнем cgroup v2 и проверяет limits memory, swap, CPU и PID до запуска. Не собирайте проект на production-хосте с диском 2 GB.

## Сеть и безопасность

Оба способа работают в host network namespace. `CAP_NET_ADMIN` нужен для частной таблицы nftables и выборочного TCP socket destroy; `CAP_NET_BIND_SERVICE` позволяет rw-core слушать порты ниже 1024.

- Запускайте доверенный точный Release или проверенный digest.
- Не используйте `privileged: true`, root Native service или лишние capabilities.
- По возможности разрешайте доступ к Node API только адресам Panel.
- Открывайте proxy-порты по конфигурации Panel.
- Защищайте Docker socket, root, каталог Compose и `/etc/remnanode-lite`.
- Проект владеет только своей runtime-таблицей nftables, а не глобальным firewall или sysctl хоста.

## Типичные ошибки

### `illegal base64 data at input byte 0`

Secret повреждён, обрезан, содержит whitespace или кавычки из list-form Compose. Получите полный Secret снова и используйте mapping из справочника.

### `SECRET_KEY missing required fields`

Значение декодируется, но не является полным Secret Node. JWT, сертификата или части private key недостаточно.

### `address already in use`

```bash
ss -H -lntp 'sport = :38329'
```

Остановите конфликтующую службу либо одновременно измените Panel, конфигурацию хоста и firewall. Не запускайте официальный и Lite контейнеры на одних портах.

### Локально healthy, в Panel offline

Проверьте соответствие порта, владельца listen socket, firewall/route, принадлежность Secret этому Node, системное время и TLS/JWT/listen errors в журнале. Local health не проверяет внешнюю сеть.

### Node online, rw-core offline

Читайте `core-errors`, ищите конфликт портов и проверяйте конфигурацию Panel. Low-memory mode даёт большой конфигурации больше времени на readiness.

### `CAP_NET_ADMIN not available`

Восстановите capabilities из проекта или выполните repair. Не скрывайте проблему privileged-контейнером или root-службой.

### ASN database unavailable

Node продолжает работать, но `asList` пуст. Docker и Native bundle содержат закреплённую базу; recreate проверенного образа либо `rnlctl repair`/точный upgrade безопаснее загрузки неподписанной базы в активный generation.

### OpenRC cgroup check fails

Исправьте delegation cgroup v2 или используйте systemd/Docker. Не обходите resource checks.

### Native требует repair

Сохраните `status --json` и выполните `rnlctl repair`. Не удаляйте вручную файлы из `/usr/local/lib/remnanode-lite` или `/var/lib/remnanode-lite-installer`.

## Резервное копирование

- Docker: Compose, необязательный `.env`, точный текущий image tag или digest.
- Native: `/etc/remnanode-lite/node.env`, `/etc/remnanode-lite/secret.key`, точная текущая версия Release.
- Fleet: предыдущая known-good точная версия или digest.

Защищайте копии Secret как private-key material. Не сохраняйте `/run`, Docker tmpfs logs, runtime-конфигурацию Xray из Panel или Native generations вместо Release assets и состояния `rnlctl`.
