# Установка BROray 3.5.0 — по шагам


Выберите свою модель, скопируйте готовую команду и откройте знакомый WebUI BROray. Вручную скачивать архивы и подставлять контрольные суммы не требуется.


> **Если у вас BROray 3.2 или более ранняя версия** Для перехода на 3.5.0 нужна чистая установка. Сначала сохраните ссылки серверов и подписок и выгрузите свои наборы маршрутов. Обновитель 3.2 и её резервная копия не предназначены для перехода на 3.5. Не удаляйте Entware вручную.


<a id="from-32"></a>
## Подготовка к переходу с 3.2


1. Если BROray ещё не установлен, пропустите этот раздел и начните с шага 1.
2. Если установлен BROray 3.2, сохраните отдельно ссылки серверов, URL подписок и файлы своих маршрутов. Копию 3.2 можно сохранить как архив для старой версии, но импортировать её в 3.5 нельзя.
3. Из домашней сети откройте старый WebUI → «BROray» → раздел удаления. После сохранения нужных данных выберите **полное удаление старого BROray**, прочитайте список удаляемых данных и подтвердите. Режим сохранения данных 3.2 для этого перехода не используйте.
4. Дождитесь успешного завершения. Старый WebUI перестанет открываться — это ожидаемо. Entware и саму прошивку Keenetic оставьте установленными. Затем переходите к шагу 1 ниже.


Подробные шаги удаления относятся именно к старой версии: [архивная инструкция 3.2, раздел «Удаление BROray»](https://github.com/BROadmin/BROray/tree/v3.2.0-r12/docs/beginner-installation.md#6-удаление-broray). Если старая версия не удаляется штатно, сначала разберите её ошибку; вручную стирать каталоги ради продолжения не нужно.


<a id="prepare"></a>
## 1. Подготовьте роутер


1. Подключите компьютер к домашней сети этого роутера по Wi-Fi или кабелю. Убедитесь, что интернет работает. Установку выполняйте из локальной сети.
2. Найдите обозначение **KN-xxxx** на наклейке снизу роутера или в его системном мониторе. Номер важнее названия: у разных поколений одного названия бывает разная архитектура.
3. На роутере должен быть установлен **Entware**, а в `/opt` — свободно не менее **60 МиБ**. Встроенного накопителя некоторых моделей недостаточно: в таком случае Entware размещают на подходящем USB-накопителе.
4. Если Entware ещё нет, сначала установите его по [инструкции производителя](https://support.keenetic.com/hero/kn-1012/en/20980-installing-the-entware-repository-on-a-usb-drive.html). Эта инструкция также объясняет подключение к Entware. Затем вернитесь сюда. Если Entware уже работает, повторно устанавливать его не нужно.


Go на роутере не нужен. Не используйте установочные команды и bootstrap-файлы ветки 3.2 для установки 3.5.


<a id="ssh"></a>
## 2. Откройте терминал Entware


1. Откройте Termius или PuTTY на компьютере и создайте SSH-подключение к локальному IP-адресу роутера. Это тот же адрес, по которому открывается его панель: например, `192.168.1.1`.
2. Укажите имя пользователя **root** и пароль Entware. Это подключение к Entware, а не вход администратором в панель Keenetic.
3. Обычно Entware использует порт **222**, если порт 22 занят встроенным SSH Keenetic. Если встроенный SSH-компонент не установлен, Entware может использовать **22**. Используйте порт, указанный при настройке Entware.
4. После входа должна появиться строка shell, заканчивающаяся символом `#`. Если вы видите `(config)>`, открыт командный интерфейс Keenetic: команды ниже туда вставлять не нужно. Переподключитесь к SSH Entware.


> **Как вставлять команды** Копируйте блок целиком кнопкой «Скопировать», вставляйте в терминал и нажимайте Enter. При вводе пароля символы обычно не отображаются — это нормально.


<a id="download-tools"></a>
## 3. Подготовьте загрузку по HTTPS


Один раз выполните этот блок. Он добавляет curl и сертификаты для скачивания установочного архива. Если пакеты уже установлены, opkg сообщит об этом.


```sh
opkg update && opkg install ca-bundle ca-certificates curl
```


Дождитесь возврата строки с `#`. Если opkg завершился ошибкой или не найден, сначала исправьте установку Entware либо подключение к нему; к следующему шагу пока не переходите.


<a id="models"></a>
## 4. Найдите свою модель и выполните её команду


Скопируйте **один** блок под группой со своим номером KN. Ничего внутри менять не нужно: команда сама скачивает нужный архив, сверяет его SHA-256 и запускает предварительную проверку и установку. При несовпадении архитектуры или контрольной суммы установка остановится.


| Модель на наклейке | Ниже используйте |
| --- | --- |
| KN-1012, KN-1811, KN-1812, KN-2312, KN-2710, KN-3811, KN-3812, KN-4110 | [Команду для первой группы — aarch64](#install-aarch64) |
| KN-1011, KN-1810, KN-1910, KN-1912, KN-2310, KN-2311, KN-2910, KN-3810 | [Команду для второй группы — mipsel](#install-mipsel) |
| KN-2112, KN-2410, KN-3610 | [Команду для DSL-группы — mips](#install-mips) |


<a id="install-aarch64"></a>
### Первая группа: KN-1012, KN-1811, KN-1812, KN-2312, KN-2710, KN-3811, KN-3812, KN-4110


Готовая команда для aarch64. Скопируйте весь блок:


```sh
(
set -eu
PATH=/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin
export PATH
entware_arch="$(awk '$1 == "arch" && $2 != "all" {print $2; exit}' /opt/etc/opkg.conf)"
case "$entware_arch" in
  aarch64-*) ;;
  *) echo "Выбран другой тип Entware: $entware_arch. Установка остановлена."; exit 1 ;;
esac
workdir="$(mktemp -d /tmp/broray-install.XXXXXX)"
cleanup() {
  rm -f "$workdir/broray.tar.gz" "$workdir/broray-3.5.0-aarch64/broray" "$workdir/broray-3.5.0-aarch64/README.txt"
  rmdir "$workdir/broray-3.5.0-aarch64" "$workdir" 2>/dev/null || true
}
trap cleanup EXIT
curl --proto '=https' --proto-redir '=https' -fL --connect-timeout 20 --max-time 600 \
  'https://github.com/BROadmin/BROray/releases/download/v3.5.0/broray-3.5.0-aarch64.tar.gz' -o "$workdir/broray.tar.gz"
printf '%s  %s\n' 'af28bbec9bb8199280a609b82502d201ce1398be96d78851377acd5c7fa6014c' "$workdir/broray.tar.gz" | sha256sum -c -
tar -xzf "$workdir/broray.tar.gz" -C "$workdir"
"$workdir/broray-3.5.0-aarch64/broray" installer preflight
"$workdir/broray-3.5.0-aarch64/broray" installer install
"$workdir/broray-3.5.0-aarch64/broray" ctl ping
echo "BROray установлен. Теперь откройте WebUI по адресу роутера на порту 8080."
)
```


<a id="install-mipsel"></a>
### Вторая группа: KN-1011, KN-1810, KN-1910, KN-1912, KN-2310, KN-2311, KN-2910, KN-3810


Готовая команда для mipsel. Скопируйте весь блок:


```sh
(
set -eu
PATH=/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin
export PATH
entware_arch="$(awk '$1 == "arch" && $2 != "all" {print $2; exit}' /opt/etc/opkg.conf)"
case "$entware_arch" in
  mipsel-*) ;;
  *) echo "Выбран другой тип Entware: $entware_arch. Установка остановлена."; exit 1 ;;
esac
workdir="$(mktemp -d /tmp/broray-install.XXXXXX)"
cleanup() {
  rm -f "$workdir/broray.tar.gz" "$workdir/broray-3.5.0-mipsel/broray" "$workdir/broray-3.5.0-mipsel/README.txt"
  rmdir "$workdir/broray-3.5.0-mipsel" "$workdir" 2>/dev/null || true
}
trap cleanup EXIT
curl --proto '=https' --proto-redir '=https' -fL --connect-timeout 20 --max-time 600 \
  'https://github.com/BROadmin/BROray/releases/download/v3.5.0/broray-3.5.0-mipsel.tar.gz' -o "$workdir/broray.tar.gz"
printf '%s  %s\n' 'ac84cdea67b4be27b0e77a927009b0350e472cce891d10d83150285aded663af' "$workdir/broray.tar.gz" | sha256sum -c -
tar -xzf "$workdir/broray.tar.gz" -C "$workdir"
"$workdir/broray-3.5.0-mipsel/broray" installer preflight
"$workdir/broray-3.5.0-mipsel/broray" installer install
"$workdir/broray-3.5.0-mipsel/broray" ctl ping
echo "BROray установлен. Теперь откройте WebUI по адресу роутера на порту 8080."
)
```


<a id="install-mips"></a>
### DSL-группа: KN-2112, KN-2410, KN-3610


Готовая команда для mips. Скопируйте весь блок:


```sh
(
set -eu
PATH=/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin
export PATH
entware_arch="$(awk '$1 == "arch" && $2 != "all" {print $2; exit}' /opt/etc/opkg.conf)"
case "$entware_arch" in
  mips-*) ;;
  *) echo "Выбран другой тип Entware: $entware_arch. Установка остановлена."; exit 1 ;;
esac
workdir="$(mktemp -d /tmp/broray-install.XXXXXX)"
cleanup() {
  rm -f "$workdir/broray.tar.gz" "$workdir/broray-3.5.0-mips/broray" "$workdir/broray-3.5.0-mips/README.txt"
  rmdir "$workdir/broray-3.5.0-mips" "$workdir" 2>/dev/null || true
}
trap cleanup EXIT
curl --proto '=https' --proto-redir '=https' -fL --connect-timeout 20 --max-time 600 \
  'https://github.com/BROadmin/BROray/releases/download/v3.5.0/broray-3.5.0-mips.tar.gz' -o "$workdir/broray.tar.gz"
printf '%s  %s\n' 'f2d73a292509022e071924d81135a743574bd8858bc0dcd7fc39a76fd497b30d' "$workdir/broray.tar.gz" | sha256sum -c -
tar -xzf "$workdir/broray.tar.gz" -C "$workdir"
"$workdir/broray-3.5.0-mips/broray" installer preflight
"$workdir/broray-3.5.0-mips/broray" installer install
"$workdir/broray-3.5.0-mips/broray" ctl ping
echo "BROray установлен. Теперь откройте WebUI по адресу роутера на порту 8080."
)
```


Соответствие моделей архитектурам приведено по [данным Keenetic](https://support.keenetic.com/hero/kn-1011/en/69806-installation-of-the-asterisk-ip-pbx-opkg-package.html). Это выбор правильного архива, а не заявление об аппаратных испытаниях BROray на каждой модели. 3.5.0 физически проверена на **KN-1811**; остальные архитектуры прошли эмулятор. Требование 60 МиБ свободного места действует для всех групп.


<a id="other-models"></a>
### Если вашей модели нет в списке


Не выбирайте соседний номер или похожее название наугад. Выполните одну команду и посмотрите строку `arch` с числом в конце:


```sh
opkg print-architecture
```


`aarch64-…` соответствует первой группе; `mipsel-…` — второй; `mips-…` — DSL-группе. Если вывод содержит `armv7-…`, используйте дополнительный блок ниже. Если определить результат не удалось, пришлите в поддержку только модель и этот вывод; пароли и ссылки подписок не нужны.


<a id="install-armv7"></a>
### Дополнительная команда: только для Entware armv7


Используйте этот блок только при подтверждённом `armv7-…`. Список моделей для этой группы не подтверждён; аппаратные испытания не заявляются.


```sh
(
set -eu
PATH=/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin
export PATH
entware_arch="$(awk '$1 == "arch" && $2 != "all" {print $2; exit}' /opt/etc/opkg.conf)"
case "$entware_arch" in
  armv7-*) ;;
  *) echo "Выбран другой тип Entware: $entware_arch. Установка остановлена."; exit 1 ;;
esac
workdir="$(mktemp -d /tmp/broray-install.XXXXXX)"
cleanup() {
  rm -f "$workdir/broray.tar.gz" "$workdir/broray-3.5.0-armv7/broray" "$workdir/broray-3.5.0-armv7/README.txt"
  rmdir "$workdir/broray-3.5.0-armv7" "$workdir" 2>/dev/null || true
}
trap cleanup EXIT
curl --proto '=https' --proto-redir '=https' -fL --connect-timeout 20 --max-time 600 \
  'https://github.com/BROadmin/BROray/releases/download/v3.5.0/broray-3.5.0-armv7.tar.gz' -o "$workdir/broray.tar.gz"
printf '%s  %s\n' '2247faa66549b9d19f08fdacdc87ed921a53da87dff8dd6beef31bbe55144c48' "$workdir/broray.tar.gz" | sha256sum -c -
tar -xzf "$workdir/broray.tar.gz" -C "$workdir"
"$workdir/broray-3.5.0-armv7/broray" installer preflight
"$workdir/broray-3.5.0-armv7/broray" installer install
"$workdir/broray-3.5.0-armv7/broray" ctl ping
echo "BROray установлен. Теперь откройте WebUI по адресу роутера на порту 8080."
)
```


<a id="result"></a>
## 5. Дождитесь результата


Не закрывайте терминал, пока выполняется установка. При успехе последняя строка сообщит, что BROray установлен, а проверка демона вернёт ответ без ошибки. Команда установки не перезагружает роутер. Xray скачивается при первом запуске; дождитесь окончания загрузки по статусу в WebUI.


| Что вы видите | Что делать |
| --- | --- |
| `INSTALL_SPACE_REQUIRED` | Освободить место на накопителе Entware до 60 МиБ или использовать подходящий накопитель. Не обходить проверку. |
| `INSTALL_LEGACY_PRESENT` | Старая установка BROray ещё обнаруживается. Завершить её штатное удаление по инструкции той версии. |
| «Выбран другой тип Entware» | Выбрана чужая группа команд. Сверить номер KN и результат `opkg print-architecture`. |
| Ошибка curl / сертификата / SHA-256 | Не продолжать вручную. Проверить интернет, время и сертификаты. Сохранить текст ошибки для поддержки. |
| `opkg: not found` или `(config)>` | Проверить, что подключились к Entware, а не к CLI Keenetic. |


<a id="open-webui"></a>
## 6. Войдите в BROray


1. Откройте в браузере **http://IP-адрес-роутера:8080/**. Например, если панель роутера открывается на `192.168.1.1`, адрес BROray будет **http://192.168.1.1:8080/**.
2. Введите имя и пароль **администратора Keenetic**. Здесь используются данные панели роутера, а не пользователь root и пароль Entware.
3. Откройте «Серверы», импортируйте свою ссылку и активируйте подключение. Либо сначала добавьте ссылку в «Подписках», затем выберите один из её серверов.
4. Нажмите «Проверить» у активного сервера. Затем установите нужные наборы на странице «Маршруты → Готовые маршруты». Одного добавления сервера недостаточно для выбора трафика устройств — его определяют маршруты и политики Keenetic.


После перезапуска или обновления BROray потребуется повторный вход. Доступ через KeenDNS при необходимости включается отдельно на странице «Keenetic»; сначала убедитесь, что локальный адрес работает.


<a id="manual-verification"></a>
## Если хотите скачать файлы вручную


Все архивы, манифест, подпись и открытый ключ доступны в [релизе 3.5.0](https://github.com/BROadmin/BROray/releases/tag/v3.5.0). В готовых командах выше уже закреплены SHA-256 из подписанного манифеста. Дополнительную проверку подписи можно выполнить на ПК с minisign:


```sh
minisign -Vm manifest.json -P RWTcmO2pPYr+FgTRMQHzWjGQeJcTF/iJZQSIQF353UWJUfYEOjaMJgX5 -x manifest.json.sig
```


Закрытый ключ для установки не нужен. Подпись относится к манифесту с хешами архивов. Не распаковывайте и не запускайте архив, чей хеш не совпал.

