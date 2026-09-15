# Полная установка и первый вход 3.1.1-r01c01

Кандидат из commit `2f3b685` установлен на разрешённый тестовый Keenetic Peak KN-2710, KeeneticOS `5.01.C.4.0-1`. Archive SHA-256: `7ba42acc7862ca03df7cccbfa968c5fa98d20ba83f510fd860423b24e2dc3594`, 2 064 588 байт. Сборка повторяема; установка реальным metadata-only OPKG, без поддельной регистрации пакета.

Выполнены capability/clean preflight, приватные snapshots/backup, установка Entware dependencies, передача и повторная SHA-проверка всех входов, полный postinst, OPKG `install user installed`, проверка всех файлов current, Xray binary/config, служебных процессов и HTTP/auth boundary. Настоящий Xray 26.9.9 запущен с первоначальным blackhole outbound. VPN-трафик ещё не проверялся.

Запущены updater-v5, WebUI, Xray, планировщик подписок и connection-monitor. Выполнен настоящий вход в BROray и Keenetic с разрешёнными пользователем учётными данными; пароль в артефакты не сохранялся. Через WebUI выполнена остановка фоновой работы при отсутствии jobs: общая пауза установлена, полная identity постоянного Xray не изменилась.

Сравнение startup-config подтвердило неизменность вне добавленных принадлежащих BROray ProxyN и HTTP proxy. В running-config дополнительно изменились только экспортные метаданные Agent `coala/rci` → `cli` и Username `admin` → `root`. Исходная проверка с REVIEW_REQUIRED и отдельное разрешение различий сохранены; сеть и файлы доступа не изменялись.

## Первый запуск: найденный интеграционный дефект

На полном fresh install панель фоновых операций и журнал возвращали недоступное состояние. Каталог operations существовал, но `operations.guard` ещё не был создан: read-only client намеренно не создаёт его. До правок зафиксированы реальная UI-ошибка, пустой ответ/exit 1 и отсутствие guard (`first-start-baseline.json`).

Исправление: `broray-runtime-prepare` вызывает отдельную `initialize` координатора под native guard до запуска WebUI. Создаются только каталоги и guard. Initialization не восстанавливает операции, не удаляет fence, не сбрасывает pause/journal. GET остаётся read-only.

Проверки исправления: 16 Linux PASS (4 init + 12 CGI), 69,81 с; 3 physical ARM64 PASS на точном префиксе production startup и приватном state. Route/DoT/platform preparation не выполнялись в focused prefix test. Полный установленный r01c01 на момент этих проверок ещё содержит прежние байты; исправление должно войти в следующий кандидат и пройти обновление/повторную полную проверку.

## Ограничения тестового установщика

Локальный transport строго привязан к четырём входам и их hashes. На ошибке он сохраняет частичную установку, вместо непроверенного рекурсивного rollback старого hook. Это тестовый пакет, не подписанный публичный release bootstrap. Ключи подписи updater не менялись. Время сборки r01c01 ошибочно задано 00:00 UTC следующего по UTC дня; tar выдал предупреждения о будущем mtime. Следующая сборка должна использовать фактическое UTC, часы роутера не менялись.

Evidence: `docs/evidence/full-install-20260916/`, `physical-initialize-20260916/`; приватные конфигурации и logs — `.private/router-install-20260916/`. Релиз, update/rollback, длительный VPN-прогон и delivery на старую заблокированную 3.1.0 остаются открытыми.
