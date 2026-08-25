# Подписанная установка BROray через штатный механизм Keenetic

## Что это за вариант

`broray-webcli-bootstrap-3.0.0-r14c68-b1` — отдельный подписанный bootstrap для каталога Keenetic `/opt/install/*.tgz`. Он не является новым кандидатом и не заменяет Stable-канал: после проверки подписи bootstrap устанавливает точные опубликованные байты BROray `3.0.0-r14c68`.

Физическая установка, повторная загрузка, идемпотентность, WebUI и сохранение пользовательских данных прошли проверку на Keenetic aarch64.

Web CLI Keenetic принимает команды KeeneticOS. Это не Linux shell и не средство загрузки файлов. Архив доставляется в `/opt/install/` отдельно — через SFTP или USB.

## Требования

- совместимый Keenetic `aarch64` с Entware `aarch64-3.10`;
- накопитель Entware уже смонтирован как `/opt`;
- доступны BusyBox, `curl`, `jq` и `openssl` из пакета `openssl-util`;
- доступ к SFTP или к накопителю через USB;
- рабочие HTTPS, DNS и системное время.

## Публичные файлы

Базовый каталог:

```text
https://api.brovibe.cloud/releases/bootstrap/broray/3.0.0-r14c68-b1/
```

Скачайте в одну папку:

- [архив bootstrap](https://api.brovibe.cloud/releases/bootstrap/broray/3.0.0-r14c68-b1/broray-webcli-bootstrap-3.0.0-r14c68-b1.tgz);
- [внешнюю подпись](https://api.brovibe.cloud/releases/bootstrap/broray/3.0.0-r14c68-b1/broray-webcli-bootstrap-3.0.0-r14c68-b1.tgz.sig);
- [публичный ключ](https://api.brovibe.cloud/releases/bootstrap/broray/3.0.0-r14c68-b1/broray-bootstrap-rsa3072-v1-public.pem);
- [SHA256SUMS](https://api.brovibe.cloud/releases/bootstrap/broray/3.0.0-r14c68-b1/SHA256SUMS);
- [проверку для Windows](https://api.brovibe.cloud/releases/bootstrap/broray/3.0.0-r14c68-b1/VERIFY-WINDOWS.ps1) или [проверку для Linux](https://api.brovibe.cloud/releases/bootstrap/broray/3.0.0-r14c68-b1/VERIFY-LINUX.sh).

Контрольные значения:

```text
Архив SHA-256: 50904456e998dc501ee534efca5eccc47b997f9986bf5aef3f959d468ad66dd7
Key ID:         aaac0ea3b4a6334c
SPKI SHA-256:   aaac0ea3b4a6334c8be5626aba4a1a10078526c731ce5392fb41ddb66f033b4a
```

Fingerprint публичного ключа нужно сверять с этой страницей через доверенное HTTPS-соединение. Нельзя считать ключ доверенным только потому, что он лежит рядом с архивом.

## Проверка в Windows

Откройте PowerShell в папке с четырьмя файлами и выполните:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\VERIFY-WINDOWS.ps1
```

Ожидаемый итог:

```text
ARCHIVE_SHA256=PASS
PUBLIC_KEY_FINGERPRINT=PASS
DETACHED_SIGNATURE=PASS
SIGNED_BOOTSTRAP=ACCEPTED
```

## Проверка в Linux

```sh
chmod 700 ./VERIFY-LINUX.sh
./VERIFY-LINUX.sh
```

Ожидаемый итог: `SIGNED_BOOTSTRAP=ACCEPTED`.

## Копирование и запуск

1. Скопируйте только проверенный файл `broray-webcli-bootstrap-3.0.0-r14c68-b1.tgz` в `/opt/install/`.
2. Убедитесь, что имя завершённого файла оканчивается на `.tgz`, а не на `.part`.
3. В интерфейсе Keenetic выполните штатную перезагрузку. Если используется Web CLI, на вкладке **Parse** вводится команда KeeneticOS для перезагрузки; команды `sh`, `curl` и `opkg` туда вводить нельзя.
4. При старте менеджер OPKG распакует архив в `/opt`. Bootstrap проверит встроенный публичный ключ, подписанный манифест и точные SHA-256 Stable-объектов, затем запустит установку.

После загрузки WebUI доступен по `http://<LAN-IP-роутера>:8080/` или, при настроенном KeenDNS, по `https://broray.<KeenDNS-домен-роутера>/`.

## Проверка результата

В Entware shell от `root`:

```sh
cat /opt/var/lib/broray-bootstrap/status
/opt/broray/bin/broray-system info
/opt/bin/broray-updaterctl check
```

Успешный первый запуск имеет код `PASS`. После следующей перезагрузки bootstrap не повторяет установку и сообщает `ALREADY_INSTALLED`.

## Ошибка и ручной повтор

Bootstrap работает fail-closed. Если подпись, HTTPS, SHA-256 или установка не прошли проверку, автоматический повтор после точки запуска блокируется. После устранения причины ручной повтор выполняется только из Entware shell:

```sh
/opt/etc/init.d/S99broray-bootstrap retry
```

Не удаляйте marker-файлы вручную и не подменяйте архив под тем же именем. При обращении в поддержку не публикуйте подписки, UUID, пароли, приватные ключи и полные конфигурации.

## Границы безопасности

- приватный ключ подписи не публикуется и не входит в архив;
- отдельная публикация bootstrap не изменяет immutable Stable `3.0.0-r14c68`;
- updater-v5 остаётся единственным каналом дальнейших обновлений BROray;
- успешный повторный запуск идемпотентен;
- серверы, подписки, маршруты, DNS и конфигурация Xray сохраняются по штатному контракту Stable.
