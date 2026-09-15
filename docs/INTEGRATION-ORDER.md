# Порядок подключения прототипа к BROray

Прототип не включать отдельным environment switch на рабочем роутере. Сначала завершить обязательные lifecycle-протоколы, затем переводить все producers одной согласованной сборкой.

## 1. Launch и child ownership

Каждый job получает отдельного краткоживущего owner; PID вечного scheduler не должен быть владельцем отдельного цикла. Scheduler создаёт launch nonce до запроса coordinator. `begin` с этим nonce идемпотентен для той же полной identity. В ответе терять authority нельзя: повтор того же запроса возвращает ту же операцию, а не создаёт вторую. После записи ответа клиентом выполняется `ack`; до ack ребёнок не получает разрешение на работу. Потеря ack-response разрешается повторным ack по token.

Зарегистрировать child до открытия его start gate. Child без подтверждённой регистрации должен завершаться, не начиная сетевую загрузку/commit. Владелец хранит child identity и фазу; recovery не считает список завершённым только потому, что parent умер. При отсутствии или повреждении сведений fail closed. `starting` без опубликованного fence сканируется отдельно; живой unacknowledged launch может быть принят только тем же owner/nonce.

Для TERM/KILL изучить supervised session: native parent удерживает своего ребёнка unreaped, child становится session/process-group leader до запуска разрешённых helpers. Это проект механизма, не доказательство безопасности. Проверить descendants, смену session, смерть supervisor, group reuse и основной Xray; не заменять доказательство повторным `/proc` чтением перед `kill(PID)`.

## 2. Подключение callers

| Файл/узел | Изменение | Обязательный тест |
| --- | --- | --- |
| `operation-manager.sh` | source client; общий namespace/history; не менять updater pointer | одновременная старая transaction history и новая background history |
| `routes-api-operation.sh` | общий begin/finish/recover, token-based release; старый read-only статус заменить public projection | late cleanup старой generation не трогает новую |
| `broray-subscription-scheduler` | job owner на цикл, source SUBSCRIPTION_AUTO, pause admission, idempotent cleanup | lost begin/ack response; daemon остаётся жив |
| `broray-server-auto-switch` | один parent-job: quality-check, затем protected activation; источник/фаза внутри job | никакой nested self-deadlock; ошибка проверки не обходит lock |
| `subscription-service.sh` | cancel перед download, внутри loops, перед server sync; commit защищён | неизменность данных при отмене на каждой границе |
| `server-service.sh` и probe helper | bounded check/ping; отдельный управляемый probe Xray; registry children | при отмене остаётся главный Xray и рабочий VPN |
| `xray-web-operation.sh` | атомарный handoff registry identity/token worker; специальный protected mode | crash до/после handoff; сохранение существующего rollback |
| `routes-web-action.sh` | связать существующий resumable stop с operation state | PID отсутствует, но logical reservation сохраняется |
| `broray-runtime-prepare`/service startup | controlled recovery scan до новой автоматики | reboot interrupted phases; не стирать domain pending |
| `broray-log-maintenance` | общий лимит без copytruncate нового structured journal | prune никогда не удаляет active/ambiguous owner/history |
| новые authenticated CGI | контракт из OPERATIONS-API-CONTRACT | auth, CSRF, GET safety, request size, traversal |
| `broray.html`/`broray.js` | перенести проверенные блоки макета, подключить реальные snapshots | 4 ширины × обе темы × все состояния на полной странице |

## 3. Storage и диагностика

Heartbeat хранить в RAM с монотонным временем, boot identity и generation; отсутствие heartbeat не является доказательством смерти. На flash писать start/phase-change/terminal/cancel, а не каждый poll. Журнал: единый сериализованный writer, лимит трёх сегментов, предел 500 событий в отчёте, потерянные события/частичные строки отмечать флагом неполноты. Сбой записи журнала не должен блокировать подтверждённое освобождение ресурса.

Терминальная запись + оставшийся fence после сбоя retirement сохраняет исходный результат и только завершает retirement. Новый claim не удаляет чужое имя, даже если оно пустое. При publication fsync error job не допускается к работе. Persistent guard-файл никогда не удаляется и не заменяется при cleanup/rotation.

## 4. Legacy и доставка

Доказанное отсутствие числового PID недостаточно для миграции старой операции: могут остаться child и domain commit. Требуется отдельный inspected quiescence/recovery protocol с сохранением Xray. Старый updater отклоняет запрос до загрузки нового приложения; в 3.1.1-коде нельзя «исправить» уже не допускающую обновление 3.1.0. Нужен поддерживаемый подписанный путь recovery/installation из доступного UI, подтверждённый на точной базе. Это самостоятельная обязательная часть, не «после релиза».

Финальная интеграция считается завершённой только после проверки всей матрицы, а не только замены пятифайлового writer на новую функцию.
