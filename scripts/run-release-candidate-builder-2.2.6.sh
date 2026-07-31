#!/usr/bin/env bash
set -u

BUILDER="/root/BROray-2.2.6-release-candidate-builder-r10.sh"
EXPECTED="30b68b51e44faf536109c4eae23fae5a730cf923ec4d4740aa5eab0b5d57d5ff"
READY=yes

printf '%s\n' "=================================================="
printf '%s\n' "BROray 2.2.6 — запуск проверенного candidate builder r10"
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
echo "Candidate r10 использует потоковую low-RAM переустановку, старое дерево для отката, прямой снимок в /opt и требует двух физических переустановок через WebUI до продвижения."
echo "Терминал остаётся открытым."
echo "=================================================="
exit "$RESULT"
