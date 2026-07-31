#!/usr/bin/env bash
set -u

BUILDER="/root/BROray-2.2.6-release-candidate-builder-r12.sh"
EXPECTED="203b9e52b37f18f81d76514bbfafa6be8e4b8cef4e012acbad75fe6a8d7ab5bc"
READY=yes

printf '%s\n' "=================================================="
printf '%s\n' "BROray 2.2.6 — запуск universal candidate builder r12"
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
echo "Candidate r12 определяет исходную версию динамически, не требует прежний IPK, сохраняет все непакетные пользовательские файлы и выполняет локальный rollback дерева, OPKG metadata, внешних файлов и служб."
echo "Публикатор r12 нормализует каталоги staging в 0755 и файлы в 0644."
echo "Продвижение запрещено до физической матрицы, принудительного rollback, проверки backup/logs/quality/custom и двух повторных переустановок."
echo "Терминал остаётся открытым."
echo "=================================================="
exit "$RESULT"
