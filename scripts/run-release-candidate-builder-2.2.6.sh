#!/usr/bin/env bash
set -u

BUILDER="/root/BROray-2.2.6-release-candidate-builder-r9.sh"
EXPECTED="eb73a5acd050570b72f1da234eea199e9fa1501a1cc27f6789347550ef0af82e"
READY=yes

printf '%s\n' "=================================================="
printf '%s\n' "BROray 2.2.6 — запуск проверенного candidate builder r9"
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
echo "Candidate r9 требует проверки singleton S28, пустого error при успехе и двух переустановок через WebUI до продвижения."
echo "Терминал остаётся открытым."
echo "=================================================="
exit "$RESULT"
