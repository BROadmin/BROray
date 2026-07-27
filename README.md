# BROray

BROray — менеджер Xray для Keenetic с WebUI, подписками, выбором серверов и интеграцией со штатным прокси-интерфейсом KeeneticOS.

Решение работает в Entware и использует штатные механизмы KeeneticOS: OPKG, CLI/`ndmc`, HTTP Proxy и интерфейсы `ProxyN`. Скрипты рассчитаны на BusyBox `ash`.

## Возможности

- авторизованный WebUI со страницами «Главная», «Серверы», «Подписки», «Маршруты», «Keenetic», «Xray» и «BROray»;
- импорт отдельных подключений и подписок с выбором активного сервера;
- проверка качества серверов и безопасное автоматическое переключение;
- создание, проверка и восстановление принадлежащего BROray интерфейса `ProxyN`;
- запуск, остановка, перезапуск, диагностика и обновление Xray;
- девять независимых наборов маршрутов: Telegram, WhatsApp, YouTube, ChatGPT, Facebook, Instagram, Meta, TikTok и Speedtest;
- безопасный импорт собственных BAT-маршрутов;
- обнаружение добавленных, изменённых и удалённых файлов маршрутов;
- безопасное добавление и удаление маршрутов с сохранением общих маршрутов других наборов;
- локальный WebUI на порту `8080` и публикация через KeenDNS HTTP Proxy;
- универсальное безопасное обновление с любой установленной версии BROray.

## Текущая версия

BROray 2.1.1, пакет OPKG `2.1.1-2`.

Проверено:

- обновление на Keenetic Peak KN-2710 EAEU;
- обновление на Keenetic Hopper KN-3811 EAEU;
- обновление и чистая установка на Keenetic Ultra KN-1811 EAEU;
- чистая установка на Keenetic Peak KN-2710 EAEU;
- чистая установка на Netcraze Ultra NC-1812 EAEU.

С 2025 года часть устройств для рынка ЕАЭС выпускается под брендом Netcraze. В списке проверенных устройств такие модели указываются отдельно по фактическому бренду и индексу модели.

## Установка

Требуется установленный Entware и доступ к терминалу Keenetic или Netcraze под `root`.

```sh
opkg update &&
opkg install ca-bundle ca-certificates wget-ssl &&
TARGET="/tmp/broray-install-2.1.1-2.sh" &&
/opt/bin/wget -qO "$TARGET.part" \
    "https://api.brovibe.cloud/releases/broray-install-2.1.1-2.sh?v=$(date +%s)" &&
echo "f14a86fc481d1a5c5c294635fe79031f71aa999d432b48be00c3a137ab7b8396  $TARGET.part" | sha256sum -c - &&
mv "$TARGET.part" "$TARGET" &&
/opt/bin/ash "$TARGET"
```

После установки WebUI обычно доступен по адресу `http://192.168.1.1:8080/`.

## Обновление

Обновление не зависит от исходной версии BROray. Обновитель до создания резервной копии очищает старые резервные копии, временные файлы, журналы и OPKG-кэш. Проверенная временная копия создаётся в `/tmp`, а при ошибке выполняется автоматический откат.

```sh
opkg update &&
opkg install ca-bundle ca-certificates wget-ssl &&
TARGET="/tmp/broray-safe-upgrade-2.1.1-2.sh" &&
/opt/bin/wget -qO "$TARGET.part" \
    "https://api.brovibe.cloud/releases/broray-safe-upgrade-2.1.1-2.sh?v=$(date +%s)" &&
echo "1cfcc536215825a29f087582483297baaecabb68fe641dd4ddc960f7146e9504  $TARGET.part" | sha256sum -c - &&
mv "$TARGET.part" "$TARGET" &&
/opt/bin/ash "$TARGET"
```

Обновитель загружает точный IPK напрямую, запрещает HTTP-кэш и проверяет SHA-256 до запуска OPKG.

## Удаление

```sh
opkg remove broray
```

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

Каталог `root/` повторяет пути устанавливаемой файловой системы. Бинарный файл Xray не хранится в исходном дереве репозитория; при сборке используется официальный выпуск Xray-core с проверкой закреплённой SHA-256.

Официальный OPKG-репозиторий:

```text
https://api.brovibe.cloud/releases/opkg/aarch64-3.10
```

## Обсуждение и поддержка

[Telegram-канал BROvibe](https://t.me/BROvibe_vpn)

## Поддержать проект

[Поддержать BROray через CloudTips](https://pay.cloudtips.ru/p/09b23d0a)

## Лицензия

Исходный код BROray распространяется на условиях [GNU General Public License v3.0](LICENSE). Xray-core не является частью исходного кода BROray и распространяется на собственных условиях, сохранённых в `third_party/xray/`.
