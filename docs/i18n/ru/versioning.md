<!-- translation: locale=ru; source=docs/versioning.md; source-sha256=537ceb6d5ee75178d7d68eb114849d7a656d0efc51c5dcac090f9b15b064ae28 -->

# Версии и теги образов

[Английский оригинал](../../versioning.md) · [Индекс](README.md) · [Процесс выпуска, англ.](../../release.md)

Remnanode Lite отделяет идентичность проекта от заявлений о совместимости. Проект может
развиваться независимо, продолжая реализовывать более старый проверенный контракт официального
Node. Точные версии и движущиеся каналы образов также имеют разный смысл: exact version
обозначает один выпуск, а `preview` и `latest` выбирают класс выпуска.

Этот документ определяет версии и каналы. Исполняемым источником истины для публикации служит
release workflow; `scripts/release-metadata.sh` классифицирует stable и preview.

| Измерение | Источник истины | Значение |
| --- | --- | --- |
| Project `Version` | `internal/version/version.go` | Идентичность кода, бинарных файлов, Release assets и exact image tag |
| Official `ContractVersion` | `internal/version/contract.version`, `internal/version/version.go` и закреплённые contract evidence | Поведение официального Node, которое реально реализовано и сообщается Panel |
| Целевая Panel для интеграции | Проверка выпуска сопровождающим | Версия Panel, использованная в реальной интеграционной проверке; в идентичность выпуска не компилируется |
| rw-core и runtime assets | `release/runtime-assets.lock.json` | Точные core, GeoIP, GeoSite, ASN, исходники, лицензии и checksums, упакованные для Docker и Native Linux |

Например:

```text
Version:         2.8.0-rnl.1
ContractVersion: 2.8.0
```

Это первый preview проекта с полноценным Native Linux bundle при сохранении проверенного контракта официального Node `2.8.0`. Суффикс `rnl.1` принадлежит этому проекту и не является ревизией официального выпуска. Одно изменение `Version` не расширяет заявленный контракт.

Изменение `ContractVersion` требует закреплённого официального исходного кода, рассмотренного
contract delta, соответствующих изменений реализации и тестов и завершённой проверки
совместимости. Одно изменение `Version` никогда не расширяет заявленный контракт.

## Два класса выпусков

### Stable: `X.Y.Z`

Обычная версия означает стабильный выпуск, согласованный с официальным контрактом той же версии. Проверки репозитория требуют `Version == ContractVersion`.

Он публикуется как annotated Git tag `vX.Y.Z`, обычный GitHub Release, exact GHCR tag `X.Y.Z` и источник движущегося канала `latest`. GitHub помечает такой Release как Latest.

Таким образом, обычная версия одновременно является неизменяемой точкой выравнивания и выпуском,
который после успешной публикации выбирает стабильный канал.

### Preview: `X.Y.Z-rnl.N`

`rnl.N` — независимая итерация Remnanode Lite. Она может использоваться для разработки до официального релиза либо для улучшения архитектуры, доставки и ресурсов при сохранении старого контракта.

Preview публикуется как annotated tag `vX.Y.Z-rnl.N`, GitHub Prerelease, exact GHCR tag `X.Y.Z-rnl.N` и движущийся канал `preview`. Preview никогда не обновляет `latest` и не становится GitHub Latest. Даже после полного автоматического release workflow он остаётся preview до публикации обычной stable-версии.

В одной линии `X.Y.Z` номер `N` начинается с 1 и увеличивается. Опубликованные номера и exact tags не переиспользуются. Числовой префикс обозначает линию разработки проекта и сам по себе не заявляет реализацию такого же официального контракта.

## Текущая линия

| Версия | Контракт | Класс | Статус |
| --- | --- | --- | --- |
| `2.8.0` | `2.8.0` | Stable | Уже опубликован; существующие tag и история не меняются |
| `2.8.0-rnl.1` | `2.8.0` | Preview | Планируемый первый self-contained Native Linux bundle |

SemVer ставит `2.8.0-rnl.1` ниже `2.8.0`, даже если preview выпущен позже по календарю. Порядок SemVer не выбирает канал: workflow явно классифицирует tag как `preview` или `latest`.

Release preflight также сравнивает stable-версию с уже существующими stable Git tags и отклоняет
более низкую версию. Это предотвращает случайный откат `latest`, даже если синтаксис tag и контракт
сами по себе допустимы.

## Git tag и image tag

```text
Git tag:       v2.8.0-rnl.1
Container tag: ghcr.io/luxiaba/remnanode-lite:2.8.0-rnl.1
```

Git tag содержит префикс `v`, image tag — нет. Оба являются неизменяемыми
идентификаторами выпуска, но обозначают разные объекты:

- annotated Git tag определяет принятый из `main` source commit;
- exact container tag определяет уже собранный и аттестованный multi-architecture manifest
  этого коммита.

Release workflow не пересобирает контейнер: он проверяет кандидат `sha-<commit>`, созданный
из `main`, и присваивает тому же digest точный release tag. Правила репозитория должны
запрещать обновление и удаление `v*`; workflow также повторно разрешает удалённый annotated
tag перед созданием draft, публикацией Release и продвижением канала. Любое перемещение tag
приводит к закрытому отказу.

Для строгой фиксации используйте content address:

```text
ghcr.io/luxiaba/remnanode-lite@sha256:<manifest-digest>
```

## Ссылки на образы

| Ссылка | Изменяемость | Значение | Назначение |
| --- | --- | --- | --- |
| `sha-<40-character-commit>` | Не перемещается по политике | Аттестованный кандидат одного коммита `main` | Проверка выпуска, воспроизведение и диагностика |
| `edge` | Движется | Последний допустимый кандидат `main` | Только наблюдение основной линии |
| `X.Y.Z-rnl.N` | Не перемещается по политике | Один preview выпуск | Контролируемое preview и rollback |
| `preview` | Движется | Последний promoted preview | Добровольное слежение за preview |
| `X.Y.Z` | Не перемещается по политике | Один stable выпуск | Production и точный rollback |
| `latest` | Движется | Последний promoted stable | Добровольное слежение за stable |
| `name@sha256:...` | Content-addressed | Один manifest digest | Самая строгая фиксация развёртывания и проверки |

Обычный push в `main` может обновить `edge`, но не `preview` и не `latest`. Эти каналы
продвигает только release workflow после публикации соответствующего выпуска.

### Stable и preview никогда не пересекаются

Два движущихся канала намеренно разделены:

- `latest` указывает только на обычный stable `X.Y.Z`;
- `preview` указывает только на prerelease `X.Y.Z-rnl.N`;
- preview не может продвинуть, заменить или исправить `latest`;
- stable не может продвинуть `preview`.

Ни один движущийся tag не меняет уже запущенный контейнер. Docker проверяет tag только после
явного pull, а Compose пересоздаёт контейнер только по явной команде.

## Выбор Docker reference

Для production выбирайте exact stable:

```text
ghcr.io/luxiaba/remnanode-lite:2.8.0
```

Для самой строгой фиксации сохраните и используйте manifest digest:

```text
ghcr.io/luxiaba/remnanode-lite@sha256:<manifest-digest>
```

Preview используйте только осознанно:

```text
ghcr.io/luxiaba/remnanode-lite:2.8.0-rnl.1
```

`preview` удобен для краткой проверки, но для fleet лучше exact preview tag или digest: он не
может переместиться между обновлением разных узлов. Сохраните предыдущую exact reference для
rollback.

`latest` — добровольно выбранный стабильный канал обновлений, а не rollback identity. Даже при
его использовании прочитайте Release, запишите resolved digest и выполните обновление явно:

```bash
docker compose pull
docker compose up -d --no-build --force-recreate
```

Для rollback всегда используйте сохранённую точную версию или digest, а не историческое значение `latest`.

`sha-<commit>` предназначен для проверки кандидата, который может стать Release. Не используйте
`edge` для release acceptance: другой build из `main` может передвинуть его во время тестирования.

## Native принимает только exact versions

Native bundle связывает имя архива, `SHA256SUMS`, manifest, встроенную версию и source revision. Поэтому installer и `rnlctl` не принимают движущиеся каналы:

```bash
sudo sh install.sh --version 2.8.0-rnl.1
sudo rnlctl upgrade --to 2.8.0-rnl.2
```

`latest`, `preview`, `edge` и `sha-*` не являются допустимыми Native version inputs.

## Когда выпуск считается опубликованным

Строка версии в исходниках, кандидат `main` или один Git tag сами по себе не образуют полный
опубликованный выпуск.

Для preview нужны одновременно:

1. annotated `vX.Y.Z-rnl.N` на принятом коммите `main`;
2. опубликованный и проверенный GitHub Prerelease;
3. exact GHCR tag с тем же digest, что и кандидат;
4. успешное продвижение digest в `preview`.

Stable требует обычный `vX.Y.Z`, полный GitHub Release, exact image `X.Y.Z`, GitHub Latest и продвижение в `latest`.

Оба класса проходят одинаковые проверки кода, совместимости, assets, provenance и attestation.
Разница заключается в статусе выпуска и канале, а не в ослабленном пути сборки preview.

Перед тем как объявлять планируемую версию или URL assets доступными, проверьте Git tags,
GitHub Releases и точные GHCR tags.

## Синхронизация официальной версии

Плановый workflow только создаёт Issue при появлении новой официальной версии. Он не меняет
автоматически контракт, код, project version или image tags.

Синхронизация нового официального контракта требует закрепить официальную версию и неизменяемый
source commit; проверить routes, schemas, errors, side effects и зависимости plugins; обновить
contract evidence и tests; выровнять Go-реализацию; проверить кандидат с целевой Panel, rw-core и
Linux environments; и лишь затем изменить `ContractVersion`.

Project version выбирается отдельно. Preview можно начать до завершения выравнивания, но он должен
продолжать сообщать реально реализованный контракт. Обычная stable-версия допустима только при
совпадении project и contract versions.

## Вывод версии и метаданные выпуска

Node и `rnlctl` сообщают обе идентичности:

```text
remnanode-lite <Version> (contract <ContractVersion>)
rnlctl <Version> (contract <ContractVersion>)
```

В release records также указываются:

- класс выпуска и продвинутый канал;
- project version, Git tag и source commit;
- official contract version и закреплённый source commit;
- принятый container manifest digest и его attestation;
- область Panel и runtime, использованная для maintainer verification;
- закреплённые версии rw-core и runtime assets;
- статус публикации `amd64` и `arm64`;
- известные отличия, риски и rollback reference;
- checksums Native bundle и attestations assets.

GitHub формирует Release notes из merged changes. Списки хостов, сведения о Panel, logs,
secrets и другие runtime observations не являются release assets и не должны попадать в репозиторий.

`NODE_CONTRACT_VERSION` предназначен только для контролируемой диагностики и экстренных
compatibility tests. Он не меняет реализованное поведение, закреплённые evidence, binary identity
или release claims.
