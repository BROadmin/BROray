# Выпуск BROray

## Проверка и сборка

Из корня репозитория:

```sh
./scripts/verify-release.sh
```

Сценарий выполняет три независимых этапа:

1. проверяет синтаксис, JSON, версии и отсутствие персональных данных;
2. заново собирает WebUI и запускает self-test;
3. собирает OPKG, раскрывает его и повторно проверяет состав, версию, Xray и контрольные суммы.

Готовые файлы создаются в `dist/`:

```text
dist/opkg.sh
dist/broray-safe-upgrade-2.1.0-2.sh
dist/broray-manual-to-opkg-2.1.0-2.sh
dist/SHA256SUMS
dist/opkg/aarch64-3.10/Packages
dist/opkg/aarch64-3.10/Packages.gz
dist/opkg/aarch64-3.10/broray_2.1.0-2_aarch64-3.10.ipk
dist/BROray-opkg-update-2.1.0-2.tar.gz
```

## Публикация на api.brovibe.cloud

Корень канала обновлений:

```text
/var/www/api.brovibe.cloud/releases
```

Архив обновления раскрывается в этот каталог:

```sh
if cd /var/www/api.brovibe.cloud/releases; then
    tar -xzf BROray-opkg-update-2.1.0-2.tar.gz
    chmod 644 \
        opkg.sh \
        broray-safe-upgrade-2.1.0-2.sh \
        broray-manual-to-opkg-2.1.0-2.sh \
        SHA256SUMS \
        opkg/aarch64-3.10/Packages \
        opkg/aarch64-3.10/Packages.gz \
        opkg/aarch64-3.10/broray_2.1.0-2_aarch64-3.10.ipk
else
    echo "ОШИБКА: каталог релизов недоступен"
fi
```

Старый пакет не удаляется до проверки обновления на Keenetic.

## Проверка опубликованного канала

```sh
curl -fI \
    https://api.brovibe.cloud/releases/opkg/aarch64-3.10/Packages.gz

curl -fI \
    https://api.brovibe.cloud/releases/opkg/aarch64-3.10/broray_2.1.0-2_aarch64-3.10.ipk

curl -fI \
    https://api.brovibe.cloud/releases/broray-safe-upgrade-2.1.0-2.sh

curl -fI \
    https://api.brovibe.cloud/releases/broray-manual-to-opkg-2.1.0-2.sh
```

На Keenetic:

```sh
opkg update &&
opkg list-upgradable | grep '^broray '
```

Для перехода с `2.1.0-1`:

```sh
wget -qO /tmp/broray-safe-upgrade-2.1.0-2.sh \
    https://api.brovibe.cloud/releases/broray-safe-upgrade-2.1.0-2.sh &&
ash /tmp/broray-safe-upgrade-2.1.0-2.sh
```

После перехода:

```sh
opkg list-installed broray
broray version
broray status
```
