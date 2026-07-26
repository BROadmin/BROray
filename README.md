# BROray

BROray — менеджер Xray для Keenetic с WebUI, подписками, выбором серверов и интеграцией со штатным прокси-интерфейсом KeeneticOS.

Решение упрощает установку и управление Xray на роутерах Keenetic. Проект работает в среде Entware и использует штатные механизмы KeeneticOS: OPKG, CLI/`ndmc`, HTTP Proxy и интерфейсы `ProxyN`. Скрипты рассчитаны на BusyBox `ash`.

## Возможности

- авторизованный WebUI со страницами «Главная», «Серверы», «Подписки», «Маршруты», «Keenetic», «Xray» и «BROray»;
- импорт отдельных подключений и подписок с выбором активного сервера;
- проверка качества серверов и безопасное автоматическое переключение;
- создание, проверка и восстановление принадлежащего BROray интерфейса `ProxyN`;
- запуск, остановка, перезапуск, диагностика и обновление Xray;
- готовые наборы маршрутов для Telegram, WhatsApp, YouTube, ChatGPT, Facebook, Instagram, Meta, TikTok и Speedtest;
- безопасный импорт собственных BAT-маршрутов с предварительной проверкой и отдельными карточками;
- локальный WebUI на порту `8080` и публикация через KeenDNS HTTP Proxy;
- обновление BROray через собственный OPKG-репозиторий.

## Текущая версия

BROray 2.1.1, пакет OPKG `2.1.1-1`.

Проверено на Keenetic Ultra KN-1811 с KeeneticOS 5.1.1 Preview, BusyBox 1.37.0 и Entware `aarch64-3.10`.

## Установка

Требуется установленный Entware и доступ к терминалу Keenetic под `root`.

```sh
opkg update &&
opkg install ca-bundle ca-certificates wget-ssl

wget -qO /tmp/broray-opkg.sh \
    https://api.brovibe.cloud/releases/opkg.sh &&
ash /tmp/broray-opkg.sh
```

После установки WebUI обычно доступен по адресу:

```text
http://192.168.1.1:8080/
```

Фактический LAN-адрес определяется автоматически.

## Переход с ручной установки 2.1.0 на OPKG

Этот сценарий предназначен только для существующей ручной установки
BROray `2.1.0` на Entware `aarch64-3.10`. Не устанавливайте пакет командой
`opkg install broray` непосредственно поверх ручной установки.

Сначала установите HTTPS-загрузчик:

```sh
opkg update &&
opkg install ca-bundle ca-certificates wget-ssl
```

Затем загрузите, проверьте и один раз запустите официальный мигратор:

```sh
MIGRATOR="/tmp/broray-manual-to-opkg-2.1.0-2.sh"
EXPECTED_SHA256="6c976a0b3958a8ad1b78584ff54874f283b8218c9c97dd57937ea0b86c214518"

/opt/bin/wget -qO "$MIGRATOR.part" \
    https://api.brovibe.cloud/releases/broray-manual-to-opkg-2.1.0-2.sh &&
ACTUAL_SHA256="$(
    sha256sum "$MIGRATOR.part" |
        awk '{print $1}'
)" &&
if [ "$ACTUAL_SHA256" = "$EXPECTED_SHA256" ]; then
    mv "$MIGRATOR.part" "$MIGRATOR" &&
    ash "$MIGRATOR"
else
    echo "ОШИБКА: SHA-256 мигратора не совпала"
    rm -f "$MIGRATOR.part"
    false
fi
```

Мигратор автоматически создаёт и проверяет резервную копию, сохраняет
серверы, подписки, маршруты и настройки, сверяет Xray с содержимым пакета,
регистрирует BROray `2.1.0-2` в OPKG и проверяет Xray, WebUI и все пять служб.
При ошибке после начала установки выполняется автоматический возврат к ручной
BROray `2.1.0`. Повторно запускать мигратор после успешного перехода не нужно.

## Обновление

Для безопасного перехода с пакета `2.1.0-1` на исправленный
`2.1.0-2` используется проверяемый сценарий с подготовленным OPKG-откатом:

```sh
wget -qO /tmp/broray-safe-upgrade-2.1.0-2.sh \
    https://api.brovibe.cloud/releases/broray-safe-upgrade-2.1.0-2.sh &&
ash /tmp/broray-safe-upgrade-2.1.0-2.sh
```

Начиная с установленного пакета `2.1.0-2`, штатные обновления доступны
через WebUI или OPKG:

```sh
opkg update &&
opkg upgrade broray
```

Перед обновлением пакет создаёт локальную резервную копию программных
файлов и пользовательской конфигурации. При ошибке OPKG возвращается
к предыдущей версии пакета, восстанавливает снимок и проверяет все пять
служб BROray.

## Удаление

```sh
opkg remove broray
```

После удаления пакета пользовательские данные и резервные копии могут сохраняться в `/opt/broray`. Перед их ручным удалением убедитесь, что нужные подписки и настройки сохранены отдельно.

## CLI

```sh
broray help
broray list
broray current
broray status
broray test
broray restart
broray-servers summary
broray-subscriptions list
broray-routes summary
```

## Исходный код и релизы

Каталог `root/` повторяет пути устанавливаемой файловой системы. Бинарный файл Xray не хранится в исходном дереве репозитория; он добавляется при сборке пакета из официального выпуска Xray-core.

Сценарий `scripts/build-opkg-release.sh`:

1. собирает очищенное дерево пакета;
2. загружает официальный Xray для Linux ARM64;
3. проверяет официальную и закреплённую SHA-256;
4. создаёт пакет, `Packages`, `Packages.gz` и архив обновления сайта;
5. проверяет, что пользовательские данные не попали в пакет.

Готовые пакеты распространяются через OPKG-репозиторий:

```text
https://api.brovibe.cloud/releases/opkg/aarch64-3.10
```

## Важно

- Для проверки shell-скриптов на Keenetic используйте `ash -n`, а не `/bin/sh -n`.
- Не публикуйте файлы из `servers/`, `subscriptions/`, `deleted-subscriptions/`, `run/`, `backup/`, `backups/` и рабочих каталогов `routes/`: они могут содержать адреса серверов, UUID, ссылки подписок и локальное состояние роутера.
- Перед применением маршрутизации убедитесь, что использование решения соответствует правилам вашей сети и применимому законодательству.

## Поддержать проект

Если BROray оказался полезен, вы можете поддержать дальнейшую разработку:

[Поддержать BROray через CloudTips](https://pay.cloudtips.ru/p/09b23d0a)

## Лицензия

Исходный код BROray распространяется на условиях [GNU General Public License v3.0](LICENSE). Xray-core не является частью исходного кода BROray и распространяется на собственных условиях, сохранённых в `third_party/xray/`.
