<!-- translation: locale=ru; source=docs/versioning.md; source-sha256=d4b26b248b395c36314c449fd2e4757dcfe4549af2a31c96fb8994e387b1eb88 -->

# Версии и теги образов

> Это русский перевод. При расхождении правил используйте [английский оригинал](../../versioning.md).

[Английский оригинал](../../versioning.md) · [Индекс](README.md) · [Процесс выпуска, англ.](../../release.md)

Remnanode Lite отделяет идентичность проекта от заявления о совместимости. Проект может развиваться самостоятельно, продолжая реализовывать ранее проверенный контракт официального Node. Точная версия и движущийся канал образа также имеют разные роли: версия обозначает один выпуск, а `preview` и `latest` выбирают класс выпуска.

Исполняемый источник истины для публикации — release workflow. Команда `release-tool metadata` определяет stable или preview по версии в исходном коде.

## Два измерения версии

| Измерение | Источник истины | Значение |
| --- | --- | --- |
| Project `Version` | `internal/version/version.go` | Идентичность исходного кода, бинарных файлов, Release assets и точного image tag |
| Official `ContractVersion` | `internal/version/contract.version`, `internal/version/version.go` и закреплённые contract evidence | Поведение официального Node, которое реализовано и сообщается Panel |
| Целевая Panel | Проверка выпуска сопровождающим | Версия Panel, использованная в реальной интеграционной проверке; не входит в идентичность выпуска |
| rw-core и runtime assets | `release/runtime-assets.lock.json` | Точные core, GeoIP, GeoSite, ASN, исходники, лицензии и checksums для Docker и Native Linux |

Например:

```text
Version:         2.8.0
ContractVersion: 2.8.0
```

Это стабильная линия, согласованная с контрактом официального Node `2.8.0`. Native Linux distribution появляется только после публикации соответствующего GitHub Release. Суффикс `rnl.N` принадлежит этому проекту, а не официальному выпуску. Изменение только `Version` не расширяет заявленный контракт.

Изменение `ContractVersion` требует закреплённого официального исходного кода, проверки contract delta, изменений реализации и тестов и завершённой проверки совместимости. Одна смена project version никогда не расширяет контракт.

## Классы выпусков

### Stable: `X.Y.Z`

Обычная версия означает стабильный выпуск, согласованный с официальным контрактом той же версии. Проверки репозитория требуют совпадения `Version` и `ContractVersion`.

После публикации stable имеет:

- Git tag `X.Y.Z`, созданный при публикации draft GitHub Release;
- обычный GitHub Release;
- точный GHCR tag `X.Y.Z`;
- движущийся канал GHCR `latest`.

GitHub также помечает такой Release как Latest. Это неизменяемая точка выравнивания и версия, выбранная стабильным каналом после публикации.

### Preview: `X.Y.Z-rnl.N`

`rnl.N` — preview-итерация Remnanode Lite. Она может использоваться до следующего официального релиза или для улучшения архитектуры, поставки и поведения ресурсов при сохранении существующего контракта.

После публикации preview имеет:

- Git tag `X.Y.Z-rnl.N`, созданный при публикации draft GitHub Release;
- GitHub Prerelease;
- точный GHCR tag `X.Y.Z-rnl.N`;
- движущийся канал GHCR `preview`.

Preview никогда не обновляет `latest` и не становится GitHub Latest. Даже после полного автоматического workflow он остаётся preview до публикации обычной stable-версии.

В одной линии `X.Y.Z` номер `N` начинается с 1 и растёт. Опубликованные версии и точные tags не переиспользуются. Числовой префикс обозначает линию разработки проекта и сам по себе не заявляет готовность такого же официального контракта.

### Текущая линия

| Версия | Контракт | Класс | Состояние |
| --- | --- | --- | --- |
| `2.8.0` | `2.8.0` | Stable | Текущая линия, выровненная по контракту; Native bundle есть только у опубликованных Releases |

SemVer ставит `X.Y.Z-rnl.N` ниже соответствующей `X.Y.Z` stable-версии. Не выводите порядок публикации или выбор канала из SemVer: workflow явно выбирает `preview` либо `latest` по формату версии.

## Git tag и точный image tag

Формальный Git tag использует точную версию проекта, как и точный container tag:

```text
Git tag:       X.Y.Z
Container tag: ghcr.io/luxiaba/remnanode-lite:X.Y.Z
```

Оба являются неизменяемыми идентификаторами выпуска, но обозначают разные объекты:

- Git tag, созданный GitHub Release, указывает на source commit, принятый из `main`;
- точный container tag указывает на уже собранный и аттестованный multi-architecture manifest этого commit.

Release workflow не пересобирает контейнер. Он проверяет `sha-<commit>` candidate, созданный из `main`, и присваивает тому же manifest digest точный release tag.

Не создавайте и не отправляйте release tags с рабочей станции. Создание draft само по себе не создаёт tag; точный version tag появляется на принятом commit `main` при публикации draft. GitHub Release immutability затем блокирует tag и assets. Ruleset для tags может запрещать обновления и удаления, но должен разрешать создание новых tags через `GITHUB_TOKEN`: стандартный GitHub Actions token не является actor в bypass list. Для ещё не опубликованной версии workflow откажется использовать уже существующий tag.

Registry tags — имена, а не адреса содержимого. Для самой строгой фиксации используйте:

```text
ghcr.io/luxiaba/remnanode-lite@sha256:<manifest-digest>
```

## Ссылки на контейнеры

| Ссылка | Изменяемость | Значение | Назначение |
| --- | --- | --- | --- |
| `sha-<40-character-commit>` | Неизменяемый по политике | Аттестованный candidate одного commit `main` | Проверка выпуска, воспроизведение и диагностика |
| `edge` | Движется | Последний подходящий candidate `main` | Только наблюдение основной линии |
| `X.Y.Z-rnl.N` | Неизменяемый по политике | Один опубликованный preview | Контролируемый preview и точный rollback |
| `preview` | Движется | Последний preview, продвинутый release workflow | Осознанное отслеживание preview |
| `X.Y.Z` | Неизменяемый по политике | Один опубликованный stable | Рекомендуемый production и точный rollback |
| `latest` | Движется | Последний stable, продвинутый release workflow | Осознанный стабильный канал обновлений |
| `name@sha256:...` | Content-addressed | Один registry manifest digest | Самая строгая фиксация развёртывания и проверки |

Обычный push в `main` может обновить `edge`, но не `preview` и не `latest`. Эти каналы продвигает только release workflow после публикации соответствующего Release.

`latest` указывает только на stable `X.Y.Z`, а `preview` — только на prerelease `X.Y.Z-rnl.N`. Каналы не пересекаются. Они не обновляют работающий контейнер автоматически: Docker проверяет tag после явного `pull`, а Compose пересоздаёт контейнер только по явной команде.

## Выбор Docker reference

Для обычного production используйте точную stable-версию:

```text
ghcr.io/luxiaba/remnanode-lite:X.Y.Z
```

Для самой строгой фиксации сохраните и используйте manifest digest:

```text
ghcr.io/luxiaba/remnanode-lite@sha256:<manifest-digest>
```

Точный preview используйте только когда его статус и изменения приемлемы:

```text
ghcr.io/luxiaba/remnanode-lite:X.Y.Z-rnl.N
```

`preview` удобен для краткой оценки, но для fleet лучше точный preview tag или digest: между обновлениями разных узлов он не сдвинется. Всегда сохраняйте предыдущую точную reference для rollback.

`latest` — добровольно выбранный стабильный канал, а не identity для rollback. Даже при его использовании прочитайте Release, сохраните resolved digest и выполните обновление явно:

```bash
docker compose pull
docker compose up -d --no-build --force-recreate
```

`sha-<commit>` служит для проверки candidate, который может стать Release. Не используйте `edge` для release acceptance: новый build из `main` может передвинуть его во время тестирования.

Candidate workflow также записывает принятый content address в attestированный asset `release-index.json`. Перед публикацией эта запись должна совпасть с проверенным candidate `sha-<commit>`. После того как Release становится immutable, восстановление использует записанный digest напрямую и не считает registry tag долговечной identity.

## Native Linux принимает только точные версии

Native install и upgrade используют полные versioned bundles и специально не следуют движущимся каналам:

```bash
sudo sh install.sh --version "<published-version>"
sudo rnlctl upgrade --to <exact-version>
```

`latest`, `preview`, `edge` и `sha-*` не являются допустимыми Native inputs. Точная версия позволяет проверить имя архива, `SHA256SUMS`, Release manifest, встроенную версию и source revision как одну идентичность выпуска.

## Когда выпуск считается опубликованным

Версия в исходниках, candidate из `main` или один Git tag сами по себе не образуют полный выпуск.

Опубликованный preview одновременно имеет:

1. `X.Y.Z-rnl.N` tag, созданный при публикации draft Release на принятом commit `main`;
2. опубликованный GitHub Prerelease с проверенными assets, включая attestированный `release-index.json`;
3. точный GHCR tag `X.Y.Z-rnl.N`, совпадающий с digest из этого index;
4. успешное продвижение этого digest в `preview`.

Stable имеет обычный `X.Y.Z`, полный GitHub Release, точный image `X.Y.Z`, GitHub Latest и продвижение в `latest`.

Оба класса используют одинаковые code, compatibility, asset, provenance и attestation gates. Различается статус Release и канал, а не строгость пути сборки.

До объявления версии или URL assets проверьте наличие Git tags, GitHub Releases и точных GHCR tags.

## Отслеживание официальных Node Releases

Запланированный contract workflow создаёт Issue при выходе новой официальной версии. Он не меняет автоматически `ContractVersion`, исходный код, project versions или container tags.

Синхронизация нового официального контракта требует закрепить версию и неизменяемый source commit, проверить routes, schemas, errors, side effects и plugin dependencies, обновить contract evidence и tests, выровнять Go implementation и проверить candidate с целевой Panel, rw-core и Linux environments. Только после этого меняется `ContractVersion`.

Project version выбирается отдельно. Preview можно начать до завершения выравнивания, но binary должен продолжать сообщать фактически реализованный контракт. Обычная stable-версия допустима только при совпадении project и contract versions.

## Вывод версии и метаданные выпуска

Node и `rnlctl` сообщают обе идентичности:

```text
remnanode-lite <Version> (contract <ContractVersion>)
rnlctl <Version> (contract <ContractVersion>)
```

Release records должны также указывать класс и продвинутый канал, project version и source commit, официальный контракт и закреплённый исходный код, принятый container digest и его attestation, а также `release-index.json`, связывающий digest с source revision, locked runtime assets, статус `amd64` и `arm64`, известные риски и rollback reference, а также checksums Native bundle и attestation assets.

GitHub формирует Release notes из merged changes. Списки хостов, сведения о Panel, logs, secrets и другие runtime observations не являются release assets и не должны попадать в репозиторий.

`NODE_CONTRACT_VERSION` предназначен только для контролируемой диагностики и emergency compatibility tests. Он не меняет реализованное поведение, закреплённые evidence, binary identity или release claims.
