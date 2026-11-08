# Расширение импорта протоколов: согласованный первый этап

Владелец принял предложение: SOCKS5, HTTP/HTTPS proxy, WireGuard INI/Xray JSON, Hysteria2 Xray JSON, параметры WS/HTTPUpgrade/gRPC/sockopt и отсутствие незаметной потери настроек. Реализация локально поверх подготовленного AWG. Публикация и изменения роутеров этим этапом не выполняются.

## Выбранный подход

Расширить нынешний backend Xray 26.10.31 и существующий каталог серверов. Второе ядро потребовало бы отдельного управления процессами и обновлениями; оно не нужно для согласованных протоколов. Произвольный запуск чужого Xray JSON нарушил бы управление BROray собственными входами, DNS и маршрутами, поэтому импортируем только проверенные настройки подключения.

## Объём

- SOCKS5: socks:// и socks5://; HTTP proxy: http:// и https://. IPv4/IPv6/DNS, порт, percent-encoded логин/пароль, TLS SNI/ALPN/fingerprint/pinning. HTTP CONNECT несёт TCP; не обещать ему UDP. Xray JSON принимает плоский и servers/users форматы без молчаливого отброса настроек.
- WireGuard: готовый клиентский INI и Xray JSON. PrivateKey/SecretKey, Address, peers, PublicKey, PresharedKey, Endpoint, AllowedIPs, PersistentKeepalive, MTU, Reserved и DNS/remoteDNS. Выполнение в Xray userspace с noKernelTun=true, без собственного интерфейса ОС. Исполняемые INI-директивы и AWG-параметры в обычном WG отклоняются, а не теряются. Ключи полностью маскируются в публичном API.
- Hysteria2 Xray JSON: version=2, auth, адрес/порт, TLS, UDP timeout, finalmask; сохранить смысл уже поддержанных ссылок. Не обещать параметры, которых нет в установленном ядре.
- WS: headers, heartbeatPeriod. HTTPUpgrade: headers. gRPC: idle_timeout, health_check_timeout, permit_without_stream, initial_windows_size, user_agent. sockopt: tcpFastOpen, tcpKeepAliveIdle, tcpKeepAliveInterval, domainStrategy. Остальные поля требуют явного отказа, особенно привязка интерфейса/mark/dialerProxy/customSockopt.
- XHTTP/TLS/REALITY/FinalMask: покрыть тестами перенос уже поддержанных полей и устранить найденные различия между URI/JSON/сохранением/генератором. Не включать непроверенные сочетания автоматически.
- Необработанные поля: явно сообщать имена без значений. Неподдержанные параметры подключения отклонять; известные метаданные сопровождать предупреждением. Не менять семантику активного подключения при импорте.
- WebUI: все новые формы проходят реальный preflight/preview/save; INI вводится одним многострочным профилем. Сохраняется предупреждение и запрет подключения для AWG.

## Приёмка и ограничения

Для каждой формы: разбор → валидная запись → backup/sync → Xray config. Позитивные/негативные, секреты, IPv6, конфликтующие/неизвестные поля. Целевые Go RED/GREEN, регрессии servers/subscriptions, Node настоящий JS, затем настоящий Xray в offline Linux VM для конфигураций и трафика. Не подменять отсутствие UDP у HTTP успешной проверкой TCP. Первые сбои сохранять и разбирать. Старые аппаратные циклы не повторять.

MASQUE/XDRIVE, TUIC/MTProto/SSR/Snell, подключение AWG, изменение идентичности SNI/ShortID и политика активного исчезнувшего сервера не входят в этот пакет. Они остаются отдельными задачами.
