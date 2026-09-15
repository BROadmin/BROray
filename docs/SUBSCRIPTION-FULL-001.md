# Подписки: отмена в установленном приложении и исправление DNS

На полном r02c01 выполнена WebUI-последовательность: создать временную подписку без автоматики, запустить загрузку с управляемого внешнего HTTP-источника, остановить её во второй вкладке BROray. Операция `op-20260915222904-25546-eda4405775ea` завершилась `aborted/CANCELLED`; supervisor registry пуст, HTTP-клиент отключился, полная identity постоянного Xray совпала. Временный источник завершился, подписка удалена через WebUI, полный набор файлов config/servers и hashes восстановлен. Xray работал с blackhole; это ещё не проверка VPN-трафика.

## Два воспроизведённых дефекта

1. Реальный BusyBox nslookup Keenetic возвращает `Address 1: IP reverse.name`. Старый awk выбирал последнее поле — reverse name. Поэтому подписка с доменным именем завершалась HTTP_ERROR, хотя прямой curl к источнику был доступен. Новый разбор выбирает поле адреса, исключает DNS-сервер и поддерживает обе формы Address. Проверка literal-адресов отвергает имена, неоднозначные/некорректные IPv4, private/reserved IPv4 и IPv6 вне global unicast; mapped IPv6 намеренно не принимаются.
2. После отмены read-only проекция показывала сообщение о прерывании вместе с кодом HTTP_ERROR и длительностью предыдущей попытки. Теперь подтверждённое terminal состояние заменяет результат текущей попытки на CANCELLED или OPERATION_INTERRUPTED, использует фактическое время завершения и не выдумывает длительность. Тот же смысл сохраняет admitted stale recovery. Чтение не записывает durable metadata. UI не выводит длительность, если она неизвестна.

## Проверки исправления

- 16 Linux PASS: 6 DNS/projection, 10 полных subscription jobs (включая cancel/drain, конфликт admission, CLI/CGI, scheduler, настоящий parser и публикацию каталога); 234,23 с.
- 5 physical prefix PASS: реальный DNS Keenetic, реальный curl и SHA подписанного immutable index, literal/private validation, read-only CANCELLED projection, admitted recovery. Установленный app не изменялся; private namespaces архивированы и удалены.
- 36 full-page UI PASS с подменой только HTTP-ответов: 1440/1024/390/360, BROray/night/day, duration null/0/8000, отсутствие overflow и старого HTTP_ERROR. Скриншоты 360/day и 1440/BROray просмотрены.
- Первый UI harness ошибочно не отдавал `ok:true` в session; приложение корректно перенаправляло на вход. Исправлена fixture, повтор полностью PASS. Ошибка сохранена отдельно.

Evidence: `full-subscription-cancel-20260916`, `physical-subscription-presentation-20260916`, `subscription-presentation-ui-20260916-02`, frozen `subscription-presentation-001`. Новые исправления ещё требуют установки следующим подписанным кандидатом и полного повторения WebUI-пути с доменным именем.

Предыдущий этап update/reinstall закреплён как FULL-UPDATE-REINSTALL-001. Обновление r01→r02 и переустановка r02 прошли через WebUI, настройки и доступ сохранены. Stable не менялся. Это промежуточный этап, не готовность релиза; ROOT_CAUSE_NOT_PROVEN исходного инцидента сохраняется.
