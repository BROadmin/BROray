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
- универсальное безопасное обновление с любой установленной версии BROray;
- фактический прогресс, безопасная остановка и продолжение долгих операций с маршрутами;
- раздельные проверки внешнего источника и локального набора с фактической сверкой Keenetic;
- DNS-over-TLS: TLS/SNI-проверка, установка, обновление и безопасное удаление;
- восстановительная переустановка текущей версии и безопасная очистка диска;

## Текущая версия

BROray 2.2.0, пакет OPKG `2.2.0-1` (кандидат до проверки на реальном Keenetic).

Проверено:

- обновление на Keenetic Peak (KN-2710) EAEU;
- обновление на Keenetic Hopper (KN-3811) EAEU;
- обновление и чистая установка на Keenetic Ultra (KN-1811) EAEU;
- чистая установка на Keenetic Peak (KN-2710) EAEU;
- чистая установка на Netcraze Ultra (NC-1812) EAEU.

В странах ЕАЭС часть новых устройств выпускается под брендом Netcraze и получает индексы моделей `NC-xxxx`. Netcraze — новый бренд компании, ранее представлявшей продукцию Keenetic в ЕАЭС; поэтому в списке совместимости могут встречаться устройства обоих брендов.

## Установка

Требуется установленный Entware и доступ к терминалу Keenetic или Netcraze под `root`.

```sh
opkg update &&
opkg install ca-bundle ca-certificates wget-ssl &&
TARGET="/tmp/broray-install-2.2.0-1.sh" &&
/opt/bin/wget -qO "$TARGET.part" \
    "https://api.brovibe.cloud/releases/broray-install-2.2.0-1.sh?v=$(date +%s)" &&
echo "af5db97667fe93b344651d928ba36a0010b93f27507aa6c5030c26cb5269f57a  $TARGET.part" | sha256sum -c - &&
mv "$TARGET.part" "$TARGET" &&
/opt/bin/ash "$TARGET"
```

После установки WebUI обычно доступен по адресу `http://192.168.1.1:8080/`.

## Обновление

Обновление не зависит от исходной версии BROray. Обновитель очищает только безопасные временные данные и OPKG-кэш. Пользовательские данные и сохранённые резервные копии не удаляются. Проверенная временная копия создаётся в `/tmp`, а при ошибке выполняется автоматический откат.

```sh
opkg update &&
opkg install ca-bundle ca-certificates wget-ssl &&
TARGET="/tmp/broray-safe-upgrade-2.2.0-1.sh" &&
/opt/bin/wget -qO "$TARGET.part" \
    "https://api.brovibe.cloud/releases/broray-safe-upgrade-2.2.0-1.sh?v=$(date +%s)" &&
echo "a3af58edbb46e8d3f39d09f514a18ebe4905cfa66a95bda684d4e4a5547c7ba3  $TARGET.part" | sha256sum -c - &&
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

BROray развивается как открытый проект. Поддержка помогает оплачивать инфраструктуру, тестирование и выпуск обновлений.

<p align="center">
  <a href="https://pay.cloudtips.ru/p/09b23d0a">
    <img src="docs/assets/cloudtips-qr.svg" alt="QR-код CloudTips для поддержки BROray" width="220">
  </a>
</p>

[Поддержать BROray через CloudTips](https://pay.cloudtips.ru/p/09b23d0a)

Прямая ссылка: `https://pay.cloudtips.ru/p/09b23d0a`

[Подробно о поддержке проекта](docs/support.md)

## Лицензия

Исходный код BROray распространяется на условиях [GNU General Public License v3.0](LICENSE). Xray-core не является частью исходного кода BROray и распространяется на собственных условиях, сохранённых в `third_party/xray/`.
