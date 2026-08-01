#!/usr/bin/env bash
set -u

BUILDER="/root/BROray-2.2.6-release-candidate-builder-r14.sh"
EXPECTED="295e074bedd22465686c2031915ee2cdf044a850bf6d931a9cf9bea708dcef19"
READY=yes

printf '%s\n' "=================================================="
printf '%s\n' "BROray 2.2.6 — запуск проверенного universal candidate builder r14"
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
    SIZE="$(stat -c '%s' "$BUILDER" 2>/dev/null)"
    echo "SHA-256: $ACTUAL"
    echo "Размер:  $SIZE"
    if [ "$ACTUAL" != "$EXPECTED" ]; then
        echo "ОШИБКА: SHA-256 builder не совпадает"
        READY=no
    elif [ "$SIZE" != "173401" ]; then
        echo "ОШИБКА: размер builder не совпадает"
        READY=no
    elif ! bash -n "$BUILDER"; then
        echo "ОШИБКА: builder не прошёл bash -n"
        READY=no
    fi
fi

if [ "$READY" = yes ]; then
    unset BRORAY_BUILDER_TEST_MODE \
        BRORAY_BUILDER_TEST_SOURCE_IPK \
        BRORAY_BUILDER_TEST_SOURCE_SHA \
        BRORAY_BUILDER_TEST_SOURCE_SIZE \
        BRORAY_BUILDER_PUBLIC_ROOT \
        BRORAY_BUILDER_BUILD_ROOT \
        BRORAY_BUILDER_OUT \
        BRORAY_BUILDER_ARCHIVE
    bash "$BUILDER"
    RESULT=$?
else
    RESULT=1
fi

echo
echo "Код завершения: $RESULT"
echo "Публичный stable-канал не изменяется при сборке кандидата."
echo "Candidate r14 определяет исходную версию динамически и не требует прежний IPK."
echo "Обязательный транзакционный rollback действует всегда; постоянная копия выбирается отдельно: keep или skip."
echo "Exact preinst-модель использует явно проверенный tar и не зависит от постороннего /opt/bin/tar release-сервера."
echo "Продвижение запрещено до staging, физической матрицы исходных версий, forced rollback, keep/skip и двух повторных переустановок."
echo "Терминал остаётся открытым."
echo "=================================================="
exit "$RESULT"
