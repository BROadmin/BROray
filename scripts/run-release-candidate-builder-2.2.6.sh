#!/usr/bin/env bash
set -u

BUILDER="/root/BROray-2.2.6-release-candidate-builder-r15.sh"
EXPECTED="851f9c1f44e97271bf626421bc93b53bb0f06008a90ac23c803f5aaf4251b045"
EXPECTED_SIZE="198955"
READY=yes

printf '%s\n' "=================================================="
printf '%s\n' "BROray 2.2.6 — запуск проверенного universal candidate builder r15"
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
    elif [ "$SIZE" != "$EXPECTED_SIZE" ]; then
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
echo "Candidate r15 определяет исходную версию динамически и не требует прежний IPK."
echo "Перед измерением удаляются только временные транзакции; постоянные архивы не удаляются, access.log ограничивается 16 МиБ."
echo "Место обязательной транзакции и keep рассчитывается отдельно; skip не вызывает оценку постоянной копии."
echo "Размер определяется через wc/readlink без stat, с тремя попытками и точным путём ошибки."
echo "Любой preflight-stop автоматически снимает compatibility overlay и возвращает исходные backend, JavaScript, CGI и wrapper."
echo "Продвижение запрещено до staging, физической матрицы исходных версий, forced rollback, keep/skip и двух повторных переустановок."
echo "Терминал остаётся открытым."
echo "=================================================="
exit "$RESULT"
