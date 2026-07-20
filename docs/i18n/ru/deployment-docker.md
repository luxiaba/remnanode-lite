<!-- translation: locale=ru; source=docs/deployment-docker.md; source-sha256=b320cff90cf77b20ce5e32d2c0988c60f3f6fb160eb43a7549ef9ad9e3c70e43 -->
# Развёртывание Docker Compose

> Это русский перевод. Если правила развёртывания расходятся с [английским руководством](../../deployment-docker.md), используйте английский оригинал.

[К индексу документации на русском](README.md)

Docker Compose является предпочтительным способом развёртывания узлов с малым объёмом памяти. Серверу нужны только YAML-файл с ограниченными правами и Docker Engine; исходный код, инструментарий Go и постоянный том журналов не требуются.

Основной сценарий на этой странице использует однофайловый Compose, подходящий для множества независимых небольших узлов. Файлы `compose.yaml` и `.env` в корне репозитория остаются вариантом для централизованной конфигурации или локальной сборки из исходного кода.

## Модель развёртывания

В контейнере работает один диспетчер приложения: `remnanode-lite` напрямую запускает и завершает rw-core, без s6 или другого постоянно работающего supervisor. Compose включает минимальный init для обязанностей PID 1, а Node и rw-core совместно используют одну cgroup контейнера со следующими ограничениями:

- жёсткий предел памяти `448 MiB` без дополнительного swap;
- `1 CPU` и `256 PIDs`;
- корневая файловая система только для чтения;
- tmpfs для `/run/remnanode`, `/tmp` и `/var/log/remnanode` с общим пределом `48 MiB`;
- ротация журналов Node драйвером Docker `json-file` по схеме `2 MiB x 2`;
- без постоянного тома данных. Пересоздание контейнера очищает копии конфигурации времени выполнения и журналы, после чего Panel повторно отправляет конфигурацию Xray.

Эти пределы оставляют ресурсы хосту в рамках целевой машины `512 MiB RAM / 1 vCPU / 2 GB disk`. Они не являются SLA для любого профиля трафика или набора плагинов. Измерения и границы приведены в [каноническом английском документе о бюджете ресурсов](../../development/resource-budget.md).

## Выбор образа

Публичный образ в GHCR доступен для анонимного скачивания:

```text
ghcr.io/luxiaba/remnanode-lite
```

| Тег | Поведение | Рекомендуемое применение |
| --- | --- | --- |
| `X.Y.Z-rnl.N` | Независимая итерация проекта, прошедшая процесс выпуска | Рекомендуется для production и точного отката |
| `X.Y.Z` | Формальная сборка, согласованная с соответствующей официальной версией | Рекомендуется для production |
| `latest` | Последняя стабильная сборка, прошедшая процесс выпуска | Осознанное слежение за стабильным каналом; не идентификатор отката |
| `sha-<40-character-commit>` | Кандидат, собранный для commit в `main` | Найти кандидат, затем разрешить и зафиксировать digest |
| `candidate-sha-<40-character-commit>` | Независимо пересобранный кандидат, вручную запущенный из `main` | Найти ручную пересборку, затем разрешить и зафиксировать digest |
| `edge` | Перемещаемый кандидат текущего `main` | Только кратковременное наблюдение |

По политике проекта точные версии, `sha-*` и `candidate-sha-*` намеренно не перемещаются, однако теги registry технически не являются неизменяемыми. Для наиболее строгой фиксации используйте digest манифеста в форме `name@sha256:...`. До первого формального Release теги `latest` и точной версии не существуют. Выберите реального кандидата в [пакете GHCR](https://github.com/luxiaba/remnanode-lite/pkgs/container/remnanode-lite) и сохраните его manifest digest.

Правила именования и продвижения описаны в [русской модели версий](versioning.md).

## Предварительные требования

- Linux `amd64` или `arm64`.
- Docker Engine с Compose v2, вызываемым как `docker compose`.
- Созданный в Panel узел и его полный `SECRET_KEY`.
- Порт Node в Panel совпадает с `NODE_PORT`.
- Firewall хоста разрешает Panel доступ к порту Node API и открывает входящие proxy-порты, необходимые фактической конфигурации.

Compose использует `network_mode: host`; не добавляйте `ports:`. Контейнер имеет `NET_ADMIN`, поэтому может управлять частной таблицей nftables проекта и закрывать соединения в сетевом namespace хоста. Запускайте только доверенные образы.

## Однофайловое развёртывание

Для рабочего узла используйте файл Compose из того же Release, что и образ. Он включён в `SHA256SUMS` выпуска и уже ссылается на точную версию.

Скачайте однофайловый артефакт и контрольные суммы из одного и того же GitHub Release:

```bash
VERSION=X.Y.Z-rnl.N # or X.Y.Z
BASE_URL="https://github.com/luxiaba/remnanode-lite/releases/download/v${VERSION}"

mkdir -p /opt/remnanode
cd /opt/remnanode
curl -fL "${BASE_URL}/docker-compose.single-file.yaml" -o docker-compose.yaml
curl -fLO "${BASE_URL}/SHA256SUMS"
grep -F ' docker-compose.single-file.yaml' SHA256SUMS \
  | sed 's|docker-compose.single-file.yaml|docker-compose.yaml|' \
  | sha256sum --check --strict
chmod 600 docker-compose.yaml
```

В примере используется GNU `sha256sum`, доступный на поддерживаемых Linux-хостах. После проверки укажите порт Node и Secret. Меняйте образ на `latest` только в том случае, если действительно хотите следовать за стабильным каналом.

Отредактируйте следующие поля:

```yaml
image: ghcr.io/luxiaba/remnanode-lite:X.Y.Z-rnl.N

environment:
  NODE_PORT: "38329"
  SECRET_KEY: "PASTE_THE_COMPLETE_BASE64_VALUE_FROM_THE_PANEL"
  LOW_MEMORY: "1"
```

Версия выше показывает только формат. Замените её точной версией, кандидатом `sha-*` или digest, который действительно существует в GHCR.

### Проверка кандидата

До первого официального выпуска или при проверке нового кандидата скачайте шаблон Compose из того же коммита, что и образ:

```bash
(
  set -euo pipefail
  candidate_tag=REPLACE_WITH_FULL_SHA_OR_CANDIDATE_SHA_TAG
  case "$candidate_tag" in
    sha-*) candidate_commit="${candidate_tag#sha-}" ;;
    candidate-sha-*) candidate_commit="${candidate_tag#candidate-sha-}" ;;
    *) echo "candidate tag must be sha-<commit> or candidate-sha-<commit>" >&2; exit 1 ;;
  esac
  printf '%s\n' "$candidate_commit" | grep -Eq '^[0-9a-f]{40}$'

  mkdir -p /opt/remnanode
  cd /opt/remnanode
  curl -fL \
    "https://raw.githubusercontent.com/luxiaba/remnanode-lite/${candidate_commit}/deploy/compose.single-file.yaml" \
    -o docker-compose.yaml
  sed -i \
    "s|ghcr.io/luxiaba/remnanode-lite:latest|ghcr.io/luxiaba/remnanode-lite:${candidate_tag}|" \
    docker-compose.yaml
  chmod 600 docker-compose.yaml
)
```

Выберите полный тег `sha-<40-character-commit>` или `candidate-sha-<40-character-commit>` в [пакете GHCR](https://github.com/luxiaba/remnanode-lite/pkgs/container/remnanode-lite). Для формальной приёмки определите digest манифеста и укажите в Compose `ghcr.io/luxiaba/remnanode-lite@sha256:<manifest-digest>`. Кандидаты предназначены для тестирования и не должны выдаваться за выпущенные версии.

### Синтаксис Secret

Переменные окружения должны быть заданы как отображение YAML (mapping):

```yaml
environment:
  SECRET_KEY: "eyJ..."
```

Не используйте форму списка:

```yaml
environment:
  - SECRET_KEY="eyJ..."
```

В форме списка кавычки становятся частью значения, что обычно приводит к ошибке:

```text
decode SECRET_KEY: illegal base64 data at input byte 0
```

При однофайловом развёртывании Secret виден в файле Compose и локальных метаданных `docker inspect`. Сохраняйте режим файла `0600` и ограничивайте доступ к Docker socket, резервным копиям и администрированию хоста. Перед запуском rw-core Node удаляет Secret Panel из окружения дочернего процесса.

## Запуск и проверка

```bash
cd /opt/remnanode
docker compose config --quiet
docker compose pull
docker compose up -d --no-build
docker compose ps
docker compose logs --tail=100 remnanode
ss -H -lnt "sport = :38329"
```

Не запускайте `docker compose config` без `--quiet` в автоматизированных журналах: команда разворачивает и печатает встроенный Secret.

Состояние `healthy` означает, что Node принял соединение через внутренний Unix-сокет. Всё равно проверьте Panel и реальный трафик: эта проверка не охватывает следующие условия:

- Panel может достичь Node по сети;
- mTLS, JWT или Secret корректны;
- rw-core находится online;
- входящие proxy-порты, отправленные Panel, доступны.

Нормально, если rw-core находится offline сразу после перезапуска Node. Node не восстанавливает старую конфигурацию Panel с диска. Следующий цикл проверки Panel снова вызывает `/node/xray/start`. Завершите проверку в Panel и протестируйте репрезентативный proxy-трафик.

## Миграция с официального контейнера

`NODE_PORT` и полный `SECRET_KEY` из официального образа `remnawave/node` остаются действительными. Они относятся к внешнему контракту Panel-to-Node, а не к внутренней структуре Node.js и s6 официального образа. Не запускайте оба контейнера во время миграции: при host networking они будут конкурировать за порт Node API и входящие proxy-порты.

1. Сделайте резервную копию текущего Compose и запишите точную версию официального образа как цель отката.
2. Замените определение сервиса полным однофайловым шаблоном с этой страницы. Сохраните как минимум host networking, обе capabilities, пределы ресурсов, корневую файловую систему только для чтения, tmpfs и пределы журналов.
3. Сохраните исходные `NODE_PORT` и Secret, но преобразуйте `environment` в отображение YAML и зафиксируйте образ на реальной версии проекта, кандидате `sha-*` или digest.
4. Скачайте и принудительно пересоздайте контейнер с тем же именем сервиса. Compose остановит старый контейнер перед созданием замены.

```bash
cd /opt/remnanode
docker compose config --quiet
docker compose pull
docker compose up -d --no-build --force-recreate
docker compose ps
docker compose logs --tail=100 remnanode
```

5. Убедитесь в Panel, что узел вернулся online, rw-core запустился, а репрезентативный proxy-трафик работает. Эта реализация пишет журналы rw-core в `/var/log/remnanode/xray.out.log` и `/var/log/remnanode/xray.err.log`, а не в `/var/log/xray/current` официального контейнера.

Переносить состояние времени выполнения контейнера или том конфигурации Xray не требуется: Panel отправит конфигурацию повторно. Для отката восстановите резервный Compose и точный официальный образ, затем повторите pull и recreate. Сохраняйте резервную копию, пока новый контейнер не завершит период наблюдения.

## Образы-кандидаты

После изменения контейнерных файлов в `main` workflow `container` собирает образы `linux/amd64` и `linux/arm64`, публикует многоархитектурный манифест и записывает происхождение сборки. Затем он публикует `sha-<commit>` и обновляет `edge`, если этот коммит всё ещё является вершиной `main`. Эти проверки показывают, как был собран образ, но не подтверждают его работу.

Перед выпуском `v2.8.0` требуется не менее 600 секунд проверки на реальном малоресурсном хосте `linux/amd64` с эталонным Compose и ссылкой по digest манифеста. Проверяются лимиты и состояние контейнера, память и PID, связь с Panel, запуск rw-core, реальный прокси-трафик, OOM и число перезапусков. Проверка `arm64-production-runtime`, нативная установка systemd/OpenRC, нагрузка в 50 000 пользователей, 24-часовой прогон и испытания отказа и отката остаются последующей работой и не блокируют `v2.8.0`. Полные требования и формат записи приведены в [протоколе приёмки (англ.)](../../development/release-acceptance.md#docker-production-smoke).

У кандидата нет артефактов GitHub Release, поэтому он не является официальным выпуском. Аттестации описывают цепочку сборки, а записи времени выполнения — наблюдения оператора; одно не заменяет другое.

## Фиксация digest и проверка provenance

После скачивания образа запишите его registry digest:

```bash
VERSION=X.Y.Z-rnl.N # or X.Y.Z
IMAGE="ghcr.io/luxiaba/remnanode-lite:${VERSION}"

DIGEST_REF="$(docker image inspect \
  --format '{{range .RepoDigests}}{{println .}}{{end}}' \
  "$IMAGE" | head -n 1)"
printf '%s\n' "$DIGEST_REF" \
  | grep -Eq '^ghcr\.io/luxiaba/remnanode-lite@sha256:[0-9a-f]{64}$'
```

Используйте полный результат в Compose:

```yaml
image: ghcr.io/luxiaba/remnanode-lite@sha256:...
```

При установленном GitHub CLI проверьте provenance, созданный этим репозиторием:

```bash
gh attestation verify \
  "oci://${DIGEST_REF}" \
  --repo luxiaba/remnanode-lite \
  --cert-identity https://github.com/luxiaba/remnanode-lite/.github/workflows/container.yml@refs/heads/main \
  --deny-self-hosted-runners
```

Тег обозначает версию, которую вы намерены использовать. Digest идентифицирует фактически развёрнутые байты. Управляемое обновление множества узлов должно сохранять digest.

## Обновление и откат

Сделайте резервную копию текущего YAML, измените `image:`, затем явно скачайте и пересоздайте контейнер:

```bash
cp -p docker-compose.yaml docker-compose.yaml.previous
docker compose config --quiet
docker compose pull
docker compose up -d --no-build --force-recreate
docker compose ps
docker compose logs --tail=100 remnanode
```

Для отката восстановите ранее проверенный YAML или верните `image:` к предыдущей точной версии либо digest, затем повторите `pull` и `up`. Никогда не реализуйте откат перемещением старого тега версии.

`latest` не заменяет работающий контейнер. Даже при слежении за ним нужны периодические явные pull и recreate с записью предыдущего digest перед каждым обновлением.

## Массовое развёртывание

На всех этапах массового развёртывания используйте один проверенный digest манифеста и сохраните предыдущий digest для отката. Точные теги версий удобнее читать, но в записях развёртывания всё равно следует хранить `name@sha256:...`. Не отправляйте `latest` или `edge` сразу на все узлы.

1. Разделите узлы по архитектуре, дистрибутиву, региону и основному профилю трафика. Для каждого узла запишите текущий digest, целевой digest и Compose для отката.
2. Начните с небольшой контрольной группы, представляющей основные сети и архитектуры. Дождитесь как минимум одного пика нагрузки и проверьте связь с Panel, синхронизацию rw-core, реальный прокси-трафик, память, перезапуски, процессы, диск и журналы.
3. Расширяйте по этапам примерно `5%`, `25%` и `50%`, затем разверните оставшиеся узлы. Завершайте период наблюдения каждого этапа до перехода к следующему. Размер партии должен позволять восстановить предыдущий digest в рамках того же окна обслуживания.
4. На каждом этапе выборочно проверяйте состояние контейнера и Panel, прокси-трафик, счётчики перезапусков и OOM, память, PID, диск, а также ошибки Xray или nft. Записывайте digest, установленный на каждом узле.
5. Остановитесь, если появились необъяснимые отключения узлов, ошибки прокси, повторные сбои запуска Xray, OOM, неожиданные перезапуски, зависшие процессы, нарушение лимитов или серия однотипных ошибок. Сначала откатите эту партию, затем сохраните журналы вместе с digest для диагностики.

Откат не зависит от перемещения тега registry. Восстановите для каждого узла записанный предыдущий Compose или digest, выполните `pull` и `up --force-recreate`, затем снова проверьте соединение с Panel и реальный трафик. Пока причина не получила ясного объяснения, не продолжайте обновление нетронутых узлов и не удаляйте предыдущий образ с canary.

Приёмка выпуска не заменяет поэтапное развёртывание. Добавьте ссылку на запись приёмки в примечания к выпуску, а затем наблюдайте за каждым этапом отдельно.

## Необязательная схема `.env`

Чтобы отделить нечувствительную структуру Compose от параметров узла, скачайте `compose.yaml`, шаблон окружения и контрольные суммы из одного формального GitHub Release. Не объединяйте Compose из будущего `main` со старым образом:

```bash
VERSION=X.Y.Z-rnl.N # or X.Y.Z
BASE_URL="https://github.com/luxiaba/remnanode-lite/releases/download/v${VERSION}"

curl -fLO "${BASE_URL}/compose.yaml"
curl -fLO "${BASE_URL}/remnanode.env.example"
curl -fLO "${BASE_URL}/SHA256SUMS"
grep -E ' (compose.yaml|remnanode.env.example)$' SHA256SUMS \
  | sha256sum --check --strict
mv remnanode.env.example .env
chmod 600 .env
```

Задайте как минимум:

```env
REMNANODE_IMAGE=ghcr.io/luxiaba/remnanode-lite:X.Y.Z-rnl.N
NODE_PORT=38329
SECRET_KEY=PASTE_THE_COMPLETE_VALUE
LOW_MEMORY=1
```

Сохраняйте `REMNANODE_IMAGE` на точной версии этого Release или замените его проверенным manifest digest. Все переменные описаны в [русском справочнике по конфигурации](configuration.md).

## Локальная сборка из исходного кода

Собирайте из исходного кода только для разработки, аудита или экстренного случая, когда registry недоступен:

```bash
git clone https://github.com/luxiaba/remnanode-lite.git
cd remnanode-lite
cp .env.example .env
chmod 600 .env
# Edit .env

docker compose -f compose.yaml -f compose.build.yaml build --pull
docker compose -f compose.yaml -f compose.build.yaml up -d --no-build
```

Не выполняйте сборку на production-узле с диском всего 2 GB. Инструментарий Go, базовые слои и cache BuildKit могут значительно превысить бюджет диска времени выполнения.

## Журналы и диск

Просмотр журналов процесса Node:

```bash
docker compose logs -f remnanode
```

Просмотр журналов rw-core:

```bash
docker exec -it remnanode tail -n 50 -F /var/log/remnanode/xray.out.log
docker exec -it remnanode tail -n 50 -F /var/log/remnanode/xray.err.log
```

Каждый поток rw-core ротируется при `4 MiB` и сохраняет один файл `.1` внутри tmpfs размером `28 MiB`; пересоздание контейнера очищает его. Docker ограничивает журналы Node `json-file` примерно до `2 MiB x 2`. Проект не требует постоянных журналов. Любой долговременный сбор должен укладываться в собственный дисковый бюджет хоста.

Проверка использования диска и удаление неиспользуемых образов:

```bash
docker system df
docker image ls ghcr.io/luxiaba/remnanode-lite
docker image prune
```

Перед очисткой запишите проверенный тег предыдущей версии или manifest digest и убедитесь, что соответствующий образ остаётся локально. Всегда сохраняйте как минимум этот один явный образ для отката. По умолчанию `docker image prune` удаляет только dangling images. Не используйте широкие параметры очистки, способные удалить единственную версию для отката. Повседневные команды и диагностика приведены в [русском руководстве по эксплуатации](operations.md).

## Содержимое образа и трассируемость

Текущий образ содержит:

- статически скомпонованный `remnanode-lite`;
- rw-core `v26.6.27`, закреплённый версией и digest ресурса;
- соответствующие `geoip.dat` и `geosite.dat`;
- компактную базу ASN, собранную из закреплённого commit `ipverse/as-ip-blocks`;
- среду выполнения Debian bookworm slim с CA-сертификатами и зависимостями nftables.

Базовые образы, rw-core и источник ASN закреплены с помощью digest или контрольной суммы. Пакеты Debian `apt` не привязаны к снимку репозитория, поэтому две сборки не обязаны совпадать побайтно. Для точной идентификации выпущенного артефакта используйте вместе digest манифеста, SBOM, provenance и attestation.
