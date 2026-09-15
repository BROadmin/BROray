# MONITOR-LIFECYCLE-001

S23 и connection-monitor используют общий service lifecycle: полную identity, уникальную generation, постоянный kernel lifetime lease и cooperative stop. Неизвестный старый PID-файл сохраняется; stop/restart возвращают ошибку, пока принадлежность не подтверждена. Контроллер не отправляет сигналы по PID-файлу. Во время длинного foreground вызова stop возвращает pending; успешная остановка требует завершения daemon и освобождения lease.

До изменения production физически воспроизведён старый дефект удаления неподтверждённого PID-файла. Исправление прошло 19 Linux-проверок за 232,59 с: 8 настоящего monitor и 11 регрессии общего service lifecycle. Отдельный процесс-canary остаётся жив после stop. На физическом ARM64 прошли 6 сценариев, включая настоящий init S23, конкурентные старты, старую generation stop, задержанный цикл и self-crash daemon.

Физический evidence archive: SHA-256 `7c42ce0df9f08aa3601220c161af15a1bced3ef775fc76bac72d51cfe41baea7`, 916 226 байт. Изолированные каталоги архивированы и удалены. Полное приложение и постоянный Xray не запускались.

Проверка сохранности connection-status и текстового журнала при storage failure остаётся отдельным этапом. При зависшем внешнем вызове службы controller сообщает pending; этот checkpoint не доказывает ограниченное принудительное завершение каждого такого вызова.

Полные hashes и результаты: `checkpoints/BROray-3.1.1-MONITOR-LIFECYCLE-001.json` в корне проекта.
