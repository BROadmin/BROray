# Первая установка BROray

Установите BROray 3.5.9 r9 на Keenetic или Netcraze с работающим Entware. После установки откройте WebUI BROray в браузере. [Обновление и переустановка уже установленного BROray](update.md).

## 1. Подготовьте роутер

Подключитесь к домашней сети. Убедитесь, что Entware работает и на разделе `/opt` свободно не менее 60 МиБ. Для подключения VPN нужен компонент KeeneticOS «Клиент прокси». Администратор Keenetic используется для входа в WebUI, а для установки нужны права root в Entware. [Требования и компоненты KeeneticOS](https://docs.brovibe.cloud/broray/reference/#keeneticos-components).

Если Entware ещё нет, сначала установите его по инструкции производителя для вашей модели. [Пример для Keenetic](https://support.keenetic.com/hero/kn-1011/en/20980-installing-the-entware-repository-on-a-usb-drive.html) · [Пример для Netcraze](https://support.netcraze.ru/ultra/nc-1812/ru/20980-installing-the-entware-repository-on-a-usb-drive.html).

## 2. Откройте терминал Entware

Откройте SSH-сеанс Entware или штатный веб-терминал Keenetic. Для веб-терминала нужен компонент «Интерфейс командной строки в браузере» и доступ OPKG для вашей учётной записи. Из приглашения `(config)>` выполните `exec sh`. Продолжайте, когда открылась оболочка Entware. Команда `id -u` должна вывести 0.

## 3. Выберите сборку

Выполните `opkg print-architecture`. Выберите код сборки по архитектуре Entware. В списке ищите строку архитектуры устройства, например aarch64-3.10. Запись all обозначает общие пакеты. [Архитектуры и модели роутеров](https://docs.brovibe.cloud/broray/reference/#architectures).

| Архитектура Entware | Код сборки |
| --- | --- |
| ARM64 (AArch64) | `aarch64` |
| ARMv7, 32 бита | `armv7` |
| MIPS Little Endian | `mipsel` |
| MIPS Big Endian | `mips` |

## 4. Скачайте и установите BROray

Если `curl` отсутствует или на роутере нет актуальных корневых сертификатов, выполните `opkg update`, затем `opkg install curl ca-bundle ca-certificates`. В первой строке следующего блока укажите выбранный код сборки. Запустите весь блок в терминале роутера.

```sh
arch=aarch64
(
set -eu
case "$arch" in
  aarch64) expected=46d8b63c6e06c90d15c1b0da4e6eddff7fe890310b037b684682413b9d2d2a90 ;;
  armv7) expected=06d44afdb843fc6e888b944371506124ea39d4c0353aaee2d3c60f3aeffd3c57 ;;
  mipsel) expected=147cd9146fc86e06600859ab7adc3a707367dc428bab98ce1ac0325e627986c1 ;;
  mips) expected=6040bac59483f52db4fd64ee5b22ca729571478c94f225d4326ad80b90ec3452 ;;
  *) echo 'Выберите архитектуру из таблицы.' >&2; exit 1 ;;
esac
if [ -e /opt/bin/broray ] || [ -L /opt/bin/broray ]; then
  echo 'BROray уже установлен. Откройте инструкцию обновления.' >&2
  exit 1
fi
work="$(mktemp -d /tmp/broray-install.XXXXXX)"
trap 'rm -rf -- "$work"' EXIT
cd "$work"
bundle="broray-3.5.9-${arch}-r9.tar.gz"
curl --proto '=https' --tlsv1.2 -fS --connect-timeout 15 --max-time 300 -o "$bundle" "https://api.brovibe.cloud/releases/broray/channels/stable/$bundle"
printf '%s  %s\n' "$expected" "$bundle" | sha256sum -c -
tar -xzf "$bundle"
"./broray-3.5.9-${arch}/broray" installer install
/opt/bin/broray version
/opt/bin/broray ctl ping
)
```

Если команда вернула ошибку, сохраните её текст и остановитесь на этом шаге. При несовпадении SHA-256 или архитектуры повторно сверяйте архив и модель. Если на роутере осталась ветка 3.2, сохраните данные и разберите её состояние до первой установки 3.5. [Решение проблем установки и входа](https://docs.brovibe.cloud/broray/troubleshooting/).

## 5. Войдите в WebUI

Откройте BROray по локальному адресу роутера с портом 8080, например `http://192.168.1.1:8080/`. Войдите с логином и паролем администратора Keenetic. В WebUI проверьте состояние Xray. Затем добавьте сервер или подписку, проверьте выбранный сервер и нажмите «Подключить». [Как подключить первый сервер](https://docs.brovibe.cloud/broray/scenarios/#first-server).

[Установка на сайте](https://docs.brovibe.cloud/broray/start/) · [Архивы и подписи на GitHub](https://github.com/BROadmin/BROray/releases/tag/v3.5.9) · [Руководство со скриншотами](USER-GUIDE-3.5.9.md)
