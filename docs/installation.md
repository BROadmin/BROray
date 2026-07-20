# Установка и обслуживание BROray 2.0.0

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

Для версии 2.0.0 ожидается пакет ревизии `2.0.0-2` или новее.

## Обновление

```sh
opkg update &&
opkg upgrade broray
```

Если OPKG сообщает о файлах с суффиксом `-opkg`, это означает, что локально изменённая конфигурация сохранена, а новый шаблон помещён рядом. Сравните файлы перед заменой:

```sh
diff -u \
    /opt/broray/config/system/settings.json \
    /opt/broray/config/system/settings.json-opkg

diff -u \
    /opt/broray/config/lighttpd.conf \
    /opt/broray/config/lighttpd.conf-opkg
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

