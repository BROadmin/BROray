#!/usr/bin/env bash
set -u

BUILDER="/root/BROray-2.2.6-release-candidate-builder-r11.sh"
EXPECTED="851cf1bd62e67fb05dcbf2350302405947737d498a801b8ced7c9cf4e7dae67a"
READY=yes

printf '%s\n' "=================================================="
printf '%s\n' "BROray 2.2.6 — запуск universal candidate builder r11"
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
echo "Candidate r11 определяет исходную версию динамически, не требует прежний IPK и выполняет локальный rollback дерева, OPKG metadata, внешних файлов и служб."
echo "Продвижение запрещено до физической матрицы 2.1.1-2, 2.2.0-2, 2.2.4, 2.2.5, принудительного rollback и повторного обновления."
echo "Терминал остаётся открытым."
echo "=================================================="
exit "$RESULT"
