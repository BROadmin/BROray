# Передача операции фоновому исполнителю

Статус: coordinator/client реализованы и проверены отдельно; существующий Xray CGI ещё не переведён на новый протокол. Production установка не выполнялась.

## Authority и gate

- `owner.json` и его копия в fence остаются immutable generation descriptor. Их байты не меняются при handoff.
- `executor.json` — единственная атомарно заменяемая запись текущей executor identity, нового случайного token и подтверждения приёма. До первого handoff её отсутствие нормально; malformed/symlink executor не означает возврат к прежнему owner.
- Старый token сохраняется только в виде SHA-256 приглашения. `handoff` подтверждает передачу, но не возвращает новый token прежнему caller. Повтор подтверждения не меняет authority и остаётся read-only даже после завершения worker.
- Worker наследует operation ID, прежний token и private handoff nonce, остаётся за gate и вызывает `accept-handoff` от своей полной identity. Только этот worker получает новый token. Повтор ответа после потери связи не создаёт новую запись.
- `tick`, обычный `ack`, регистрация helper и `finish` до принятия handoff запрещены. Старый CGI cleanup после передачи не может завершить работу нового исполнителя.
- Handoff пока разрешён только для `xray:install/update/reinstall` в фазе `working`, до commit и при отсутствии children/domain pending/cancel. Он является частью протокола **подготовки до изменения Xray**. Расширять на другие producers можно только после проверки их pre-mutation границы.
- После доказанной смерти непринявшего работу worker можно снять pre-admission fence даже для protected операции. Смерть принявшего protected worker сама по себе не разрешает discard domain commit; остаётся recovery соответствующего transaction state.
- Status/classify/recovery/journal/новые helper регистрации используют актуального executor. Публичный статус показывает фазу ожидания, пока приглашение не принято.

## Проверки

- Повторный прогон затронутых suite: 97 PASS, 618,09 с (54 operations, 7 projection, 12 CGI, 8 supervisor integration, 13 handoff protocol, 3 actual handoff). Native binaries не менялись; их отдельные проверки остаются в SUPERVISOR-001. Runtime hashes VM совпадают с этим этапом.
- 13 Linux protocol cases с simulated identities: повтор ответов, прежний token, чужое приглашение, PID reuse, гибель parent/worker, до/после ack, commit/cancel boundaries, live child, malformed executor.
- 3 actual Linux process cases: gate до передачи, production client обеих сторон, native helper следит за новым worker после смерти прежнего owner.
- 9 физических cases на ARM64 с настоящими coordinator/client/native: `../../docs/evidence/physical-handoff-20260915/`. Тестовый namespace сохранён и удалён; Xray и другие службы не устанавливались.
- Первый actual-process прогон обнаружил ошибку fixture: её Python subreaper не забирал умерших orphan helpers во время ожидания shell worker, и проверка отсутствия children оставалась закрытой. Исправлена только fixture; повтор — PASS. Эта ошибка сохранена в `../../docs/evidence/handoff-protocol-initial/` (точная suite указана внутри отчёта).

## Следующее подключение callers

1. Xray CGI: nonce до spawn, worker принимает invitation до любой Xray mutation, parent вызывает `broray_ops_handoff_to` вместо записи `global/pid`; result/rollback сохраняются. Worker заканчивает manager state по фактическому результату; прежний PID-only release убирается.
2. Server CGI: `broray_servers_api_run` сейчас запускает service function в `( "$@" )`, изолируя `broray_die/exit`. Это тоже отдельный executor. Нельзя просто оставить владельцем CGI и объявить все children учтёнными. Нужен gated service worker, затем перенос действующего error envelope в wrapper.
3. Subscription CGI запускает service functions непосредственно в CGI. Fetch/extract/stage нужно вынести в bounded helpers с передачей счётчиков/error state через private файлы. Перед server sync — protected phase; запись error/status после отмены должна оставаться в своей operation generation.
4. Scheduler и auto-switch: каждый цикл — новая shell до begin; lifetime daemon не становится owner. Quality-check и activation остаются одним job, без nested global admission. Persistent Xray start выполняется вне ptrace helper.
5. Перевести все legacy writers/reclaimers согласованно, затем проверить resumption, boot recovery, installer/rollback, рабочий Xray/VPN и long soak. Этот этап не закрывает эти условия релиза.
