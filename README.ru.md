<!-- translation: locale=ru; source=README.md; source-sha256=a3b25d081a9df7d06803f87f10022c8da66e40da17164aeb593a44a837b9ed2a -->
<div align="center">

# Remnanode Lite

**Лёгкая реализация Remnawave Node на Go для небольших Linux-серверов**

[English](README.md) | [简体中文](README.zh-CN.md) | **Русский**

**Это перевод. Актуальной считается [английская версия README.md](README.md).**

[![CI](https://github.com/luxiaba/remnanode-lite/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/luxiaba/remnanode-lite/actions/workflows/ci.yml)
[![Container](https://github.com/luxiaba/remnanode-lite/actions/workflows/container.yml/badge.svg?branch=main)](https://github.com/luxiaba/remnanode-lite/actions/workflows/container.yml)
[![Security](https://github.com/luxiaba/remnanode-lite/actions/workflows/security.yml/badge.svg)](https://github.com/luxiaba/remnanode-lite/actions/workflows/security.yml)
[![Go](https://img.shields.io/badge/Go-1.26.5-00ADD8?logo=go&logoColor=white)](go.mod)
[![License](https://img.shields.io/badge/license-AGPL--3.0-blue.svg)](LICENSE)

[Быстрый запуск Docker](#быстрый-запуск-docker) · [Настройка](docs/i18n/ru/configuration.md) · [Эксплуатация](docs/i18n/ru/operations.md) · [Документация](docs/i18n/ru/README.md)

</div>

Remnanode Lite работает как Remnawave-совместимый Node на Linux. Он получает конфигурацию из Remnawave Panel, управляет процессом rw-core, пользователями и правилами плагинов, а также отправляет статистику системы и трафика. Docker-образ уже содержит rw-core и необходимые ему файлы данных.

Поддерживаемая конфигурация развёртывания рассчитана на сервер с **512 MiB RAM, 1 vCPU и 2 GB диска**. Образы доступны для `linux/amd64` и `linux/arm64`.

> [!NOTE]
> Remnanode Lite является независимым проектом сообщества, не связанным с Remnawave и не имеющим её официальной поддержки. Совместимость с официальным Node основана на его публично наблюдаемом поведении; код проекта разрабатывается и сопровождается независимо.

## Возможности

- Реализует контракт API Remnawave Node `2.8.0`.
- Node работает как единый процесс на Go и напрямую управляет rw-core; Node.js и s6 не требуются.
- Включает поддерживаемый Compose-профиль с пониженным потреблением памяти для серверов с 512 MiB RAM.
- Поддерживает обновление пользователей на лету, сбор статистики, управление соединениями и официальные форматы правил плагинов.
- Публикует в GHCR мультиархитектурные образы с SBOM, данными о происхождении и аттестациями сборки.
- Для развёртывания достаточно одного Compose-файла. Исходный код, `.env` и постоянный том данных не нужны.

## Быстрый запуск Docker

Для запуска потребуются Docker Engine с Compose v2, узел, созданный в Remnawave Panel, и его полный Secret Key. Порт узла должен быть доступен со стороны Panel. Команды ниже предполагают запуск из оболочки `root`; при необходимости добавьте `sudo`.

Скачайте Compose-файл последнего стабильного выпуска:

```bash
mkdir -p /opt/remnanode
cd /opt/remnanode

curl -fL \
  https://github.com/luxiaba/remnanode-lite/releases/latest/download/docker-compose.single-file.yaml \
  -o docker-compose.yaml

chmod 600 docker-compose.yaml
```

Скачанный файл уже привязан к точной версии образа из этого выпуска.

Откройте `docker-compose.yaml` и укажите порт узла и полный Secret Key из Panel:

```yaml
environment:
  NODE_PORT: "38329"
  SECRET_KEY: "PASTE_THE_COMPLETE_PANEL_SECRET_KEY"
```

Запустите узел:

```bash
cd /opt/remnanode
docker compose config --quiet
docker compose pull
docker compose up -d --no-build
docker compose ps
docker compose logs --tail=100 remnanode
```

Контейнер должен перейти в состояние `healthy`, после чего узел появится в Panel как подключённый. Затем проверьте прохождение реального трафика через прокси. Статус `healthy` сам по себе не подтверждает связь с Panel или прохождение трафика через rw-core.

При переходе с официального контейнера можно сохранить прежние `NODE_PORT` и `SECRET_KEY`, но перед запуском нового контейнера необходимо остановить старый. В [руководстве по Docker](docs/i18n/ru/deployment-docker.md) описаны миграция, установка конкретной версии, закрепление образа по дайджесту и откат.

## Основные переменные окружения Docker

В большинстве случаев достаточно изменить `NODE_PORT` и `SECRET_KEY`. Необязательные параметры добавляются в ту же секцию `environment` Compose-файла.

| Переменная | Обязательна | Значение в Compose-файле релиза | Назначение |
| --- | --- | --- | --- |
| `NODE_PORT` | Да | `38329` | HTTPS-порт для связи с Panel. Должен совпадать с портом узла в Panel. |
| `SECRET_KEY` | Да | Требует замены | Полный Secret Key в формате base64 или base64url, выданный Panel. |
| `LOW_MEMORY` | Нет | `1` | Включает параметры для работы на сервере с небольшим объёмом памяти. |
| `NODE_BIND_ADDR` | Нет | Не задано | Задаёт локальный адрес для прослушивания. Если переменная не задана, Node принимает подключения на всех интерфейсах. |
| `BODY_LIMIT_MB` | Нет | Автоматически | Переопределяет максимальный размер тела запроса внешнего API Node. При `LOW_MEMORY=1` автоматически устанавливается 16 MiB. |
| `GOMEMLIMIT` | Нет | Автоматически | Переопределяет мягкий лимит памяти Go. При `LOW_MEMORY=1` автоматически устанавливается 180 MiB. |

Задавайте переменные в виде YAML-словаря, как показано выше. Не используйте `- SECRET_KEY="..."`: при записи списком кавычки становятся частью значения, и `SECRET_KEY` не удаётся декодировать. Compose-файл содержит секретный ключ, а значения переменных окружения видны в локальных метаданных Docker, поэтому сохраняйте для файла права `0600`.

Все параметры, допустимые значения и порядок приоритетов приведены в [справочнике конфигурации](docs/i18n/ru/configuration.md).

## Повседневные операции

Просмотр журналов Node:

```bash
docker compose logs --tail=100 -f remnanode
```

Просмотр вывода и ошибок rw-core:

```bash
docker exec -it remnanode tail -n 50 -F \
  /var/log/remnanode/xray.out.log \
  /var/log/remnanode/xray.err.log
```

Проверка запущенной версии:

```bash
docker exec remnanode remnanode-lite version
```

При переходе между точными версиями сначала измените `image:`, затем загрузите образ и пересоздайте контейнер:

```bash
docker compose pull
docker compose up -d --no-build --force-recreate
```

При использовании `latest` контейнер перейдёт на новый стабильный образ только после явного pull и пересоздания; работающий контейнер сам по себе не обновляется. Журналы rw-core находятся в tmpfs, а журналы Node сохраняются и ротируются драйвером Docker `json-file`. Проверка состояния, диагностика, поэтапное обновление и откат описаны в [руководстве по эксплуатации](docs/i18n/ru/operations.md).

## Версии и теги образов

| Тег | Назначение |
| --- | --- |
| `X.Y.Z` | Стабильный релиз, совместимый с контрактом соответствующей версии официального Node. Рекомендуется для эксплуатации и отката. |
| `X.Y.Z-rnl.N` | Проверенная итерация Remnanode Lite: ранняя работа над будущей версией или дополнительные улучшения уже согласованной версии. |
| `latest` | Последний опубликованный стабильный релиз. Тег перемещается и не подходит для отката. |
| `sha-<commit>` / `candidate-sha-<commit>` | Образы для тестирования конкретного кандидата из `main` до формального релиза. |
| `edge` | Перемещаемый образ текущего `main`, только для кратковременного тестирования. |

Для группы серверов используйте один фиксированный тег версии или дайджест манифеста и сохраняйте предыдущее значение для отката. Полные правила приведены в [документе о версиях и тегах](docs/i18n/ru/versioning.md).

## Совместимость

| Компонент | Текущее значение |
| --- | --- |
| Контракт Node | `2.8.0` |
| rw-core | `v26.6.27` |
| Платформы | `linux/amd64`, `linux/arm64` |
| Целевая конфигурация хоста | `512 MiB RAM / 1 vCPU / 2 GB диска` |
| Лимит сервиса в Compose | `448 MiB RAM`, без дополнительной подкачки |

Указанные ресурсы относятся к поддерживаемому Compose-профилю и не гарантируют, что любая нагрузка или любой набор плагинов будут работать на сервере той же конфигурации. Результаты измерений и ограничения приведены в [описании бюджета ресурсов (на английском)](docs/development/resource-budget.md).

## Как это работает

```mermaid
flowchart LR
    Panel["Remnawave Panel"] -->|"mTLS + JWT"| Node["Remnanode Lite"]
    Node -->|"конфигурация, пользователи, статистика"| Core["rw-core"]
    Node --> Rules["nftables и соединения"]
    Core --> Traffic["Трафик через прокси"]
```

Node управляет процессом rw-core и текущим состоянием сервиса. Активная конфигурация Xray поступает из Panel, поэтому после пересоздания контейнера отдельный том для конфигурации не нужен. Границы между пакетами, правила жизненного цикла и потоки данных описаны в [документе об архитектуре (на английском)](docs/architecture.md).

## Документация

| Задача | С чего начать |
| --- | --- |
| Развернуть или перенести узел | [Docker Compose](docs/i18n/ru/deployment-docker.md) · [Установка в Linux без Docker](docs/i18n/ru/deployment-native.md) |
| Настроить и обслуживать узел | [Конфигурация](docs/i18n/ru/configuration.md) · [Эксплуатация](docs/i18n/ru/operations.md) |
| Понять устройство проекта | [Область проекта](docs/i18n/ru/project.md) · [Архитектура (англ.)](docs/architecture.md) |
| Работать с кодом | [Разработка (англ.)](docs/development/README.md) · [Тестирование (англ.)](docs/development/testing.md) · [Участие в проекте (англ.)](CONTRIBUTING.md) |
| Разобраться в версиях и выпусках | [Версии](docs/i18n/ru/versioning.md) · [Процесс выпуска (англ.)](docs/release.md) |
| Сообщить о проблеме безопасности | [Политика безопасности](docs/i18n/ru/security.md) |

В [русскоязычном указателе документации](docs/i18n/ru/README.md) собраны остальные руководства и ссылки на материалы на английском языке.

## Разработка

Обычные модульные тесты не требуют доступа к Panel, Secret Key или запущенного rw-core:

```bash
git switch dev
go mod download
go test -count=1 ./...
mkdir -p bin
go build -trimpath -o bin/remnanode-lite ./cmd/remnanode-lite
./bin/remnanode-lite version
```

Сетевые интеграционные тесты в Linux, проверки с реальным rw-core, совместимость с Panel и приёмка релиза относятся к отдельным уровням тестирования. Перед изменением этих частей ознакомьтесь с [руководством разработчика (на английском)](docs/development/README.md).

## Безопасность

Контейнер использует сетевой режим хоста (`network_mode: host`) и получает Linux-привилегию `NET_ADMIN`, поэтому может изменять сетевые настройки хоста. Запускайте только доверенные образы; для эксплуатации выбирайте фиксированную версию или дайджест манифеста. Установите на Compose-файл права `0600` и ограничьте доступ к сокету Docker и административным учётным записям хоста.

Не публикуйте секретные ключи, сертификаты, реальные данные узла, код эксплойтов или подробности уязвимостей в открытых Issues. Порядок конфиденциального сообщения приведён в [политике безопасности](docs/i18n/ru/security.md).

## Лицензия

Remnanode Lite распространяется по лицензии [AGPL-3.0-only](LICENSE).
