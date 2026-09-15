# Контроль дочерних процессов на kernel 4.9-ndm-5

Статус: native runner, регистрация в coordinator и клиентский helper реализованы и проверены изолированно. Фоновые службы ещё не переведены на этот протокол. Это следующий этап после LIFECYCLE-API-001.

## Физические проверки 15 сентября

- В `/proc/self/ns` представлен только `mnt`; строк Seccomp/NoNewPrivs в status нет. PID namespace и seccomp не выбраны основой решения на этом роутере.
- Проба ptrace v1 принята ядром, но попытка инъекции SIGKILL через PTRACE_CONT после PTRACE_INTERRUPT не завершила child. Проба остановлена собственным alarm через 5 секунд; этот способ сигнализации отвергнут.
- Проба v2 подтвердила PTRACE_SEIZE, PTRACE_INTERRUPT, TERM собственному неубранному fork-child в ptrace stop, а также фактический SIGKILL tracee при гибели tracer. Сработал PR_SET_CHILD_SUBREAPER.
- `tests/native_ptrace_probe.c`: три проверки PASS. Tracer наблюдал 4 tracee, 2 fork, 1 clone и 3 exec. После принудительной гибели tracer исчезли все потомки, включая thread и процесс в отдельной session. Соседний нетрассируемый sentinel сохранился и завершился только по своему pipe.
- Источники, хеши и console: `../../docs/evidence/ptrace-tree-20260915/`; отдельные ранние пробы сохранены в `ptrace-probe-20260915/` и `ptrace-probe-v2-20260915/`. Временные бинарные файлы удалены.

## Реализованный протокол helper

1. Native supervisor применяется только к явно cooperative helpers: загрузка/разбор подписки, ping, проверка сервера и временный probe Xray. Запуск постоянного Xray и protected commit проходят вне этого tracer. Иначе EXITKILL остановит постоянный сервис при штатном завершении helper.
2. Каждый цикл scheduler получает отдельный краткоживущий shell-owner с begin/ack. Вечный daemon не владеет отдельной операцией. CGI owner также не делает exec после begin. Auto-check и auto-switch выполняются в рамках одного owner; activation переводится в protected phase.
3. Перед запуском helper native регистрирует себя через coordinator: operation ID/token, собственная полная identity, случайный supervisor ID и путь RAM ledger, полученный только из этих ID. После регистрации child остаётся за закрытым pipe gate.
4. Native устанавливает PTRACE_SEIZE с EXITKILL и событиями FORK/VFORK/CLONE/EXEC. Ошибка — отказ запуска. Root child добавляется в RAM ledger до открытия gate. Каждый новый tracee публикуется до первого PTRACE_CONT; обрабатывается также child-stop, пришедший раньше parent fork-event.
5. Ledger — единственный файл, который пишет native; он живёт в RAM, заменяется атомарно и связан с operation ID, supervisor ID, boot ID и birth native-owner. Child entries содержат PID/startTicks/bootId. Во время нормальной жизни native не меняет cmdline/executable.
6. Для каждого fork нельзя вызывать coordinator: трассируемый helper может сам вызвать coordinator, удерживать его guard и ждать свой jq-child. Такой вызов регистратора из tracer привёл бы к самоблокировке. Поэтому coordinator сохраняет supervisor один раз до gate, а RAM ledger имеет одного writer. Recovery читает ledger только после доказанной смерти supervisor; при его жизни освобождение не допускается.
7. На отмену сначала отводится время cooperative code. Затем TERM адресуется только собственному direct fork-child native, до его единственного wait/reap. Это lifetime-контроль ядра, а не PID из файла или повторный /proc-check. Остальным tracee внешние PID-сигналы не отправляются. При истечении grace native завершает себя, а EXITKILL ядра завершает всё трассируемое дерево.
8. Native отслеживает исчезновение shell-owner по birth/boot без внешних сигналов. При его смерти native завершает себя, активируя EXITKILL. Recovery до смерти native или до исчезновения записанных tracee блокируется. Неполный или отсутствующий ledger в том же boot — ambiguous. Новый boot доказывает отсутствие прежних processes.
9. После штатного завершения root helper native также завершает оставшиеся вспомогательные tracee через EXITKILL. Owner проверяет отсутствие детей до следующего protected commit и finish. Probe cleanup больше не должен посылать сигналы по PID из shell-переменной; завершение временного Xray выполняет native.
10. Подтверждённо завершённые supervisor удаляются из persistent registry до удаления RAM ledger. Повтор сбора не дублирует TERM/KILL в пределах сохранённых сегментов журнала; события связаны с одним helper через производный event ID. Реальные consumers ещё не подключены.

## Проверка runner

- Полный Linux regression: 116 PASS (445,53 с), включая 54 проверки операций, 15 guard, 5 actual owner, 7 projection, 3 preflight, 12 CGI и две suite supervisor ниже. Runtime hashes в VM совпадают с checkpoint.
- 12 Linux-проверок самого native runner с искусственным callback регистрации: выполнение/exit status, fork/exec, exec из non-leader thread, 100 thread lifecycles, group-stop, отмена до gate, TERM, timeout, escaped session, гибель tracer и owner.
- 8 Linux-проверок с настоящим coordinator/client и actual `/proc`: запрет commit/finish при живом helper, потеря ledger, живой записанный child после смерти tracer, отмена admission, protected admission, восстановление после смерти owner, журнал и повторный сбор.
- 10 изолированных физических проверок ARM64: те же product binaries, настоящие coordinator/client/Entware jq, filesystem /opt и RAM. Источник и полный namespace сохранены в `../../docs/evidence/physical-supervisor-20260915-02/`; namespace удалён.
- Первая физическая попытка остановилась после семи успешных проверок: в чистом Entware отсутствует команда `setsid`. Примитив перенесён в тестовый C fixture. Неуспешная попытка и отдельная проверка её удаления сохранены в `physical-supervisor-20260915/`.

## Не закрытые этой пробой риски

- Полная матрица промежуточных crash points регистрации/сбора на физическом устройстве; физическая потеря RAM ledger и ENOSPC.
- Стоимость tracing на Go/Xray и массовых shell/jq forks. Ledger пишет RAM, а не flash; всё равно необходима оценка задержек на устройстве.
- Исключение постоянного Xray из tracer, управляемые probe cleanup и защита commit.
- Async Xray handoff ещё не реализован: нужно атомарно менять authority token и полную executor identity, сохраняя immutable generation fence. Старый CGI cleanup не должен завершить переданную работу.
- Полный проверенный producer rollout, legacy blocked-upgrade, VPN и длительный прогон.

Основание: [Linux ptrace manual](https://man7.org/linux/man-pages/man2/ptrace.2.html), [waitpid/неубранные children](https://man7.org/linux/man-pages/man2/waitpid.2.html). Изолированные проверки runner подтверждают описанные сценарии; они ещё не подтверждают работу всех реальных служб и непрерывность VPN.
