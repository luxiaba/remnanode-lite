<!-- translation: locale=ru; source=docs/deployment-native.md; source-sha256=b226928707e7d24793185dae0284459b04e720fee4c3ddb0a241199b1b4258d3 -->
# Нативное развёртывание Linux

> English is the only authoritative source. If this translation differs from the [canonical English native deployment guide](../../deployment-native.md), follow the English document.

[К индексу документации на русском](README.md)

В этом руководстве описана установка Remnanode Lite из бинарных файлов GitHub Release на хосте с systemd или OpenRC. Для очень небольшого узла, которому нужна только контейнерная среда, начните с [русского руководства по Docker Compose](deployment-docker.md). Нативное развёртывание исключает накладные расходы Docker daemon и позволяет менеджеру сервисов хоста напрямую управлять процессами.

Отображаемое имя проекта — `Remnanode Lite`, бинарный файл — `remnanode-lite`. Имя нативного сервиса и файлы unit/init в репозитории сохраняют `remnawave-node` для совместимости с существующими обновлениями, мониторингом и эксплуатационными командами. Это стабильный системный интерфейс; он не означает, что данный репозиторий имеет отношение к официальному проекту как upstream.

## Граница поддержки

Сборка и CI охватывают Linux `amd64` и `arm64`. Сейчас документированы следующие реальные инженерные снимки установки и управления сервисом:

| Платформа | Менеджер сервисов | Архитектура |
| --- | --- | --- |
| Ubuntu 24.04 | systemd | arm64 |
| Alpine 3.22 | OpenRC | arm64 |

CI выполняет кросс-компиляцию amd64 и arm64 и тесты сетевого администрирования Linux на runner с Ubuntu. Наличие сборки не означает приёмку времени выполнения. Единственный блокирующий профиль `v2.8.0` — канонический smoke-тест Docker Compose на реальном хосте `x86_64` (`linux/amd64`); `native-systemd-install` и `native-openrc-install` на любой архитектуре отложены и не блокируют выпуск. Две строки выше остаются реальными инженерными снимками, а не требованием повторить обе системы init или обе архитектуры перед выпуском `v2.8.0`.

Реальная работа на `arm64`, нагрузка 50k пользователей на кандидате, 24-часовой soak-тест, инъекция отказов и инъекция отката также отложены для последующей проверки `v2.8.0`. Отсутствующие результаты следует указывать как deferred, а не passed. Ожидается, что другие современные дистрибутивы с systemd будут работать, но они не являются проверенной базовой средой. На системах, отличных от Debian/Ubuntu, заранее установите команды, необходимые скриптам.

Целевой тег должен иметь опубликованный GitHub Release с бинарными архивами, файлами support, `SHA256SUMS` и базой ASN. Кандидатные образы GHCR `edge` или `sha-*` не заменяют нативные артефакты Release.

## Предварительные требования

- Доступ root.
- Linux amd64 или arm64.
- Созданный в Panel узел и его полный Secret Key.
- Порт Node, настроенный в Panel, совпадает с `NODE_PORT` хоста.
- Правильное системное время и рабочий доступ к сети.
- Перед первой установкой или синхронизацией ресурсов rw-core рекомендуется не менее 1 GiB свободного места. Установщик вычисляет фактический бюджет по каждой файловой системе для скачивания, распаковки, staging целевых файлов и существующих резервных копий.
- Bash, curl и `flock` из util-linux установлены до bootstrap.
- Firewall хоста разрешает Panel доступ к порту Node API и открывает входящие proxy-порты, необходимые фактической конфигурации.

Шаблоны systemd и OpenRC ограничивают сервис до `448 MiB RAM / 0 swap / 1 CPU / 256 tasks`. Для OpenRC дополнительно требуются доступные для записи и фактически действующие контроллеры memory, CPU и PIDs в cgroup v2. Если любой контроллер недоступен, сервис отказывается запускаться.

### Зависимости bootstrap

Ubuntu или Debian:

```bash
sudo apt-get update
sudo apt-get install --yes curl util-linux
```

Alpine:

```bash
apk add --no-cache bash curl util-linux
```

Затем установщик добавляет зависимости времени выполнения, включая CA certificates, tar, unzip, iproute2 и nftables.

## Установка с systemd

Выберите точный тег, для которого уже опубликован Release. Допустимы и версии, согласованные с официальными, и независимые итерации проекта:

```bash
release_tag='vX.Y.Z-rnl.N' # or vX.Y.Z
```

Интерактивная установка запрашивает порт и Secret:

```bash
curl -fsSL \
  "https://raw.githubusercontent.com/luxiaba/remnanode-lite/${release_tag}/scripts/install-node.sh" \
  | sudo env RNL_TAG="${release_tag}" bash
```

Для неинтерактивной установки передавайте Secret через файл с ограниченными правами, чтобы он не остался в истории shell:

```bash
umask 077
printf '%s' 'PASTE_THE_COMPLETE_SECRET_KEY_FROM_THE_PANEL' > /tmp/remnanode-secret.key

curl -fsSL \
  "https://raw.githubusercontent.com/luxiaba/remnanode-lite/${release_tag}/scripts/install-node.sh" \
  | sudo env RNL_TAG="${release_tag}" bash -s -- \
      --install --yes --port 2222 --secret-file /tmp/remnanode-secret.key

rm -f /tmp/remnanode-secret.key
```

Проверьте установку:

```bash
sudo systemctl --no-pager status remnawave-node
sudo ss -H -lntp 'sport = :2222'
sudo remnanode-lite doctor
```

## Установка с Alpine/OpenRC

Для Alpine предусмотрена отдельная точка входа:

```bash
release_tag='vX.Y.Z-rnl.N' # or vX.Y.Z

curl -fsSL \
  "https://raw.githubusercontent.com/luxiaba/remnanode-lite/${release_tag}/scripts/install-node-alpine.sh" \
  | env RNL_TAG="${release_tag}" bash
```

Неинтерактивные параметры совпадают с systemd:

```bash
curl -fsSL \
  "https://raw.githubusercontent.com/luxiaba/remnanode-lite/${release_tag}/scripts/install-node-alpine.sh" \
  | env RNL_TAG="${release_tag}" bash -s -- \
      --install --yes --port 2222 --secret-file /root/remnanode-secret.key
```

Проверьте установку:

```bash
rc-service remnawave-node status
ss -H -lntp 'sport = :2222'
remnanode-lite doctor
```

Сейчас `doctor` также проверяет unit systemd, поэтому WARN об отсутствии unit systemd ожидаем в OpenRC. Ошибки ERROR влияют на код завершения и основной результат. Сквозное соединение с Panel всё равно необходимо подтвердить в Panel.

## Параметры установщика

Обе точки входа предоставляют одинаковые основные параметры:

| Параметр | Описание |
| --- | --- |
| `--install` | Новая установка. Если обнаружена полная установка, переключается на обновление с возможностью отката и по умолчанию синхронизирует rw-core, geo и ASN из целевого Release. Добавьте `--skip-xray`, чтобы сохранить существующие ресурсы. |
| `--upgrade` | Явно обновить Node, сервис и support, по умолчанию сохранив rw-core. |
| `--uninstall` | Перейти к процедуре удаления. |
| `--yes`, `-y` | Пропустить подтверждение. Если Secret отсутствует, установка завершается без запуска сервиса. |
| `--dry-run` | Предварительно показать действия без изменения системы. |
| `--skip-xray` | Не устанавливать rw-core. Только для продвинутых сред, самостоятельно предоставляющих совместимый core. |
| `--low-memory` | Принудительно записать `LOW_MEMORY=1` в конфигурацию. Рекомендуется для узлов с малым объёмом памяти. |
| `--port PORT` | HTTPS-порт Node в диапазоне `1..65535`; по умолчанию 2222. |
| `--secret-file PATH` | Безопасно прочитать, канонизировать и проверить Secret из обычного файла. |

Установщик автоматически включает режим малого объёма памяти, если общий `MemTotal <= 512 MiB`. Если `node.env` уже существует, текущие порт и выбор режима малого объёма памяти сохраняются, пока они не переопределены явно.

## Транзакция установки

Установщик:

1. Получает глобальную блокировку установщика, отклоняя параллельную установку, обновление, обновление rw-core или удаление.
2. Проверяет архитектуру, бюджет диска и базовые команды.
3. Создаёт выделенную системную учётную запись `remnanode:remnanode` и каталоги с ограниченными правами.
4. Скачивает `SHA256SUMS` и архив архитектуры целевого Release, затем проверяет контрольные суммы, структуру и версию бинарного файла.
5. Устанавливает Node, файлы support, закреплённый rw-core, geo-данные и компактную базу ASN.
6. Проверяет и сохраняет Secret, устанавливает определение сервиса и вспомогательные команды журналов.
7. Запускает сервис и подтверждает, что настроенным TCP-портом владеет ровно один целевой процесс Node.

При повторном запуске `--install` для полной установки транзакционное обновление делегируется `upgrade.sh`, а rw-core, geo и ASN по умолчанию синхронизируются из целевого Release. Явный `--upgrade` по умолчанию обновляет только Node, сервис и файлы support, сохраняя существующие ресурсы core; для синхронизации добавьте `--upgrade-xray`. Если существует только частичная установка, `--install` следует пути восстановления установки и не принимает неполное состояние за обычное обновление.

## Структура файловой системы

| Путь | Владелец или назначение |
| --- | --- |
| `/usr/local/bin/remnanode-lite` | Основная программа Node. |
| `/usr/local/bin/remnanode-xlogs` | Просмотр stdout rw-core. |
| `/usr/local/bin/remnanode-xerrors` | Просмотр stderr rw-core. |
| `/etc/remnanode/node.env` | `root:remnanode 0640`; конфигурация времени выполнения. |
| `/etc/remnanode/secret.key` | `root:remnanode 0640`; Secret Panel. |
| `/usr/local/lib/remnanode/rw-core` | Частный rw-core проекта. |
| `/usr/local/lib/remnanode/support/<tag>` | Сервис и support установщика, соответствующие установленному Release. |
| `/usr/local/lib/remnanode/support-current` | Управляемая символическая ссылка на текущий каталог support. |
| `/usr/local/share/remnanode/xray` | Geo и необязательные данные zapret. |
| `/usr/local/share/remnanode/asn/asn-prefixes.bin` | Компактная база ASN. |
| `/var/lib/remnanode` | Рабочий каталог сервиса. Node не сохраняет здесь конфигурацию Xray от Panel. |
| `/var/log/remnanode` | Журналы rw-core; OpenRC также хранит здесь журналы supervisor. |
| `/run/remnanode` | Каталог Unix-сокета, очищаемый при перезагрузке. |
| `/var/lib/remnanode-installer` | Каталог скачивания, распаковки и транзакций, доступный только root. |
| `/run/lock/remnanode-installer.lock` | Блокировка, общая для всех изменяющих систему точек входа установщика. |

Проект не владеет общими путями `/usr/local/bin/xray` и `/usr/local/share/xray` и не удаляет их.

Определения сервисов в репозитории находятся в [`deploy/remnawave-node.service`](../../../deploy/remnawave-node.service) и [`deploy/remnawave-node.openrc`](../../../deploy/remnawave-node.openrc).

## Модель безопасности сервиса

Нативный сервис не работает от root. systemd и OpenRC используют выделенного пользователя `remnanode` и предоставляют только:

- `CAP_NET_ADMIN` для управления таблицей nftables проекта и уничтожения сокетов через `NETLINK_SOCK_DIAG`.
- `CAP_NET_BIND_SERVICE`, чтобы rw-core мог прослушивать порты с 1 по 1023.

systemd также применяет ограничивающий набор capabilities, `NoNewPrivileges`, системные каталоги только для чтения, ограничения namespace, syscall и address family, а также частные временные каталоги. OpenRC использует `supervise-daemon`, `no_new_privs` и пределы cgroup v2.

Менеджер сервисов не экспортирует `node.env`. Перед запуском rw-core Node удаляет из окружения дочернего процесса Secret Panel, путь к файлу Secret и путь к файлу конфигурации Node, затем передаёт пути ресурсов и внутренний токен, необходимые core.

## Управление сервисом

systemd:

```bash
sudo systemctl status remnawave-node
sudo systemctl restart remnawave-node
sudo systemctl stop remnawave-node
sudo journalctl -u remnawave-node -f
```

OpenRC:

```bash
rc-service remnawave-node status
rc-service remnawave-node restart
rc-service remnawave-node stop
tail -F /var/log/remnanode/openrc.log
```

На обеих платформах журналы rw-core можно просматривать командами:

```bash
remnanode-xlogs
remnanode-xerrors
```

После перезапуска сервиса Node сначала сообщает, что rw-core offline, и ждёт нового запроса start от Panel. Это ожидаемо и не означает потерю локальной конфигурации или ошибку запуска сервиса.

## Обновление

Выберите тег целевого Release:

```bash
target_tag='vX.Y.Z-rnl.N' # or vX.Y.Z

curl -fsSL \
  "https://raw.githubusercontent.com/luxiaba/remnanode-lite/${target_tag}/scripts/upgrade.sh" \
  | sudo env RNL_TAG="${target_tag}" bash -s -- --yes
```

По умолчанию обновляются только Node, сервис и файлы support, а установленный rw-core сохраняется. Если целевой Release явно требует соответствующий core или нужно обновить данные geo и ASN:

```bash
curl -fsSL \
  "https://raw.githubusercontent.com/luxiaba/remnanode-lite/${target_tag}/scripts/upgrade.sh" \
  | sudo env RNL_TAG="${target_tag}" bash -s -- --yes --upgrade-xray
```

Транзакция обновления:

1. Записывает, работал ли сервис; при делегировании из install также записывает, был ли сервис включён при загрузке.
2. Сохраняет резервные копии бинарного файла, определения сервиса, файлов support, `node.env`, `secret.key` и необязательных ресурсов rw-core, geo и ASN.
3. Останавливает Node и процесс rw-core, указанный конфигурацией, и подтверждает их завершение.
4. Атомарно заменяет целевые файлы и мигрирует поддерживаемую устаревшую конфигурацию.
5. Восстанавливает работающее состояние только тогда, когда сервис работал до обновления или делегированная установка требует его запуска.
6. Проверяет версию бинарного файла и фиксирует транзакцию только после того, как ровно один целевой процесс владеет настроенным портом.

Явное обновление оставляет ранее остановленный сервис остановленным. Любая ошибка проверки запускает попытку восстановления исходных файлов, регистрации при загрузке и рабочего состояния. Если откат не завершён полностью, резервная копия остаётся в каталоге установщика, доступном только root, а операция завершается с ненулевым кодом.

Изменение `node.env` или Secret не требует повторной установки. Обновите файлы с правильными правами, как описано в [русском справочнике по конфигурации](configuration.md), затем перезапустите сервис.

## Откат на предыдущую версию

Используйте только старый тег, для которого проект действительно выпустил Release:

```bash
old_tag='vX.Y.Z-rnl.N' # or vX.Y.Z

curl -fsSL \
  "https://raw.githubusercontent.com/luxiaba/remnanode-lite/${old_tag}/scripts/upgrade.sh" \
  | sudo env RNL_TAG="${old_tag}" bash -s -- --yes
```

Добавьте `--upgrade-xray`, если старой версии требуется соответствующий rw-core. Перед откатом прочитайте примечания обоих Releases и убедитесь в совместимости конфигурации и базовых контрактов.

## Удаление

Предпочтительно использовать скрипт support, установленный с текущей версией:

```bash
sudo bash /usr/local/lib/remnanode/support-current/scripts/uninstall.sh
```

Неинтерактивные режимы:

| Режим | Команда | Сохраняемые данные |
| --- | --- | --- |
| Сохранить конфигурацию | `--keep-config --yes` | `node.env`, Secret, журналы, данные и rw-core/geo/ASN. |
| Очистить данные времени выполнения | `--purge --yes` | rw-core/geo/ASN. |
| Удалить все ресурсы проекта | `--full` | Конфигурация, журналы, данные и rw-core/geo/ASN проекта не сохраняются. |
| Предварительный просмотр | Добавьте `--dry-run` | Изменения не выполняются. |

Файлы удаляются только после подтверждения, что менеджер сервисов остановился, а целевые процессы Node и rw-core завершились. Установщик также удаляет частную таблицу nftables проекта, но не завершает посторонние процессы со схожими именами и не удаляет общие пути Xray.

Даже с `--full` сохраняются следующие системные элементы:

- системные пользователь и группа `remnanode`;
- общие системные пакеты, установленные установщиком;
- marker-каталог `/var/lib/remnanode-installer`, доступный только root.

Эти элементы облегчают безопасную повторную установку. Они также означают, что `--full` не возвращает хост в состояние, в котором проект никогда не был установлен.

## Дальнейшая эксплуатация

Проверки состояния, бюджеты журналов, политика обновления и диагностика описаны в [русском руководстве по эксплуатации](operations.md).
