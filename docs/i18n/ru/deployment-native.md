<!-- translation: locale=ru; source=docs/deployment-native.md; source-sha256=dba090d727b843193d91bac9991d8d69f4c1d5702258022ef6421191c38936df -->

# Нативное развёртывание Linux

> Русский перевод; при изменении правил используйте [английский оригинал](../../deployment-native.md).

[Индекс документации](README.md) · [Конфигурация](configuration.md) · [Эксплуатация](operations.md) · [Версионирование](versioning.md)

Нативный вариант запускает `remnanode-lite` непосредственно через systemd или OpenRC. Он подходит для небольших хостов, где Docker нельзя установить или где постоянные расходы Docker Engine daemon и container runtime не подходят. Native не означает отсутствие фоновой службы: `remnanode-lite` всё равно работает под systemd или OpenRC. Для большинства узлов Docker Compose остаётся вариантом по умолчанию. Самодостаточные Native lifecycle bundle распространяются как assets GitHub Release с точным тегом.

Каждый опубликованный bundle содержит Node, `rnlctl`, rw-core, GeoIP, GeoSite, данные ASN, определения служб, лицензии и SPDX SBOM. Manifest фиксирует digest каждого файла. Установщик сначала проверяет digest внешнего архива и только затем изменяет хост.

Установка и обновление принимают только точную версию Release с Native lifecycle assets. Release пригоден для Native, только если содержит `install.sh`, `SHA256SUMS` и архив для архитектуры хоста. Имена движущихся каналов `latest`, `preview`, `edge` и `sha-*` для Native недопустимы.

## Поддерживаемые хосты

| Хост | Менеджер служб | Уровень поддержки |
| --- | --- | --- |
| Rocky Linux 9 | systemd | Основная цель |
| Rocky Linux 8 | systemd 239 | Совместим; новый hardening drop-in автоматически не устанавливается |
| Debian 12 | systemd | Совместим |
| Другие актуальные дистрибутивы с systemd | systemd | Должны работать, но сначала проверьте конкретный образ |
| OpenRC с доступными контроллерами cgroup v2 | OpenRC | Экспериментально |

Native lifecycle bundle собираются для Linux `amd64` и `arm64`. Служба ограничена `448 MiB RAM`, без дополнительного swap, `1 CPU` и `256 tasks`, чтобы оставить запас на хосте `512 MiB / 1 vCPU / 2 GB`. OpenRC дополнительно требует `supervise-daemon`, `checkpath`, `rc-update`, cgroup v2, доступные для записи контроллеры memory/CPU/PID и `cgroup.kill`; при отсутствии этих условий запуск блокируется.

Установщик не меняет репозитории пакетов, sysctl, firewall, SELinux или синхронизацию времени. Это ответственность администратора хоста.

## Предварительные требования

Запускайте установщик от root. Для онлайн-установки нужны systemd (либо описанная экспериментальная среда OpenRC), `nft` из nftables, `ss` из iproute2, `useradd` и `groupadd`, если выделенная учётная запись `remnanode-lite` ещё не существует, доверенное хранилище CA, `curl` или `wget`, GNU tar и gzip. Порт Node должен быть доступен Panel, а входящие proxy-порты — соответствовать конфигурации Panel.

Rocky Linux 8/9:

```bash
sudo dnf install -y ca-certificates curl nftables iproute
```

Debian 12:

```bash
sudo apt-get update
sudo apt-get install -y ca-certificates curl nftables iproute2
```

Поддерживайте правильное системное время: неправильные часы ломают mTLS и JWT.

## Установка точной версии

Выберите опубликованную версию на странице GitHub Releases, затем скачайте installer и список digest из этого точного Release, проверьте installer и запустите его. Версия исходного кода и образ-кандидат не являются загружаемым Native bundle:

```bash
VERSION="<published-version>" # например: X.Y.Z или X.Y.Z-rnl.N
BASE="https://github.com/luxiaba/remnanode-lite/releases/download/v${VERSION}"

workdir="$(mktemp -d /var/tmp/remnanode-lite-download.XXXXXX)"
trap 'rm -rf "$workdir"' EXIT
cd "$workdir"
curl -fLO "${BASE}/install.sh"
curl -fLO "${BASE}/SHA256SUMS"
grep '  install.sh$' SHA256SUMS | sha256sum --check --strict -

sudo sh ./install.sh --version "$VERSION" --port 38329
```

Замените `38329` портом, настроенным для этого Node в Panel. Если действующий Secret ещё не установлен, installer запросит его без отображения на экране и попросит отдельное подтверждение. Онлайн installer скачивает только архив точной версии для данной архитектуры; он не следует за GitHub Latest или движущимся каналом образов.

### Неинтерактивная установка

Положите полный Secret Panel во временный обычный файл и передайте его через `--secret-file`. Флаг `--yes` пропускает только подтверждение:

```bash
umask 077
printf '%s\n' 'PASTE_THE_COMPLETE_PANEL_SECRET_KEY' >/root/remnanode-lite.secret

sudo sh ./install.sh \
  --version "$VERSION" \
  --port 38329 \
  --secret-file /root/remnanode-lite.secret \
  --yes

rm -f /root/remnanode-lite.secret
```

Не передавайте Secret аргументом командной строки: его могут увидеть список процессов и история shell.

### Подготовить без запуска

`--prepare-only` устанавливает и проверяет выпуск, но не включает и не запускает службу:

```bash
sudo sh ./install.sh --version "$VERSION" --port 38329 --prepare-only --yes
sudo rnlctl activate --secret-file /root/remnanode-lite.secret
```

Подготовленную установку нельзя запускать через `rnlctl start`: `activate` явно проверяет конфигурацию, включает службу, запускает её и ждёт внутреннего healthcheck.

## Офлайн-установка

С подключённой машины скачайте из одного Release и сохраните имена трёх файлов:

```text
install.sh
remnanode-lite_<version>_linux_<architecture>.tar.gz
SHA256SUMS
```

Проверьте их и перенесите на целевой хост:

```bash
grep -E '  (install\.sh|remnanode-lite_.*_linux_(amd64|arm64)\.tar\.gz)$' \
  SHA256SUMS | sha256sum --check --strict -
sudo sh ./install.sh \
  --bundle "./remnanode-lite_${VERSION}_linux_amd64.tar.gz" \
  --port 38329
```

Если `--sha256` не указан, installer берёт единственную совпадающую строку из `SHA256SUMS`, лежащего рядом с архивом. Для production лучше использовать архив и независимо загруженный checksum-файл, а не только распакованный каталог.

## Раскладка файлов

```text
/usr/local/sbin/rnlctl
/usr/local/bin/remnanode-lite -> /usr/local/lib/remnanode-lite/current/bin/remnanode-lite

/usr/local/lib/remnanode-lite/
├── current -> generations/<generation-id>
├── previous -> generations/<previous-id>       # появляется после первого обновления
└── generations/<generation-id>/

/etc/remnanode-lite/
├── node.env
└── secret.key

/var/lib/remnanode-lite/
/var/log/remnanode-lite/
/run/remnanode-lite/

/var/lib/remnanode-lite-installer/
├── state.json
├── journal.json                                # во время операции или восстановления
├── retained.json                               # может остаться после обычного удаления
├── bundles/
└── tmp/                                        # root-only временный каталог на диске
```

Installer предпочитает безопасный явно заданный `TMPDIR`. Иначе используется `/var/lib/remnanode-lite-installer/tmp`, а если его нельзя подготовить — `/var/tmp`. Workspace каждой операции имеет режим `0700` и удаляется при выходе. Это не даёт большому архиву попасть в `/tmp`, который на хосте 512 MiB может быть tmpfs.

`rnlctl` — отдельный обычный файл, принадлежащий root, а не ссылка в текущий generation. Поэтому инструмент восстановления остаётся доступным при проверке ссылок. Служба работает от непривилегированного пользователя и группы `remnanode-lite`; `uninstall --purge` удаляет только созданные этим установщиком и не изменённые identity.

Имена служб: `remnanode-lite.service` для systemd и `remnanode-lite` для OpenRC:

```bash
systemctl status remnanode-lite.service
rc-service remnanode-lite status
```

## Проверка после установки

```bash
sudo rnlctl status --json
sudo rnlctl doctor
sudo rnlctl logs node --lines 100
sudo rnlctl logs core-errors --lines 100
remnanode-lite version
```

`status --json` проверяет выбранный generation, конфигурацию, службу, права, cache восстановления и внутренний Unix socket. `doctor` выдаёт отдельный результат по каждой подсистеме. Эти команды не доказывают связь с Panel и работоспособность proxy-трафика: проверьте их отдельно.

| Состояние | Значение |
| --- | --- |
| `absent` | Управляемой Native-установки нет |
| `prepared` | Установлено и проверено, но намеренно выключено |
| `installed` | Файлы, состояние службы и health согласованы |
| `degraded` | Установка есть, но одна или несколько проверок не пройдены |
| `recovery-required` | Остался journal или нечитаемое состояние; нужен repair |

## Служба и журналы

```bash
sudo rnlctl start
sudo rnlctl stop
sudo rnlctl restart
sudo rnlctl logs node --follow
sudo rnlctl logs core --follow
sudo rnlctl logs core-errors --follow
```

Вывод Node через systemd попадает в journald; OpenRC использует `/var/log/remnanode-lite/openrc.log` и `openrc.err.log`. Вывод rw-core всегда находится в `/var/log/remnanode-lite/xray.out.log` и `xray.err.log`; `rnlctl logs` выбирает правильный backend и следует за ротацией.

## Обновление и откат

Онлайн-обновление принимает только точную опубликованную версию:

```bash
VERSION="<published-version>"
sudo rnlctl upgrade --to "$VERSION"
```

Для офлайн-обновления используйте проверенный архив:

```bash
sudo rnlctl upgrade \
  --bundle "./remnanode-lite_${VERSION}_linux_amd64.tar.gz" \
  --sha256 '<64-character-sha256>' \
  --expected-version "$VERSION"
```

Транзакция сохраняет состояние enabled/running, проверяет все файлы и ждёт внутренний health перед фиксацией. Хранятся только current и previous; не заменяйте бинарный файл прямой записью в `/usr/local/bin/remnanode-lite`.

Откат к сохранённому предыдущему generation:

```bash
sudo rnlctl rollback
sudo rnlctl rollback --to '<previous-generation-id>'
```

## Восстановление прерванной операции

Изменяющие команды используют root-only журнал и lock `/run/remnanode-lite-installer/operation.lock`. При сообщении о необходимости восстановления не удаляйте вручную lock, journal, generation или cache:

```bash
sudo rnlctl status --json
sudo rnlctl doctor
sudo rnlctl repair
```

Если cache повреждён, передайте архив уже записанной версии с `--expected-version`. `repair` восстанавливает состояние, но не выполняет незапрошенное обновление.

## Порт и Secret

`/etc/remnanode-lite/node.env` — файл данных, а не shell-скрипт. Secret хранится в `/etc/remnanode-lite/secret.key` с владельцем `root:remnanode-lite` и режимом `0640`. Для ротации создайте root-only временный файл, проверьте его и замените атомарно:

```bash
umask 077
secret_tmp="$(mktemp)"
printf '%s\n' 'PASTE_THE_NEW_COMPLETE_SECRET_KEY' >"$secret_tmp"
remnanode-lite validate-secret <"$secret_tmp"
sudo install -o root -g remnanode-lite -m 0640 \
  "$secret_tmp" /etc/remnanode-lite/secret.key.new
sudo mv -f /etc/remnanode-lite/secret.key.new /etc/remnanode-lite/secret.key
rm -f "$secret_tmp"
sudo rnlctl restart
```

При изменении `NODE_PORT` одновременно обновите Panel и firewall, затем выполните `sudo rnlctl doctor` и `sudo rnlctl restart`.

## Удаление

Обычное удаление убирает службу, бинарные файлы, generation, runtime-состояние, журналы и cache установщика, но оставляет `/etc/remnanode-lite` для безопасной повторной установки:

```bash
sudo rnlctl uninstall
```

Для удаления конфигурации и метаданных явно подтвердите purge:

```bash
sudo rnlctl uninstall --purge --yes
```

Purge не удаляет системные пакеты, правила firewall, sysctl, сторонние установки Xray или данные администратора.

## Безопасность

- Каталог `/etc/remnanode-lite` должен иметь `root:remnanode-lite 0750`, файлы конфигурации и Secret — `0640`.
- В Native `node.env` не записывайте непустой `SECRET_KEY`; используйте `SECRET_KEY_FILE`.
- Службе нужны только `CAP_NET_ADMIN` и `CAP_NET_BIND_SERVICE`; не запускайте её от root для обхода ошибки capability.
- Перед массовым обновлением сохраните предыдущую точную версию и проверьте Panel и реальный трафик.
