# Обязательные требования к релизам и обновлению BROray

Версия документа: **1.7**, принята 4 августа 2026 года для BROray 2.2.7 и последующих релизов.

Этот документ — канонический обязательный контракт. Код, тест или кандидат, расходящийся с ним, считается ошибочным даже при лабораторном `PASS`. Изменение архитектуры допускается только новой явно утверждённой версией требований.

## Канонические пути

```text
Рабочая транзакция: /tmp/broray-update-<operation-id>/
Единственная полная копия: /tmp/broray-update-<operation-id>/backup.tar.gz
Точный старый пакет: /tmp/broray-update-<operation-id>/previous.ipk
Точный кандидат: /tmp/broray-update-<operation-id>/candidate.ipk
Атомарная блокировка: /tmp/broray-update.lock/
Постоянный маркер: /opt/var/lib/broray/current-operation.json
Ограниченная история: /opt/var/lib/broray/operations/
```

Backup, candidate, previous/rollback IPK, staging и рабочая распаковка **не размещаются в `/opt`**.

## Неподлежащий изменению порядок

### REQ-UPD-001 — неизменяемый кандидат

Сначала создаётся immutable release candidate. Именно эти байты проходят лабораторный и полевой gate и только затем побайтово продвигаются в stable. Любое изменение создаёт новый SHA-256 и полный повтор тестов.

### REQ-UPD-002 — обязательный путь WebUI

Публичное обновление проверяется полным пользовательским путём:

```text
BROray → Проверить обновление → Установить обновление
```

Ручной `opkg install`, локальный IPK и runner не заменяют полевой тест WebUI.

### REQ-UPD-003 — блокировка и первая изменяющая операция

До очистки разрешены только read-only проверки, чтение metadata, атомарный `mkdir`-lock и создание минимального operation marker. После этого первой операцией, изменяющей управляемое хранилище, является очистка мусора.

### REQ-UPD-004 — allowlist-очистка

До backup и загрузки IPK удаляются только подтверждённые BROray-объекты:

- старые автоматические копии;
- остатки завершённых/устаревших транзакций;
- управляемые временные данные и release-артефакты;
- старые IPK BROray;
- временные данные маршрутов;
- OPKG-кэш.

Сначала формируется `cleanup-plan.json`/`cleanup-plan.txt` с путём, типом, размером, причиной и маркером принадлежности. Широкие непроверенные маски запрещены.

Не удаляются серверы, подписки, активный сервер, Xray config, настройки, пользовательские и установленные маршруты, DNS, Proxy0, ручные backup и активные журналы вне отдельной безопасной ротации.

### REQ-UPD-005 — место после очистки

После очистки повторно измеряется свободное место отдельно в `/opt` и `/tmp`; значения до/после сохраняются в журнале.

### REQ-UPD-006 — одна полная копия

Создаётся ровно одна полная копия BROray:

```text
/tmp/broray-update-<operation-id>/backup.tar.gz
```

Она содержит все пакетные файлы BROray, конфигурацию, пользовательские данные, серверы, подписки, маршруты, DNS-состояние, runtime, журналы, init-скрипты и внешние BROray-файлы, необходимые для точного возврата. Это полная копия BROray, а не всего Entware.

Отдельный user-data archive, payload archive или урезанный `preinst` backup не заменяют её.

### REQ-UPD-007 — согласованный снимок

Updater фиксирует исходное состояние пяти служб, копирует runner в `/tmp`, останавливает только ранее работавшие службы, ждёт завершения процессов, выполняет `sync` и только затем архивирует неизменяющееся дерево. Намеренно остановленная служба остаётся остановленной после успеха и отката.

Игнорировать `file changed as we read it` запрещено.

### REQ-UPD-008 — атомарный backup-контракт

Сначала создаётся `backup.tar.gz.part`; после полного `PASS` он атомарно становится `backup.tar.gz`.

Доказательства рядом:

```text
backup.tar.gz.sha256
backup-contract.json
backup-contents.txt
backup-metadata.txt
```

Они не являются дополнительными копиями. До продолжения обязательны `gzip -t`, полное чтение tar, проверка SHA/размера, обязательных групп, версии контракта, отсутствия абсолютных/`../` путей и самой транзакции внутри архива.

Начиная с 2.2.7 разрешён один полный формат. Старый updater поддерживается только bridge, создающим тот же контракт до изменения файлов; иначе установка блокируется.

### REQ-UPD-009 — точный previous IPK

После проверки backup, но до кандидата, получается точный `previous.ipk`. Проверяются package name, version, architecture, доверенный SHA-256, control/data archives и безопасные пути. Без точного старого IPK установка не начинается.

### REQ-UPD-010 — точный candidate IPK

Только после backup и previous IPK загружается кандидат по точному `Filename` из OPKG metadata без HTTP-cache. Проверяются SHA-256, размер, version, architecture, release ID, revision, control/data archives, пути и release manifest.

### REQ-UPD-011 — раздельный расчёт места

Для `/opt` учитываются `X-BROray-Required-Opt-KB`, OPKG reserve и system reserve. Для `/tmp` — backup, previous/candidate IPK, logs, validation extraction и safety reserve. `/tmp` проверяется до backup и повторно после него. Недостаток места блокирует операцию до `opkg install`.

### REQ-UPD-012 — единый runner

Update и reinstall используют один transaction runner из `/tmp`; различается только версия candidate. Переустановка не использует `opkg --force-reinstall` и не пересекается с отложенной очисткой предыдущей операции.

### REQ-UPD-013 — package hooks

Updater передаёт `preinst` operation-id, update dir, backup path/SHA/contract, `BRORAY_BACKUP_VERIFIED=1` и services-before. `preinst` проверяет контракт, не создаёт второй backup, не повторяет cleanup и не переносит транзакцию в `/opt`.

Прямой административный OPKG-путь обязан создать тот же полный контракт либо остановиться до изменения payload.

### REQ-UPD-014 — журналы операции

У каждой операции собственные `operation.json`, `transaction.log`, `opkg.log`, `preinst.log`, `postinst.log`, `postcheck.log`, `rollback.log`. Полный stdout/stderr сохраняется; до rollback пользователю показываются stage, command, exit code и диагностический tail. Другая операция не может перезаписать причину ошибки.

### REQ-UPD-015 — postcheck

С ограниченными повторами проверяются:

- application/OPKG version, release ID, WebUI build;
- исходный набор пяти служб;
- Xray config;
- фактический SOCKS;
- Proxy0;
- WebUI по реальному LAN listener;
- DNS-over-TLS;
- фактические маршруты Keenetic;
- active server, server/subscription counts;
- protected-data manifest и зарегистрированные migrations.

Успех — только после двух последовательных полных `PASS`. PID сам по себе не доказывает работоспособность.

### REQ-UPD-016 — protected-data manifest

До операции создаются `protected-before.sha256` и `protected-before.json` как доказательства, не как второй backup. Файлы делятся на immutable, migratable и runtime. Изменение migratable допустимо только зарегистрированной migration со схемой до/после и тестом сохранения пользовательского смысла.

### REQ-UPD-017 — откат

При ошибке после изменения дерева:

1. сохраняется полный failure evidence;
2. останавливаются новые службы;
3. старое дерево возвращается из `backup.tar.gz`;
4. устанавливается exact `previous.ipk` для OPKG metadata/hooks;
5. backup повторно накладывается для local conffiles/state;
6. возвращается исходный набор служб;
7. проверяются старая версия, OPKG, WebUI, Xray, SOCKS, Proxy0 и protected data.

Rollback успешен только после полного старого postcheck. При неудаче transaction dir не удаляется, WebUI показывает: `Откат не завершён. Не перезагружайте роутер.`

### REQ-UPD-018 — завершение

После успеха удаляются backup, previous/candidate IPK, staging и working extraction; снимаются lock и current marker. В `/opt/var/lib/broray/operations/` остаются только ограниченная summary/evidence без backup и IPK.

### REQ-UPD-019 — перезагрузка во время операции

`current-operation.json` обновляется атомарно по этапам. Если после reboot `/tmp` исчез, новая операция не стартует, прошлой не присваивается успех, WebUI показывает interruption и запускает read-only diagnostics. Автооткат без проверенного backup запрещён.

### REQ-UPD-020 — OPKG metadata и старые версии

Чтение работает с `Packages` и локальным `src/gz`; отсутствие `SHA256sum` — metadata error, не checksum mismatch. Для каждой поддерживаемой старой версии проверяется реальный WebUI-переход. Минимальный compatibility bridge не устанавливает release, а только приводит старый updater к этому контракту.

### REQ-UPD-021 — реальный `/opt`, services и conffiles

Payload/rollback применяются относительно фактического mount/symlink `/opt`. Functions recording services явно возвращают `0` при успехе. Остановленная необязательная служба не является ошибкой backup. Modified conffiles не превращают успешную установку в разрушительный rollback.

### REQ-UPD-022 — публикация

Публичные `.sh`/`.txt` имеют права не строже `0644`. Публикация проверяет HTTP 200, size и SHA-256 внешним клиентом. Publication archive и detached command выдаются вместе с отдельными SHA-256.

## Операции и совместимость

### REQ-OPS-001 — lock без `flock`

Lock создаётся атомарным `mkdir`; `flock` не требуется. В lock-dir записываются operation-id, pid, timestamps, operation type и versions. Stale lock не удаляется без проверки PID и current marker.

### REQ-OPS-002 — постоянный журнал

В `/opt/var/lib/broray/operations/` допускаются только bounded summary/logs. Backup, IPK и staging запрещены. История ограничивается количеством и суммарным размером.

### REQ-COMP-001 — PATH и `/opt/tmp`

Canonical PATH:

```text
/opt/sbin:/opt/bin:/usr/sbin:/usr/bin:/sbin:/bin
```

До OPKG проверяется штатный `/opt/tmp -> /tmp`. Отсутствующий неактивный `opkg.lock` не является ошибкой.

### REQ-COMP-002 — зависимости и диагностика

Нельзя вводить обязательные `flock`, `paste` или GNU-only options без package dependency, preflight и physical test. Metadata collector имеет fallback, сохраняет stderr и не трактует `symlinks=0`/`invalidJson=0` как ошибку без отдельного failure.

Processes определяются устойчивой argv-signature, а не обязательным полным путём. WebUI проверяется по фактическому LAN listener, не только `127.0.0.1`.

## Обязательная трассировка

Каждое требование имеет связь:

```text
requirement → implementation → automated test → evidence
```

Кандидат содержит `requirements-traceability.json`. Builder отклоняет кандидат при отсутствующем ID, implementation/test/evidence, failed test или запрещённом transaction path.

Static gate запрещает рабочие пути:

```text
/opt/var/tmp/broray-transactions
/opt/broray/backup/user-data-before-*
/opt/.../candidate.ipk
/opt/.../previous.ipk
/opt/.../rollback.ipk
```

## Минимальная release matrix

Обязательны:

- две побайтово одинаковые deterministic builds;
- BusyBox ash, JavaScript и JSON checks;
- exact manifest/control/metadata/IPK consistency;
- доказанный порядок `cleanup < backup < previous < candidate download < install`;
- allowlist cleanup без изменения protected data;
- ровно один backup и отсутствие transaction artifacts в `/opt`;
- damaged/missing backup/previous/candidate rejection до install;
- regression test `file changed as we read it`;
- S28 stopped-before preserved after success/rollback;
- `/opt` and `/tmp` low-space gates;
- one runner for update/reinstall;
- `preinst` no-second-backup test;
- failures in preinst, partial OPKG extraction, postinst, service start and final postcheck;
- exact rollback of package files, OPKG metadata, user data and service set;
- retry after rollback and idempotent reinstall;
- WebUI polling across lighttpd restart;
- two consecutive postcheck PASS;
- separate Xray config/SOCKS/Proxy0/WebUI tests;
- DNS 9 requested/8 effective migration without automatic Keenetic change;
- routes, servers, subscriptions and active server preservation;
- no `flock`/`paste` dependency;
- `/opt/tmp -> /tmp`, BusyBox `ps` and real LAN-listener tests;
- stale lock/reboot interruption gate;
- real WebUI upgrade from every supported old version and physical clean install;
- real `/opt` mount/symlink test and modified conffiles test;
- public normal/no-cache downloads equal `Packages`;
- publication files HTTP 200, size, SHA and permissions;
- field test of immutable bytes before stable/docs switch;
- UI regression: operation card placement and stable `#cleanup-state` anchor.

## Запреты

Запрещено:

- менять published artifact под прежним именем;
- публиковать `Packages` раньше artifacts;
- объявлять field success по simulation;
- переводить candidate в stable до physical WebUI test;
- скрывать OPKG output общей фразой;
- оставлять revoked artifacts в public channel;
- хранить backup/candidate/previous/staging на `/opt`;
- заменять один полный backup частичными архивами;
- менять порядок ради optimization без новой утверждённой версии;
- считать тест достаточным, если он проверяет альтернативную архитектуру;
- добавлять dependency без package/preflight/physical gate.
