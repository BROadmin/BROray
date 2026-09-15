# Capability-контракт r14c01 для Keenetic / Entware

Версия совместимого идентификатора: **`keenetic-entware-capabilities/1`**  
Активная схема описания: **3**  
Дата фиксации: **14 августа 2026 года**

Этот документ нормативно описывает только активный путь
`r14c01 clean bootstrap -> broray-updater/5 app-slot lifecycle`.
Активный selector: `clean-bootstrap-functional-gates-plus-keenetic-aarch64`.
Идентификаторы `keenetic-entware-capabilities/1`, `broray-space/2` и
`bro-any-structural` сохранены ради совместимости с метаданными кандидата. Они
не означают, что в r14c01 активны универсальная транзакция полного IPK,
миграция произвольного старого BROray или прежняя snapshot-модель.

## Основание в официальной документации

- Компонент Open Package монтирует хранилище как `/opt`, запускает
  `/opt/etc/initrc` либо init-скрипты в алфавитном порядке, ограничивает
  системный hook 24 секундами и публикует OPKG `PATH`:
  <https://support.keenetic.com/>.
- Entware публикует разные репозитории для разных архитектур, а standard и
  alternative installations имеют различающийся состав BusyBox:
  <https://bin.entware.net/Readme.txt>.
- Наличие и options BusyBox applets определяются конкретной сборкой:
  <https://busybox.net/downloads/BusyBox.html>.
- OPKG получает architectures, priorities и destinations из своей
  конфигурации:
  <https://github.com/oe-mirrors/opkg/blob/master/man/opkg.conf.5.in>.
- KeeneticOS предоставляет управляемые интерфейсы ProxyN:
  <https://support.keenetic.com/>.
- DoT/DoH являются штатными возможностями KeeneticOS:
  <https://support.keenetic.com/>.

Официальная документация задаёт платформенные возможности, но не доказывает
работу конкретных байтов кандидата. Физический запуск на совместимом Keenetic
с архитектурой `aarch64` остаётся отдельным gate.

## Поддерживаемый жизненный цикл

| Операция | Единственный поддерживаемый путь | Что не используется |
|---|---|---|
| Чистая установка | metadata-only IPK из immutable feed; `preinst` и `postinst` r14c01 | overlay старой установки, migration старого BROray |
| Обновление приложения | authenticated WebUI -> постоянный `broray-updater/5` -> новый app-slot | `opkg upgrade`, полный IPK, legacy universal transaction |
| Переустановка той же версии | authenticated WebUI -> source receipt активного slot -> новый app-slot | сохранённый исторический IPK |
| Обычное удаление | authenticated WebUI -> exact operation-id authorization -> `opkg remove` -> copied worker finalization | прямой CLI remove |
| Полное удаление | тот же путь, но без сохранения внешнего пользовательского архива | принудительное удаление в обход WebUI |

Прямые OPKG upgrade/reinstall/remove отклоняются. Чистая установка отклоняет
любую существующую регистрацию или residue BROray. В raw OPKG status допустимы
либо ноль BROray stanzas, либо ровно один физический tombstone с уникальными
полями `Package: broray`, `Version: 3.0.0-r14`,
`Architecture: aarch64-3.10` и
`Status: install prefer,user not-installed`. Такой tombstone может остаться
после неудачного физического bootstrap, когда `opkg status broray` уже
возвращает rc=0 с пустыми stdout/stderr. Любое другое значение, дублирование
core fields или несколько BROray stanzas остаются fail-closed. Единственное
разрешённое внешнее пользовательское состояние — `/opt/broray-preserved`,
созданное обычным WebUI-удалением; оно принимается только как безопасный
checksummed архив с точным protected manifest и allowlisted пользовательскими
roots.

Проверка этого raw-status boundary, пустого `opkg status broray`, отсутствия
`/opt/lib/opkg/info/broray.*`, payload и recovery authorization выполняется
внешним `INSTALL-ON-ROUTER.sh` до публикации feed и до `opkg install`.
`preinst` не читает status/info: во время hook OPKG уже может показывать записи
входящего пакета, которые не являются остатком прежней установки.

## Точные функциональные gates совместимого Keenetic aarch64

До мутации требуются одновременно:

1. запуск от `root`, executable `/opt/bin/ash`, canonical
   `PATH=/opt/bin:/opt/sbin:/opt/usr/bin:/opt/usr/sbin:/bin:/sbin:/usr/bin:/usr/sbin`
   и `LC_ALL=C`;
2. ровно один непустой диагностический `hw_id`, ровно один `arch: aarch64` и
   непустой диагностический `title` из `show version`; значение `hw_id`
   сохраняется как read-only evidence и не является model selector;
3. Entware architecture `aarch64-3.10` в `opkg print-architecture`;
4. ровно по одному component token `proxy`, `opkg`, `ndns`;
5. успешное выполнение read-only API `show version`,
   `show version | grep components`, `show ndns`, `show ip http proxy` и
   `show running-config`; пустой stdout для ещё не настроенных ndns/http-proxy
   допустим, но rc должен быть нулевым;
6. ровно одна строка `opkg disk ...` в running configuration;
7. чистый filesystem/OPKG target и установленные фиксированные минимумы места.

Версия KeeneticOS, версия BusyBox и `uname -m` не являются compatibility
branches. После скачивания и полной проверки всех объектов `postinst` повторно
проверяет непустой `hw_id`, `arch: aarch64`, title, компоненты, read-only API,
один OPKG disk,
отсутствие чужого `ip http proxy broray` и однозначный LAN-IP. Entware
`print-architecture` и свободное место непосредственно перед первой постоянной
записью повторно не измеряются; это явно отражено в machine-readable контрактах.

## Чистая установка и content binding

До первой постоянной записи `postinst` обязан:

- скачать по HTTPS versioned index, app bundle, updater-v5 platform bundle и
  Xray 26.7.28 для `aarch64-3.10`;
- проверить опубликованные размер и SHA-256 каждого объекта;
- отклонить unsafe/duplicate archive paths и любые объекты, кроме regular file
  и directory;
- извлечь app/platform только в operation-local `/tmp`;
- доказать полный exact file set через внутренние `SHA256SUMS`;
- проверить BusyBox ash syntax, JSON, app-slot metrics, одинаковый Xray wrapper
  и точные bytes updater-v5;
- распаковать и функционально проверить точный Xray binary; его установленная
  identity: 35 389 566 bytes и SHA-256
  `4b8af237444801bf17b3dc10a1c5c24581fbe3d433eba3d78c6c3a0da1df56fc`.

После начала bootstrap временные ownership sentinels ограничивают cleanup
объектами, созданными этим запуском. Ошибка до commit вызывает попытку удалить
только bootstrap-owned filesystem objects и созданные по receipt Keenetic
objects. Это best-effort cleanup, а не прежний полный snapshot rollback.

Commit требует точный current-slot manifest, одну shared Xray runtime,
`broray-updater/5`, рабочие S22/S24/S25, owned ProxyN, scoped owned KeenDNS HTTP
Proxy, HTTP 200 на локальном корне WebUI и HTTP 401 на локальном
`/api/session.cgi`. Первичный Xray outbound остаётся fail-closed `blackhole` до
выбора пользователем сервера.

## Updater-v5

`broray-updater/5` меняет только компактный app-slot и не запускает OPKG.
Обновление требует свежий (не старше часа) channel index и иной candidateId.
Переустановка берёт URL, SHA-256, size и app metadata из durable source receipt
активного slot.

Перед переключением updater:

- проверяет внешний bundle size/SHA-256, структуру, полный `SHA256SUMS`, release
  metadata и app-slot metrics;
- удаляет прежний rollback-slot, проверяет место и готовит новый slot;
- сохраняет состояние пяти служб S23/S24/S25/S27/S28 и останавливает только
  ранее запущенные;
- переключает `current` двумя same-filesystem rename с durable `switch.phase`.

После переключения проверяются slot tree, Xray config, ранее запущенные службы и
фактический JSON `broray-system info`, включая version/release/WebUI identity и
health. Ошибка возвращает предыдущий slot двумя обратными rename. После crash
daemon завершает rename, откатывает его или останавливается с
`RECOVERY_LAYOUT_AMBIGUOUS`; неоднозначность автоматически не угадывается.
Shared `/opt/broray/runtime/xray` и пользовательские config/routes при app
update/reinstall не копируются.

## Блокировки и evidence

Активный updater использует отдельные mkdir-fences:

```text
/opt/var/lib/broray-updater/request.lock
/opt/var/lib/broray-updater/daemon.lock
```

Application operations используют
`/opt/var/lock/broray/global-operation.lock`: после публикации своего fence они
повторно проверяют updater `request.lock`. Updater выполняет симметричный
fail-closed admission: до создания `request.lock`, после атомарной публикации в
нём `operation-id`/PID/process-start, после передачи ownership daemon, после
download непосредственно перед первой мутацией release tree и после staging
перед state seed/остановкой служб/slot switch. На каждой проверке блокируют
любой присутствующий либо неоднозначный common global object, legacy
`/tmp/broray-global-operation.lock` и running/resumable route state. Updater не
удаляет и не исправляет чужой fence; конфликт завершается
`GLOBAL_OPERATION_CONFLICT` без изменения активного slot.

Точный durable route gate читается из
`/opt/broray/routes/operations/*.json`; running либо resumable запись блокирует
admission updater.

Это userspace cross-fence coordination, а не доказательство native OPKG
advisory lock. Updater-v5 не создаёт native OPKG FIFO owner/FD proof, не держит
OPKG lock и не пишет OPKG status/info.

Durable updater evidence ограничено фактически создаваемыми объектами:

```text
/opt/var/lib/broray/operations/<operation-id>/state.json
/opt/var/lib/broray/operations/<operation-id>/log.txt
/opt/var/lib/broray/last-operation
/opt/var/lib/broray-updater/slots/<slot>.json
/opt/var/lib/broray-updater/updater.log
```

Чистая установка доказывает результат rc OPKG hooks, точными postconditions и
stdout, но не создаёт прежний durable per-probe argv/stdout/stderr hash
envelope. Активный lifecycle также не создаёт native-lock evidence, полный
mount graph или legacy full-IPK transaction envelope. Наличие неиспользуемого
legacy-кода в app-slot не делает его активным call graph.

## Space contract

Чистый `preinst` использует фиксированные fail-closed пороги:

```text
/opt free >= 67 108 864 bytes
/tmp free >= 78 643 200 bytes
/tmp allocatable inodes >= 4096
```

Свободные bytes измеряются через поддерживаемый `LC_ALL=C df -Pk`. Наличие
inode проверяется фактическим созданием и удалением 4096 пустых файлов внутри
одного приватного `mktemp`-каталога на `/tmp`; неподдерживаемый целевым BusyBox
`df -Pi` не используется. Immutable bootstrap одновременно удерживает не более
341 собственных объектов в `/tmp`, поэтому probe оставляет reserve 3755
объектов. Для slot metrics используются `find -xdev -printf` и `wc -l`. Эти
пороги покрывают утверждённый immutable r14c01, но не являются
универсальной формулой произвольного source tree и не суммируют `/opt`/`/tmp`
по mount identity.

Updater перед download требует `/tmp` free bytes не меньше bundle size; после
безопасного удаления старого rollback-slot он требует `/opt` free bytes не
меньше published logical app bytes плюс marker. Он не дублирует shared Xray,
сохраняет не более current+одного rollback slot и после staging повторно
сверяет logical bytes/file/directory/max-file metrics. Это не allocation upper
bound: updater не делает inode gate и не доказывает peak
`bundle + extraction` на `/tmp`.

Clean bootstrap принимает только HTTPS URL и запрещает downgrade любого
redirect через `curl --proto '=https' --proto-redir '=https'`. После загрузки,
проверки размеров/SHA-256, архивов, manifests, Xray и read-only Keenetic gate
полная clean filesystem boundary повторно проверяется непосредственно перед
первой persistent записью: поздний чужой объект никогда не перезаписывается.

Если `postinst` вернул nonzero и OPKG оставил failed/unpacked registration,
повтор разрешён только immutable router installer. Он обязан доказать exact
control/hooks кандидата и отсутствие app/platform payload, опубликовать свежий
operation-bound marker owner `BROray-R14C01-Router-Installer`, выполнить
только recovery remove и доказать отсутствие status/info/payload/marker.
Обычный CLI remove и неоднозначная или чужая registration остаются fail-closed.

Удаление до мутации создаёт и проверяет gzip snapshot защищённых roots. Для
normal дополнительно до OPKG commit создаётся checksummed архив в
`/opt/broray-preserved`; full не оставляет внешний архив. Отдельной advance
space formula нет: невозможность записать/проверить snapshot обязана остановить
операцию до мутации.

## Владение объектами Keenetic

- LAN-IP выбирается только как однозначное пересечение адресов configured
  interface с единственным exact `security-level private` и live RFC1918
  bindings. `protected`, `public`, имя interface, `127.0.0.1`,
  `192.168.1.1`, `br0` и «первый RFC1918» не являются defaults. При нуле или
  нескольких private matches операция блокируется, а stable diagnostic code и
  агрегированные counts выводятся до очистки временной области.
- BROray использует свободный либо уже точно принадлежащий ему ProxyN и
  receipt-bound protocol/upstream/link checks. Чужой ProxyN не перезаписывается
  и не удаляется.
- `ip http proxy broray` создаётся/удаляется только с exact scoped receipt.
- DoT/DoH apply/delete использует receipt-scoped transaction; лабораторный
  fixture проверяет восстановление live и local state при HUP/INT/TERM.
- WebUI запускает private `broray-lighttpd` identity и не полагается на
  basename-wide управление Entware `S80lighttpd`.

## Удаление и сигналы

WebUI требует точную фразу подтверждения и создаёт authorization с exact
operation-id, mode `normal|full` и возрастом не более 300 секунд. `prerm` и
`postrm` перепроверяют ту же identity; `postrm` выполняет только preflight и
требует остановленные службы. Metadata-only IPK не содержит app payload.

Copied WebUI worker заранее проверяет ownership, делает rollback snapshot,
фиксирует состояние, удаляет только managed routes/ProxyN/HTTP Proxy/DoT и
останавливает службы. HUP/INT/TERM до OPKG commit запускают восстановление
snapshot, Keenetic objects, routes и исходного набора служб. В commit window
эти три cooperative signal игнорируются и наследуются OPKG. Только после rc=0,
пустого `opkg status broray` и отсутствия `/opt/lib/opkg/info/broray.*` copied
worker удаляет owned payload. Normal публикует проверенный `LATEST`, full
проверяет отсутствие preserved root.

SIGKILL либо потеря питания между OPKG commit и окончанием payload finalization
не объявлены лабораторно доказанной гарантией. Это отдельный физический
crash/recovery gate.

## Лабораторная и физическая валидация

`validate-r14c01.sh` работает с final extracted bytes server carrier. Он
проверяет наружные/внутренние hashes и manifests, BusyBox ash/JSON, updater
update+reinstall+rollback, ownership/process fixtures, DoT signal rollback и
normal/full/signal uninstall lifecycle. Результат имеет смысл
`LAB_EXERCISED`, не `production-authorized`.

Сборка разделена на две fail-closed фазы. `probe` содержит включённое
реверсивное write-поведение в точных целевых
publication/IPK/app/router-installer bytes, но это не является утверждением о
сертификации. Его server installer безусловно отказывает в публикации до любой
мутации server filesystem. `R14C01-PHYSICAL-WRITE-PROTOCOL.json` и
`R14C01-PROBE-BINDINGS.json` связывают exact IPK/app и активные write-source
SHA-256. Физические Keenetic CLI transcripts должны привязать SHA-256 этого
протокола и отдельно сертифицировать Proxy interface, HTTP Proxy, DoT и routes.
После физического PASS целевые bytes менять запрещено. Только затем
`publication` carrier воспроизводит те же target publication bytes и добавляет
внешний exact receipt; final validator и server installer повторно сверяют его
с фактическими publication bytes. Read-only evidence этот гейт не заменяет.

Перед stable promotion на exact candidate SHA-256 и физическом совместимом
Keenetic aarch64 должны быть отдельно подтверждены:

1. чистая установка и фактическое ограничение Keenetic hook менее 24 секунд;
2. direct LAN WebUI и session boundary;
3. KeenDNS TLS/session enforcement;
4. реальный SOCKS traffic через owned ProxyN;
5. live update и same-version reinstall с сохранением/rollback служб;
6. normal uninstall -> clean reinstall -> byte-exact user-data restore;
7. full uninstall -> clean reinstall;
8. reboot, safe Entware unmount и сосуществование с `S80lighttpd`;
9. наблюдение SIGKILL/power-loss на границе uninstall commit/finalization.

У r14c01 `stable:false`. Отсутствие физического evidence не маскируется
успехом контейнерных fixtures.

## Снятые утверждения

Для r14c01 не являются активными и не должны использоваться как PASS:

- приём и миграция произвольного установленного BROray (`bro-any-structural` в
  его прежнем смысле);
- универсальная update/reinstall/restore транзакция полного IPK;
- native OPKG FIFO owner/FD/contention proof как блокировка updater-v5;
- полный per-probe argv и stdout/stderr SHA-256 evidence envelope;
- mount graph и combined `/opt`+`/tmp` phase planner;
- full snapshot/gzip/hardlink formulas прежнего `broray-space/2`;
- утверждение, что LAB result сам по себе разрешает stable promotion.
