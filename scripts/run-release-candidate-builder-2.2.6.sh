#!/usr/bin/env bash
set -u

BUILDER="/root/BROray-2.2.6-release-candidate-builder-r13.sh"
EXPECTED="3a6925cb2451b86904b68445b5cdcdb18e1326b8db99d3437c035420ada5baab"
READY=yes

printf '%s\n' "=================================================="
printf '%s\n' "BROray 2.2.6 — запуск проверенного universal candidate builder r13"
printf '%s\n' "=================================================="

if [ "$(id -u)" -ne 0 ]; then
    echo "ОШИБКА: требуется root"
    READY=no
fi

if [ ! -f "$BUILDER" ]; then
    echo "ОШИБКА: файл не найден: $BUILDER"
    READY=no
fi

if [ "$READY" = yes ]; then
    ACTUAL="$(sha256sum "$BUILDER" 2>/dev/null | awk '{print $1}')"
    echo "SHA-256: $ACTUAL"
    if [ "$ACTUAL" != "$EXPECTED" ]; then
        echo "ОШИБКА: SHA-256 builder не совпадает"
        READY=no
    elif ! bash -n "$BUILDER"; then
        echo "ОШИБКА: builder не прошёл bash -n"
        READY=no
    fi
fi

if [ "$READY" = yes ]; then
    bash "$BUILDER"
    RESULT=$?
else
    RESULT=1
fi

echo
echo "Код завершения: $RESULT"
echo "Публичный stable-канал не изменяется при сборке кандидата."
echo "Candidate r13 определяет исходную версию динамически, не требует прежний IPK и сохраняет все непакетные пользовательские файлы."
echo "Обязательный транзакционный rollback действует всегда; постоянная пользовательская копия выбирается отдельно: keep или skip."
echo "Keep рекомендован для USB/достаточного накопителя; skip допускается при ограниченной внутренней памяти после preflight."
echo "Продвижение запрещено до staging, физической матрицы исходных версий, forced rollback, keep/skip и двух повторных переустановок."
echo "Терминал остаётся открытым."
echo "=================================================="
exit "$RESULT"
