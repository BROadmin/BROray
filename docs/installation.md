# Установка и обслуживание BROray 2.1.0

## Требования

- Keenetic с установленным Entware;
- терминал с правами `root`;
- доступ в интернет;
- поддерживаемая архитектура пакета: `aarch64-3.10`.

Проверка среды:

```sh
command -v opkg
df -h /opt
```

## Чистая установка

Сначала установите поддержку HTTPS:

```sh
opkg update &&
opkg install ca-bundle ca-certificates wget-ssl
```

Затем подключите репозиторий и установите BROray:

```sh
wget -qO /tmp/broray-opkg.sh \
    https://api.brovibe.cloud/releases/opkg.sh &&
ash /tmp/broray-opkg.sh
```

Сценарий подключает OPKG-репозиторий, устанавливает зависимости, определяет LAN-адрес, настраивает HTTP Proxy Keenetic и запускает службы BROray.

## Проверка

```sh
opkg list-installed broray
broray version
broray status
```

Для версии 2.1.0 ожидается пакет `2.1.0-2` или новее:

```sh
opkg list-installed broray
broray version
```

## Обновление

Переход с `2.1.0-1` на `2.1.0-2` выполняется безопасным сценарием:

```sh
wget -qO /tmp/broray-safe-upgrade-2.1.0-2.sh \
    https://api.brovibe.cloud/releases/broray-safe-upgrade-2.1.0-2.sh &&
ash /tmp/broray-safe-upgrade-2.1.0-2.sh
```

Сценарий заранее загружает предыдущий пакет, проверяет новый пакет по
индексу OPKG и при любой ошибке возвращает версию OPKG, файлы и все пять
служб в предыдущее состояние.

Для последующих обновлений:

```sh
opkg update &&
opkg upgrade broray
```

Пакет сохраняет активную конфигурацию Xray, серверы и подписки. Перед заменой программных файлов создаётся локальная резервная копия в `/opt/broray/backups`.

Если OPKG сообщает о файлах с суффиксом `-opkg`, локально изменённая конфигурация сохранена, а новый шаблон помещён рядом. Основные пользовательские настройки можно сравнить командами:

```sh
diff -u \
    /opt/broray/config/system/settings.json \
    /opt/broray/config/system/settings.json-opkg

diff -u \
    /opt/broray/routes/config.json \
    /opt/broray/routes/config.json-opkg
```

## Удаление

Удалите пакет штатно:

```sh
opkg remove broray
```

После удаления проверьте, остались ли пользовательские данные:

```sh
ls -la /opt/broray
```

Не удаляйте оставшиеся данные, пока не сохраните нужные подписки, серверы и резервные копии.

## Совместимость BusyBox

Все shell-файлы проверяются командой:

```sh
ash -n /путь/к/файлу.sh
```
