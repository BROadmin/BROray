# ATOMIC-PUBLICATION-001

Исправлено зависание protected fence после завершённой однофайловой записи состояния автовыбора или качества сервера. Coordinator под общим flock сохраняет свидетельство old/new SHA-256, nonce, revision и исходную фазу, выполняет замену и восстанавливает прежнюю фазу. Потерянный ответ можно повторить; устаревший запрос не перезаписывает новое состояние. При восстановлении допускаются только подтверждённое старое или новое содержимое; внешняя protected транзакция остаётся защищённой.

Native guard v6 добавляет `--sync-state`: fsync существующего private regular file и его каталога, либо каталога отсутствующего файла. Две статические сборки каждой архитектуры совпали. ARM64 SHA-256: `a7e2cc93312436a9e00c4610d85f38143b23ef350320a1789b2c28a4c3f761e8`.

## Проверки

- Исходный дефект воспроизведён до изменения production: self-crash настоящего auto-switch producer после native replace оставляет permanent protected fence. Ожидаемый FAIL сохранён.
- Focused Linux: 39 PASS — 19 публикации/recovery и 20 native guard. Матрица включает синтетические identity и отдельный настоящий producer.
- Полный затронутый Linux-прогон: 85 PASS за 1064,78 с. Coordinator 54, servers 13, auto-switch 8, subscriptions 10. Он выполнен на окончательном production-коде.
- Физический ARM64: 54 PASS — publication 10, actual auto jobs 7, actual server jobs 9, coordinator/core 28. Все тестовые каталоги архивированы и удалены.
- Source validation: 236 shell, 36 JSON, 30 JavaScript файлов; baseline archive сохранён.

Первый широкий прогон остановился из-за накопления полных копий приложения в тестовом RAM filesystem. Сохранён отдельно; test cleanup теперь ждёт отсутствия всех дочерних процессов перед удалением только своего каталога. Повторный прогон прошёл полностью.

После focused Linux и producer regressions добавлен fsync каталога после удаления подтверждённого pending-файла. Эта точная дельта проверена повторной физической publication-матрицей, core-матрицей и полным Linux-прогоном. Скрипт фиксации проверяет hashes и допускает только эту явно описанную разницу для ранних свидетельств.

## Граница результата

Подключены только auto-state и server-quality. Полная установка, постоянный Xray/VPN, остальные producers, protected domain recovery, storage/power-loss и длительная совместная работа этим checkpoint не подтверждаются. Служебные временные файлы и прерывание самих metadata writes остаются отдельной проверкой. Релиз не опубликован.

Checkpoint и полные evidence hashes находятся в `checkpoints/BROray-3.1.1-ATOMIC-PUBLICATION-001.json` в корне проекта.
