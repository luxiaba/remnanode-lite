<!-- translation: locale=ru; source=docs/deployment-native.md; source-sha256=677c3ee233af2a465ca8508cd60c6cdf9496f87e618ec9c668afbb850275a974 -->

# Нативное развёртывание Linux

> Русский перевод; при изменении правил используйте [английский оригинал](../../deployment-native.md).

[Индекс документации](README.md) · [Конфигурация](configuration.md) · [Эксплуатация](operations.md) · [Версионирование](versioning.md)

Нативный вариант запускает `remnanode-lite` непосредственно через менеджер служб хоста. Он подходит для небольших серверов, где Docker нельзя установить или где постоянные расходы Docker Engine daemon и container runtime нежелательны. Native не означает отсутствие фоновой службы: `remnanode-lite` работает под systemd, а на подходящем хосте Alpine — под штатным OpenRC. Для большинства узлов Docker Compose остаётся вариантом по умолчанию. Самодостаточные Native lifecycle bundle публикуются как assets GitHub Release с точным тегом.

Каждый опубликованный bundle содержит Node, `rnlctl`, rw-core, GeoIP, GeoSite, данные ASN, определения служб, лицензии и SPDX SBOM. Manifest фиксирует digest каждого файла. Установщик сначала проверяет digest внешнего архива и только затем изменяет хост.

Установка и обновление принимают только точную версию Release с Native lifecycle assets. Release пригоден для Native, только если содержит `install.sh`, `SHA256SUMS` и архив для архитектуры хоста. Имена движущихся каналов `latest`, `preview`, `edge` и `sha-*` для Native недопустимы.

## Поддерживаемые хосты

| Хост | Менеджер служб | Уровень поддержки |
| --- | --- | --- |
| Rocky Linux 9 | systemd | Основная цель |
| Rocky Linux 8 | systemd 239 | Совместим; новый hardening drop-in автоматически не устанавливается |
| Debian 12 | systemd | Совместим |
| Другие актуальные дистрибутивы с systemd | systemd | Должны работать, но сначала проверьте конкретный образ |
| Alpine Linux 3.22.x (постоянная установка `sys`) | Штатный OpenRC | Поддерживается при выполнении предварительных условий |

Native lifecycle bundle собираются для Linux `amd64` и `arm64`. Служба ограничена `448 MiB RAM`, не использует дополнительный swap и получает не более `1 CPU` и `256 tasks`, чтобы оставить запас на хосте `512 MiB / 1 vCPU / 2 GB`.

Строка про Alpine относится только к указанной конфигурации; это не обещание поддержки любого дистрибутива с OpenRC. Требуется постоянная установка Alpine Linux 3.22.x типа `sys` на `amd64` или `arm64`, штатный OpenRC в роли PID 1, Linux 5.14 или новее и единая иерархия cgroup v2. Контроллеры `cpu`, `memory` и `pids`, файл `memory.swap.max`, родительский `cgroup.procs` и `cgroup.kill` группы службы должны быть доступны и пригодны для работы. В `start_pre` управляемая служба применяет точные лимиты, проверяет их и своё членство в cgroup; если хотя бы одно условие не выполнено, запуск блокируется.

Контейнеры, образы без init, а также вложенные или виртуализированные окружения, которые не предоставляют этот контракт cgroup, не считаются поддерживаемыми хостами Native Alpine. Вложенная полноценная виртуальная машина подходит только в том случае, если проходит те же проверки во время запуска; одного названия дистрибутива недостаточно. Не отключайте и не ослабляйте проверку службы ради запуска на ограниченном хосте.

Установщик не меняет репозитории пакетов, sysctl, firewall, SELinux или синхронизацию времени. Это ответственность администратора хоста.

## Предварительные требования

Запускайте установщик от root. Для онлайн-установки нужны systemd (либо описанная выше среда Alpine/OpenRC), `nft` из nftables, `ss` из iproute2, `useradd` и `groupadd`, если выделенная учётная запись `remnanode-lite` ещё не существует, доверенное хранилище CA, `curl` или `wget`, GNU tar и gzip. Порт Node должен быть доступен Panel, а входящие proxy-порты — соответствовать конфигурации Panel.

Rocky Linux 8/9:

```bash
sudo dnf install -y ca-certificates curl nftables iproute
```

Debian 12:

```bash
sudo apt-get update
sudo apt-get install -y ca-certificates curl nftables iproute2
```

Alpine Linux 3.22.x (оболочка root):

```bash
apk add --no-cache ca-certificates curl openrc shadow nftables iproute2 tar
rc-update add cgroups boot
rc-service cgroups start
```

В Alpine пакет `shadow` предоставляет команды `useradd` и `groupadd`, а пакет `tar` — GNU tar. `tar` из BusyBox недостаточно для строгой распаковки Native bundle. OpenRC сам предоставляет `checkpath` как внутреннюю вспомогательную программу в окружении `openrc-run`; это не отдельная зависимость, и искать её в обычном пользовательском `PATH` не нужно.

Поддерживайте правильное системное время: неправильные часы ломают mTLS и JWT.

## Установка точной версии

Выберите опубликованную версию на странице GitHub Releases, затем скачайте installer и список digest из этого точного Release, проверьте installer и запустите его. Версия исходного кода и образ-кандидат не являются загружаемым Native bundle:

```bash
VERSION="<published-version>" # например: X.Y.Z или X.Y.Z-rnl.N
BASE="https://github.com/luxiaba/remnanode-lite/releases/download/${VERSION}"

workdir="$(mktemp -d /var/tmp/remnanode-lite-download.XXXXXX)"
trap 'rm -rf "$workdir"' EXIT
cd "$workdir"
curl -fLO "${BASE}/install.sh"
curl -fLO "${BASE}/SHA256SUMS"
grep '  install.sh$' SHA256SUMS | sha256sum -c -

sudo sh ./install.sh --version "$VERSION" --port 2222
```

Замените `2222` портом, настроенным для этого Node в Panel. Если действующий Secret ещё не установлен, installer запросит его без отображения на экране и попросит отдельное подтверждение. Онлайн installer скачивает только архив точной версии для данной архитектуры; он не следует за GitHub Latest или движущимся каналом образов.

### Неинтерактивная установка

Положите полный Secret Panel во временный обычный файл и передайте его через `--secret-file`. Флаг `--yes` пропускает только подтверждение:

```bash
umask 077
printf '%s\n' 'PASTE_THE_COMPLETE_PANEL_SECRET_KEY' >/root/remnanode-lite.secret

sudo sh ./install.sh \
  --version "$VERSION" \
  --port 2222 \
  --secret-file /root/remnanode-lite.secret \
  --yes

rm -f /root/remnanode-lite.secret
```

Не передавайте Secret аргументом командной строки: его могут увидеть список процессов и история shell.

### Подготовить без запуска

`--prepare-only` устанавливает и проверяет выпуск, но не включает и не запускает службу:

```bash
sudo sh ./install.sh --version "$VERSION" --port 2222 --prepare-only --yes
sudo rnlctl activate --secret-file /root/remnanode-lite.secret
```

Подготовленную установку нельзя запускать через `rnlctl start`: `activate` явно проверяет конфигурацию, включает службу, запускает её и ждёт внутреннего healthcheck.

`--prepare-only` проверяет и раскладывает файлы Release, но не запускает службу. Поэтому команда может успешно завершиться на хосте, который не соответствует контракту cgroup для Alpine/OpenRC. `rnlctl activate` впервые окончательно проверяет именно runtime-контракт cgroup и лимитов управляемой службы: OpenRC выполняет `start_pre`, применяет и проверяет лимиты и блокирует запуск, если эти возможности недоступны. Версию Alpine, постоянную установку `sys`, OpenRC в роли PID 1 и версию ядра по-прежнему проверяет оператор и release acceptance; `activate` не определяет эти условия вместо оператора.

## Офлайн-установка

С подключённой машины скачайте из одного Release и сохраните имена трёх файлов:

```text
install.sh
remnanode-lite_<version>_linux_<architecture>.tar.gz
SHA256SUMS
```

Проверьте их и перенесите на целевой хост:

```bash
VERSION="<опубликованная-версия>"
ARCH="<amd64-или-arm64>" # архитектура целевого хоста
ARCHIVE="remnanode-lite_${VERSION}_linux_${ARCH}.tar.gz"
awk '$2 == "install.sh"' SHA256SUMS | sha256sum -c -
awk -v archive="$ARCHIVE" '$2 == archive' SHA256SUMS | sha256sum -c -
```

На целевом хосте снова задайте версию и архитектуру:

```bash
VERSION="<опубликованная-версия>"
ARCH="<amd64-или-arm64>"
ARCHIVE="remnanode-lite_${VERSION}_linux_${ARCH}.tar.gz"
sudo sh ./install.sh \
  --bundle "./${ARCHIVE}" \
  --port 2222
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

`rnlctl` — отдельный обычный файл, принадлежащий root, а не ссылка в текущий generation. Поэтому инструмент восстановления остаётся доступным при проверке ссылок. Служба работает от имени пользователя и группы `remnanode-lite`; интерактивный вход для этой учётной записи запрещён. `uninstall --purge` удаляет только созданные этим установщиком и не изменённые identity.

Имена служб: `remnanode-lite.service` для systemd и `remnanode-lite` для OpenRC:

```bash
systemctl status remnanode-lite.service
rc-service remnanode-lite status
```

## Проверка после установки

```bash
sudo rnlctl status
sudo rnlctl status --json
sudo rnlctl doctor
sudo rnlctl logs node --lines 100
sudo rnlctl logs core-errors --lines 100
remnanode-lite version
```

Теперь `rnlctl status` без параметров выводит единообразную и понятную сводку жизненного цикла, а не перенаправляет сырой вывод `systemctl status` или `rc-service status`. Формат для человека не является интерфейсом для разбора. Существующим скриптам следует использовать `status --json`, схема которого не изменилась. Для низкоуровневых сведений запускайте команды менеджера служб, приведённые выше.

Status проверяет выбранный generation, конфигурацию, службу, права, cache восстановления и внутренний Unix socket. `doctor` выдаёт отдельный результат по каждой подсистеме, затем итог и упорядоченные подсказки `Next` для известных сбоев; существующая схема `doctor --json` не изменилась. Эти команды не доказывают связь с Panel и работоспособность трафика прокси: проверьте их отдельно.

| Состояние | Значение |
| --- | --- |
| `absent` | Управляемой Native-установки нет |
| `prepared` | Установлено и проверено, но намеренно выключено |
| `installed` | Файлы, состояние службы и health согласованы |
| `degraded` | Установка есть, но одна или несколько проверок не пройдены |
| `recovery-required` | Остался journal или нечитаемое состояние; нужен repair |

## Работа в командной строке

Глобальные параметры можно ставить в любом месте до или после команды и подкоманды:

```bash
sudo rnlctl --quiet config set LOW_MEMORY=1
sudo rnlctl status --no-color
```

`--quiet` (или `-q`) скрывает сообщения об успешных изменениях жизненного цикла и конфигурации, строку `configuration ok` от `config check`, а также человекочитаемый вывод `status` и `doctor`. Он не скрывает help, version, `config show`/`get`, журналы, сценарии автодополнения, планы dry-run, JSON или ошибки.

Status и doctor используют сдержанные цвета только тогда, когда stdout подключён к TTY. Перенаправление вывода, `--no-color`, непустая переменная `NO_COLOR` или `TERM=dumb` полностью отключают ANSI-последовательности.

Обычно код `0` означает успех, `1` — ошибку выполнения или нездоровое состояние, а `2` — ошибку в аргументах. `absent` является допустимым status и возвращает `0`; если скрипту нужна именно установленная служба, он должен также проверить `installed` или `deployment` в JSON. После запуска `journalctl` или `tail` команда `logs` возвращает код завершения этого процесса, в том числе `128 + signal` при завершении сигналом.

### Автодополнение команд оболочки

`rnlctl completion bash|zsh|fish` только печатает сценарий автодополнения в stdout. Команда не устанавливает файлы и не изменяет настройки запуска оболочки.

Для Bash с `bash-completion` установите файл в пользовательский каталог XDG:

```bash
bash_dir="${BASH_COMPLETION_USER_DIR:-${XDG_DATA_HOME:-$HOME/.local/share}/bash-completion}/completions"
mkdir -p "$bash_dir"
/usr/local/sbin/rnlctl completion bash >"$bash_dir/rnlctl"
```

Откройте новый сеанс Bash после загрузки `bash-completion`. Для текущего сеанса используйте fallback ниже; эту же строку можно самостоятельно добавить в `.bashrc`:

```bash
source <(/usr/local/sbin/rnlctl completion bash)
```

Для Zsh поместите `_rnlctl` в пользовательский каталог `fpath`:

```zsh
mkdir -p ~/.zfunc
/usr/local/sbin/rnlctl completion zsh > ~/.zfunc/_rnlctl
```

Добавьте каталог в `fpath` до существующего вызова `compinit`. Если автодополнение ещё не инициализируется в вашей конфигурации, выполните также две последние строки:

```zsh
fpath=(~/.zfunc $fpath)
autoload -Uz compinit
compinit
```

Fish напрямую загружает файлы из пользовательского каталога автодополнения:

```fish
mkdir -p ~/.config/fish/completions
/usr/local/sbin/rnlctl completion fish > ~/.config/fish/completions/rnlctl.fish
```

Сгенерированный сценарий статичен: он не запрашивает Releases, generation ID или состояние службы и не содержит Secret либо внутренних имён конфигурации. После обновления `rnlctl` создайте файл заново. Автодополнение после `sudo` зависит от настроек оболочки пользователя и его framework; сами сценарии этого не гарантируют.

## Служба и журналы

```bash
sudo rnlctl start
sudo rnlctl stop
sudo rnlctl restart
sudo rnlctl logs node --follow
sudo rnlctl logs core --follow
sudo rnlctl logs core-errors --follow
```

Вывод Node через systemd попадает в journald. Его можно ограничить положительной длительностью в формате Go и сочетать этот фильтр с числом строк и follow:

```bash
sudo rnlctl logs node --since 15m --lines 100
sudo rnlctl logs node --since 15m --follow
```

`--lines` по умолчанию равен `50` и принимает `1..100000`. `--since` поддерживается только для журналов Node в systemd и принимает положительные длительности Go, например `15m` или `2h`, но не абсолютную дату или `1d`. У журналов Node в OpenRC и файлов `core`/`core-errors` нет общего надёжного формата времени, поэтому они отклоняют этот параметр.

В systemd `--lines N` выбирает не более N записей суммарно для всего unit. В OpenRC читается по N строк из `openrc.log` и `openrc.err.log`. Для rw-core источникам соответствуют отдельные `xray.out.log` и `xray.err.log`. Файловые журналы читаются только по текущему пути: если текущий файл короче N строк, данные из `.1` не добавляются. `--follow` использует `tail -F` и продолжает чтение после последующей ротации.

## Обновление и откат

До изменения установки проверьте точный опубликованный кандидат:

```bash
VERSION="<published-version>"
sudo rnlctl upgrade --to "$VERSION" --dry-run
sudo rnlctl upgrade --to "$VERSION" --dry-run --json
```

Для предварительной проверки нужны права root, существующая согласованная установка и отсутствие незавершённого lifecycle journal. С `--to` она загружает полный кандидат в приватный временный workspace и статически проверяет его, а затем ненадолго получает lock жизненного цикла для проверки текущего состояния и известных требований к хосту. Команда не создаёт generation, cache или transaction journal, не переключает и не перезапускает службу, не исполняет бинарный файл кандидата, не проверяет health целевой версии и не сохраняет загруженный bundle. `--json` допускается только вместе с `--dry-run`.

Dry-run использует временный диск, но не резервирует и не гарантирует место для настоящего обновления. Он также не гарантирует, что состояние хоста не изменится или что последующее обновление обязательно завершится успешно. Тем же параметром можно проверить локальные `--bundle` с `--sha256` или `--bundle-root`. После проверки плана явно запустите обновление:

```bash
sudo rnlctl upgrade --to "$VERSION"
```

Для офлайн-обновления используйте проверенный архив:

```bash
VERSION="<опубликованная-версия>"
ARCH="<amd64-или-arm64>"
ARCHIVE="remnanode-lite_${VERSION}_linux_${ARCH}.tar.gz"
sudo rnlctl upgrade \
  --bundle "./${ARCHIVE}" \
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

Все изменения lifecycle и конфигурации удерживают lock `/run/remnanode-lite-installer/operation.lock`. Переходы generation и состояния службы дополнительно записываются в устойчивый journal. Изменения конфигурации и Secret используют атомарную замену файла и восстановление в рамках текущего процесса, описанное ниже; crash-safe journal для них не создаётся. Если lifecycle-команда сообщает о необходимости восстановления, не удаляйте вручную lock, journal, generation или cache:

```bash
sudo rnlctl status --json
sudo rnlctl doctor
sudo rnlctl repair
```

Если cache повреждён, передайте архив уже записанной версии с `--expected-version`. `repair` восстанавливает состояние, но не выполняет незапрошенное обновление.

## Порт и Secret

`/etc/remnanode-lite/node.env` — единственный источник настроек Native-службы. `rnlctl config` служит безопасным интерфейсом к этому файлу, а не отдельным хранилищем. Он открывает только шесть администраторских полей из [справочника конфигурации](configuration.md) и не показывает Secret или внутренние управляемые поля.

Для активного Node порт можно изменить и применить одной командой:

```bash
sudo rnlctl config set NODE_PORT=2222 --apply
```

Одновременно задайте тот же порт в Panel и firewall хоста: host networking не выполняет трансляцию портов.

Для ротации поместите полный новый Secret во временный обычный файл, доступный только root, затем передайте его `rnlctl` для проверки и установки:

```bash
umask 077
sudo install -m 0600 /dev/null /root/new-node-secret.key
sudoedit /root/new-node-secret.key
sudo rnlctl secret set --file /root/new-node-secret.key --apply
sudo rm -f /root/new-node-secret.key
```

Значение Secret не попадает в `node.env`, аргументы или вывод. Если после изменения файла перезапуск или внутренняя проверка состояния в `set --apply`, `unset --apply` или `secret set --apply` завершается ошибкой, `rnlctl` пытается вернуть прежний файл и восстановить активную службу со старой конфигурацией. Это лишь попытка восстановления в рамках текущего процесса, а не журналируемая транзакция, устойчивая к сбою процесса или системы.

`--apply` доступен только для активной управляемой службы. Для остановленной службы измените значение без `--apply`, затем выполните `rnlctl start`. Для установки в состоянии `prepared` также сначала измените значение без `--apply`, затем выполните `rnlctl activate`; Secret можно передать при активации через `rnlctl activate --secret-file PATH`. В остановленном состоянии или состоянии `prepared` параметр `--apply` отклоняется до записи файла.

Ручное редактирование `node.env` поддерживается. Сохраните владельца и режим `root:remnanode-lite 0640`, выполните `sudo rnlctl config check`, а для активной установки затем `sudo rnlctl config apply`. Эта команда проверяет файл, перезапускает службу и ждёт внутреннюю проверку состояния, но не может откатить ручную правку, поскольку снимка прежнего файла нет. Ни `check`, ни `apply` не проверяют связь с Panel или трафик прокси.

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

Оба варианта удаления убирают управляемый unit и управляемый drop-in
`20-remnanode-lite-hardening.conf`. Ожидаемый пустой каталог drop-in также
удаляется. Локальное override, например `90-local.conf`, или необычный объект
каталога намеренно остаётся нетронутым.

## Безопасность

- Каталог `/etc/remnanode-lite` должен иметь `root:remnanode-lite 0750`, файлы конфигурации и Secret — `0640`.
- В Native `node.env` не записывайте непустой `SECRET_KEY`; используйте `SECRET_KEY_FILE`.
- Управляемый процесс Node работает от имени `remnanode-lite` и получает только `CAP_NET_ADMIN` и `CAP_NET_BIND_SERVICE`. В OpenRC `supervise-daemon`, работающий от root, остаётся частью менеджера служб; не запускайте сам процесс Node от root для обхода ошибки capability.
- Перед массовым обновлением сохраните предыдущую точную версию и проверьте Panel и реальный трафик.
