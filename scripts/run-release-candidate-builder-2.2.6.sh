#!/usr/bin/env bash
set -u

BUILDER="/root/BROray-2.2.6-release-candidate-builder-r3.sh"
EXPECTED="226e5bd894c1eade64cc5507dc12b4a5c0763f4fa243a6dc205961a1cfe5ca9f"
READY=yes

printf '%s\n' "=================================================="
printf '%s\n' "BROray 2.2.6 — запуск проверенного candidate builder r3"
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
echo "Публичный стабильный канал не должен изменяться на этапе сборки кандидата."
echo "Терминал остаётся открытым."
echo "=================================================="
exit "$RESULT"
