# Первая установка BROray

Установите BROray 3.5.9 r9 на Keenetic или Netcraze с работающим Entware. После установки откройте WebUI BROray в браузере. [Обновление и переустановка уже установленного BROray](update.md).

## Перед началом

Подключитесь к домашней сети. На роутере нужны работающий Entware, 60 МиБ свободного места в `/opt` и компонент «Клиент прокси». [Как проверить компоненты KeeneticOS](https://docs.brovibe.cloud/broray/reference/#keeneticos-components).

<details>
<summary>Если Entware ещё не установлен</summary>

Выберите инструкцию производителя для вашей модели. [Пример для Keenetic](https://support.keenetic.com/hero/kn-1011/en/20980-installing-the-entware-repository-on-a-usb-drive.html) · [Пример для Netcraze](https://support.netcraze.ru/ultra/nc-1812/ru/20980-installing-the-entware-repository-on-a-usb-drive.html).

</details>

## 1. Откройте терминал Entware

В веб-терминале Keenetic выполните `exec sh` из приглашения `(config)>`. Если входите по SSH в Entware, войдите как `root`.

<details>
<summary>Если терминал или загрузка не работают</summary>

Для веб-терминала нужны компонент «Интерфейс командной строки в браузере» и доступ OPKG для вашей учётной записи. Команда `id -u` должна показать `0`. Если отсутствует `curl` или проверка сертификата не проходит, выполните `opkg update`, затем `opkg install curl ca-bundle ca-certificates`.

</details>

## 2. Найдите модель и скопируйте команду

Раскройте свою группу и вставьте готовую команду в терминал целиком. Она скачает нужный архив, проверит его и установит BROray.

<details>
<summary>ARM64 · KN-1012, KN-1811, KN-1812, KN-2312, KN-2710, KN-3811, KN-3812, KN-4110, NC-1812</summary>

```sh
(
set -eu
if [ -e /opt/bin/broray ] || [ -L /opt/bin/broray ]; then
  echo 'BROray уже установлен. Откройте инструкцию обновления.' >&2
  exit 1
fi
work="$(mktemp -d /tmp/broray-install.XXXXXX)"
trap 'rm -rf -- "$work"' EXIT
cd "$work"
bundle="broray-3.5.9-aarch64-r9.tar.gz"
curl --proto '=https' --tlsv1.2 -fS --connect-timeout 15 --max-time 300 -o "$bundle" "https://api.brovibe.cloud/releases/broray/channels/stable/$bundle"
printf '%s  %s\n' '46d8b63c6e06c90d15c1b0da4e6eddff7fe890310b037b684682413b9d2d2a90' "$bundle" | sha256sum -c -
tar -xzf "$bundle"
"./broray-3.5.9-aarch64/broray" installer install
/opt/bin/broray ctl ping
)
```

</details>

<details>
<summary>MIPS Little Endian · KN-1011, KN-1810, KN-1910, KN-1912, KN-1913, KN-2310, KN-2311, KN-2910, KN-3810, NC-1913</summary>

```sh
(
set -eu
if [ -e /opt/bin/broray ] || [ -L /opt/bin/broray ]; then
  echo 'BROray уже установлен. Откройте инструкцию обновления.' >&2
  exit 1
fi
work="$(mktemp -d /tmp/broray-install.XXXXXX)"
trap 'rm -rf -- "$work"' EXIT
cd "$work"
bundle="broray-3.5.9-mipsel-r9.tar.gz"
curl --proto '=https' --tlsv1.2 -fS --connect-timeout 15 --max-time 300 -o "$bundle" "https://api.brovibe.cloud/releases/broray/channels/stable/$bundle"
printf '%s  %s\n' '147cd9146fc86e06600859ab7adc3a707367dc428bab98ce1ac0325e627986c1' "$bundle" | sha256sum -c -
tar -xzf "$bundle"
"./broray-3.5.9-mipsel/broray" installer install
/opt/bin/broray ctl ping
)
```

</details>

<details>
<summary>MIPS Big Endian · KN-2112, KN-2410, KN-3610</summary>

```sh
(
set -eu
if [ -e /opt/bin/broray ] || [ -L /opt/bin/broray ]; then
  echo 'BROray уже установлен. Откройте инструкцию обновления.' >&2
  exit 1
fi
work="$(mktemp -d /tmp/broray-install.XXXXXX)"
trap 'rm -rf -- "$work"' EXIT
cd "$work"
bundle="broray-3.5.9-mips-r9.tar.gz"
curl --proto '=https' --tlsv1.2 -fS --connect-timeout 15 --max-time 300 -o "$bundle" "https://api.brovibe.cloud/releases/broray/channels/stable/$bundle"
printf '%s  %s\n' '6040bac59483f52db4fd64ee5b22ca729571478c94f225d4326ad80b90ec3452' "$bundle" | sha256sum -c -
tar -xzf "$bundle"
"./broray-3.5.9-mips/broray" installer install
/opt/bin/broray ctl ping
)
```

</details>

<details>
<summary>ARMv7 · только для подтверждённого Entware ARMv7</summary>

```sh
(
set -eu
if [ -e /opt/bin/broray ] || [ -L /opt/bin/broray ]; then
  echo 'BROray уже установлен. Откройте инструкцию обновления.' >&2
  exit 1
fi
work="$(mktemp -d /tmp/broray-install.XXXXXX)"
trap 'rm -rf -- "$work"' EXIT
cd "$work"
bundle="broray-3.5.9-armv7-r9.tar.gz"
curl --proto '=https' --tlsv1.2 -fS --connect-timeout 15 --max-time 300 -o "$bundle" "https://api.brovibe.cloud/releases/broray/channels/stable/$bundle"
printf '%s  %s\n' '06d44afdb843fc6e888b944371506124ea39d4c0353aaee2d3c60f3aeffd3c57' "$bundle" | sha256sum -c -
tar -xzf "$bundle"
"./broray-3.5.9-armv7/broray" installer install
/opt/bin/broray ctl ping
)
```

</details>

Если модели нет в списке, выполните `opkg print-architecture` и выберите группу по архитектуре Entware. [Таблица архитектур](https://docs.brovibe.cloud/broray/reference/#architectures).

Если установка завершилась ошибкой, сохраните её текст и проверьте причину. [Решение проблем](https://docs.brovibe.cloud/broray/troubleshooting/).

## 3. Откройте BROray

Откройте в браузере адрес роутера с портом `8080`, например `http://192.168.1.1:8080/`. Войдите с логином и паролем администратора Keenetic. [Подключение первого сервера](https://docs.brovibe.cloud/broray/scenarios/#first-server).

[Установка на сайте](https://docs.brovibe.cloud/broray/start/) · [Архивы и подписи на GitHub](https://github.com/BROadmin/BROray/releases/tag/v3.5.9) · [Руководство со скриншотами](USER-GUIDE-3.5.9.md)
