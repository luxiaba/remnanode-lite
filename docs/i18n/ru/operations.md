<!-- translation: locale=ru; source=docs/operations.md; source-sha256=c90887e1b41cb78a7ec656a5872b8add62b215b70dfa29c60f155c59e8d5d28f -->

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
ss -H -lntp 'sport = :2222'
```

Native:

```bash
sudo rnlctl status
sudo rnlctl status --json
sudo rnlctl doctor
sudo rnlctl logs node --lines 100
sudo rnlctl logs core-errors --lines 100
ss -H -lntp 'sport = :2222'
```

Теперь `rnlctl status` без параметров выводит единообразную человекочитаемую сводку жизненного цикла, а не перенаправляет сырой вывод менеджера служб. Этот формат не является интерфейсом для разбора. `status --json` сохраняет прежнюю схему с current/previous generation, версией, менеджером службы, enabled/active, возможностью repair и незавершённой операцией. При `degraded` или `recovery-required` обе формы возвращают ненулевой код.

`doctor` проверяет manifests, digest файлов, links, конфигурацию, Secret, службу, internal health и cache восстановления. Человекочитаемый вывод заканчивается итогом и упорядоченными подсказками `Next` для известных сбоев; схема `doctor --json` не изменилась. Команда не связывается с Panel и не генерирует прокси-трафик.

Низкоуровневые команды:

```bash
sudo systemctl --no-pager --full status remnanode-lite.service
sudo systemctl show remnanode-lite.service \
  --property=ActiveState,SubState,MainPID,MemoryCurrent,MemoryPeak,TasksCurrent

# Alpine/OpenRC
sudo rc-service remnanode-lite status
```

Для полных низкоуровневых сведений используйте эти команды менеджера служб напрямую. Скриптам, которые раньше разбирали вывод `rnlctl status` без параметров, следует перейти на `rnlctl status --json`.

## Вывод `rnlctl` и автоматизация

`--quiet`/`-q`, `--no-color` и `--progress auto|plain|never` — глобальные
параметры; их можно ставить в любом месте команды `rnlctl`. Ход операции
записывается в stderr, итог и данные команды — в stdout, а ошибки — в stderr.

`auto` — режим по умолчанию. При TTY на stderr используется живой индикатор,
в остальных случаях выводятся стабильные строки по этапам; `TERM=dumb` также
выбирает построчный вывод. `plain` отключает перерисовку курсора, а `never`
скрывает только прогресс. Процент, скорость и ETA показываются лишь для загрузки
с известным полным размером. Этапы соответствуют реальной работе, но не являются
общим процентом готовности или стабильным интерфейсом для разбора.

`--quiet` имеет приоритет над выбранным режимом прогресса и скрывает сообщения об
успешных изменениях, успешный результат `config check` и человекочитаемый вывод
`status`/`doctor`. Он не скрывает help, version, `config show`/`get`, журналы,
автодополнение, планы dry-run, JSON или ошибки. При выводе JSON прогресс отключён.

Сдержанные цвета используются только тогда, когда соответствующий поток
подключён к TTY: status и doctor проверяют stdout, а прогресс — stderr.
`--no-color`, непустая переменная `NO_COLOR` и `TERM=dumb` отключают цвет в
обоих потоках; перенаправленный поток также не содержит цветовых
escape-последовательностей.

Первый `SIGINT`, `SIGTERM`, `SIGHUP` или `SIGQUIT` запрашивает корректную отмену;
команды хоста и проверки состояния, необходимые для очистки или отката,
выполняются с минутным сроком восстановления. Работа с локальными файлами может
завершить текущий ограниченный шаг перед выходом. Ctrl-C возвращает `130` после
этой попытки. Затем восстанавливается стандартная обработка сигнала ОС, поэтому
повторный сигнал может немедленно завершить процесс. При необходимости после
этого проверьте `status --json` и запустите `repair`.

Обычно `0` означает успех, `1` — ошибку выполнения или нездоровое состояние, `2` — ошибку в аргументах. `absent` является допустимым status и возвращает `0`; если скрипту нужна установленная служба, он должен проверить `installed` или `deployment` в JSON. После запуска `journalctl` или `tail` команда `logs` возвращает его код завершения, в том числе `128 + signal` при завершении сигналом.

Сценарии автодополнения для Bash, Zsh и Fish генерируются командой `rnlctl completion <shell>`; пользовательская установка описана в [руководстве Native](deployment-native.md#автодополнение-команд-оболочки). Команда только пишет сценарий в stdout и не изменяет каталоги автодополнения или файлы запуска оболочки.

## Журналы

| Развёртывание | Журнал Node | Хранилище |
| --- | --- | --- |
| Docker | `docker compose logs -f remnanode-lite` | Docker `json-file`, в шаблоне `2 MiB x 2` |
| Native systemd | `sudo rnlctl logs node --follow` | Политика journald хоста |
| Native Alpine/OpenRC | `sudo rnlctl logs node --follow` | `/var/log/remnanode-lite/openrc.log` и `.err.log` |

На малом systemd-хосте настройте разумную общую квоту journald и контролируйте `journalctl --disk-usage` и `df -h`.

Docker использует приватные контейнерные пути rw-core:

```bash
docker exec -it remnanode-lite \
  sh -c 'tail -n 50 -F "$LOG_DIR/xray.out.log" "$LOG_DIR/xray.err.log"'
```

Native:

```bash
sudo rnlctl logs node --since 15m --lines 100
sudo rnlctl logs core --follow
sudo rnlctl logs core-errors --follow
```

`--lines` по умолчанию равен `50` и принимает `1..100000`. `--since` принимает положительную длительность Go, например `15m` или `2h`, но не абсолютную дату или `1d`; он сочетается с `--lines` и `--follow`, но поддерживается только для `node` в systemd. Журналы Node в OpenRC и файловые источники `core`/`core-errors` отклоняют этот параметр.

Systemd применяет `--lines N` ко всему unit. OpenRC читает по N строк из `openrc.log` и `openrc.err.log`, а каждый core source читает один текущий файл. Native-файлы находятся в `/var/log/remnanode-lite/xray.out.log` и `xray.err.log`; для каждого потока хранится текущий файл и один `.1` с порогом 4 MiB, но обычное чтение не дополняется данными из `.1`. `--follow` использует `tail -F` и продолжает работу после последующей ротации. Docker держит каталог core logs в tmpfs 28 MiB, поэтому recreate очищает его.

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
sudo rnlctl upgrade --to 2.8.0-rnl.2 --dry-run
sudo rnlctl upgrade --to 2.8.0-rnl.2 --dry-run --json
sudo rnlctl upgrade --to 2.8.0-rnl.2
sudo rnlctl rollback
```

Для dry-run нужны права root, существующая согласованная установка и отсутствие незавершённого lifecycle journal. С `--to` он полностью загружает и статически проверяет кандидат, затем ненадолго получает lock жизненного цикла для проверки текущего состояния и известных требований к хосту. Он не создаёт generation, cache или transaction journal, не изменяет службу, не исполняет бинарный файл кандидата, не запускает health целевой версии и не сохраняет bundle. `--json` допускается только с `--dry-run`. Проверка использует временный диск, но не резервирует и не гарантирует место для настоящего обновления; успешный план не означает, что обновление обязательно завершится успешно. Локальные `--bundle` с `--sha256` и `--bundle-root` поддерживают тот же dry-run.

Предварительная проверка и обновление в обычном текстовом режиме сообщают о
выборе точного Release, загрузке, проверке и реально выполняемых этапах жизненного
цикла. Процент и ETA появляются только при известном полном размере; JSON dry-run
всегда остаётся без прогресса.

Полный bundle Node/runtime становится новым generation, прежний сохраняется как previous. Если состояние показывает `recovery-required`, сначала изучите проблему. Запускайте repair только когда status показывает читаемую транзакцию `pending`; в остальных случаях используйте doctor для проверки нечитаемых метаданных жизненного цикла. Не изменяйте ссылки или файлы состояния вручную:

```bash
sudo rnlctl status --json
sudo rnlctl repair
sudo rnlctl doctor
```

Repair использует проверенный cache и никогда не обновляет версию. Все mutation lifecycle используют `/run/remnanode-lite-installer/operation.lock`; дождитесь текущей операции и не удаляйте lock или `/var/lib/remnanode-lite-installer/journal.json`.

## Изменение конфигурации

После изменения Docker `.env` или Compose mapping проверьте модель и заново создайте контейнер. В Native единственным источником настроек службы служит `/etc/remnanode-lite/node.env`; `rnlctl config` читает и изменяет его напрямую и показывает только шесть администраторских полей без Secret. Secret и внутренние управляемые поля не выводятся.

Просмотр, проверка и изменение активной установки:

```bash
sudo rnlctl config show
sudo rnlctl config check
sudo rnlctl config set NODE_PORT=2222 --apply
```

`config show` и `get` выводят значения, сохранённые в `node.env`, а не значения по умолчанию, вычисленные службой. `config check` ничего не записывает и проверяет права на управляемые `node.env` и `secret.key`. `set` и `unset` до записи проверяют всю получившуюся конфигурацию. С `--apply` они перезапускают активную службу и ждут внутреннюю проверку состояния; при ошибке после изменения команда пытается вернуть прежний файл и службу. Это попытка восстановления без гарантий, а не журналируемая транзакция, устойчивая к сбою процесса или системы. В остановленном состоянии или состоянии `prepared` параметр `--apply` отклоняется до записи: измените настройку без него, затем выполните `rnlctl start` или `rnlctl activate`.

Ручное редактирование поддерживается: сохраните `root:remnanode-lite 0640`, выполните `rnlctl config check`, затем `rnlctl config apply` для активной службы. У `config apply` нет снимка файла до ручной правки, поэтому откатить её команда не может.

Secret меняется только из защищённого обычного файла; значение не принимается аргументом и не выводится:

```bash
sudo rnlctl secret set --file /root/new-node-secret.key --apply
```

После операции удалите исходный файл. Для Secret действуют те же требования к активному состоянию и попытка восстановления без гарантий, что и для `config set --apply`. Полная процедура приведена в [руководстве Native](deployment-native.md#порт-и-secret). `config check` и `config apply` не подключаются к Panel и не проверяют трафик прокси; состояние Panel и реальный трафик проверяйте отдельно. При изменении `NODE_PORT` обновите Panel и firewall хоста. Оба способа используют host networking без трансляции портов.

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

При запуске через Alpine/OpenRC служба создаёт `openrc.remnanode-lite` под обнаруженным корнем единой иерархии cgroup v2. До запуска она проверяет `memory.max=469762048`, `memory.swap.max=0`, `cpu.max=100000 100000`, `pids.max=256`, собственное членство в cgroup, доступность записи в родительский `cgroup.procs` и в `cgroup.kill` группы службы.

Не собирайте проект на production-хосте с диском 2 GB.

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
ss -H -lntp 'sport = :2222'
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

### Хост Alpine/OpenRC не проходит проверку

Версия Alpine на хосте должна присутствовать в [матрице хостов Native](deployment-native.md#матрица-хостов-native). Нужна постоянная установка `sys`, загруженная через штатную связку init/OpenRC. Требуются Linux 5.14 или новее и единая иерархия cgroup v2 с рабочими контроллерами CPU, памяти и PID, ограничением подкачки, изменением членства через родительский `cgroup.procs` и завершением группы через `cgroup.kill`. Успешная команда `--prepare-only` этого не доказывает, поскольку служба ещё не запускается. Если `activate` или `start` отклоняет хост, используйте среду с полным набором возможностей либо выберите хост с systemd из поддерживаемой матрицы или Docker. Не обходите проверку: без неё нельзя гарантировать заявленные лимиты ресурсов и корректное завершение процессов.

### Native требует repair

Сохраните `status --json` и выполните `rnlctl repair`. Не удаляйте вручную файлы из `/usr/local/lib/remnanode-lite` или `/var/lib/remnanode-lite-installer`.

## Резервное копирование

- Docker: Compose, необязательный `.env`, точный текущий image tag или digest.
- Native: `/etc/remnanode-lite/node.env`, `/etc/remnanode-lite/secret.key`, точная текущая версия Release.
- Fleet: предыдущая known-good точная версия или digest.

Защищайте копии Secret как private-key material. Не сохраняйте `/run`, Docker tmpfs logs, runtime-конфигурацию Xray из Panel или Native generations вместо Release assets и состояния `rnlctl`.
