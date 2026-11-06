# BROray 3.5.0 — канал релизов и самообновление

Реализация `docs/BRORAY-3.5.0-UPDATER-DESIGN.md` (§3–§7). Код: `internal/update` (манифест, подпись,
загрузчик), `internal/installer/apply.go` + `journal.go` (установщик `installer apply`, журнал,
`ctl recover`), `internal/control` (ping и аутентифицированная остановка через сокет),
`areas/ops/update.go` (WebUI), `tools/release` (инструменты выпуска, в роутерный бинарник не входят).

## 1. Канал

| Что | Значение |
|---|---|
| Имя канала | `update.channel` в `config/config.v1.json`; по умолчанию `stable`. Допустимо `[a-z0-9][a-z0-9-]{0,31}`. |
| Адрес манифеста | встроенный `https://api.brovibe.cloud/releases/broray/channels/<канал>/manifest.json` (`DefaultBaseURL`, допускается переопределение при сборке) или `update.channelUrl` для настроенного канала. |
| Схемы | только `https://` и `file://` (`file:///абсолютный/путь`); `http`, учётные данные в URL, параметры запроса — отказ `CHANNEL_INVALID`. Артефакты `file://` допускаются только из `file://`-манифеста. |
| Подпись | отдельный файл `<manifest>.sig` рядом с манифестом. |

Переопределение адреса не ослабляет защиту: манифест по любому адресу обязан быть подписан ключом,
встроенным в бинарник. Встроенный производственный ключ 3.5 `16FE8A3DA9ED98DC` разрешён только для `stable` и `beta`.
Тестовый ключ и прежний ключ 3.2 не дают доверия производственному каналу 3.5.

Пример `config.v1.json` для тестового канала на роутере:

```json
"update": { "channel": "test", "channelUrl": "file:///opt/broray-test-channel/manifest.json" }
```

## 2. Манифест (`schemaVersion: 1`)

```json
{
  "schemaVersion": 1,
  "channel": "test",
  "issuedAt": "2026-11-03T00:00:00Z",
  "expiresAt": "2026-12-03T00:00:00Z",
  "releases": [
    {
      "version": "3.5.0-dev6",
      "notes": "…",
      "minimumSourceVersion": "3.5.0-dev5",
      "installer": {
        "arm64":            {"url": "broray-3.5.0-dev6-aarch64.tar.gz", "size": 4511234, "sha256": "…", "format": "tar.gz", "entry": "broray-3.5.0-dev6-aarch64/broray", "unpackedSize": 11403264},
        "arm/7":            {"url": "…armv7.tar.gz",  "size": …, "sha256": "…", "format": "tar.gz", "entry": "…/broray", "unpackedSize": …},
        "mipsle/softfloat": {"url": "…mipsel.tar.gz", "size": …, "sha256": "…", "format": "tar.gz", "entry": "…/broray", "unpackedSize": …},
        "mips/softfloat":   {"url": "…mips.tar.gz",   "size": …, "sha256": "…", "format": "tar.gz", "entry": "…/broray", "unpackedSize": …}
      }
    }
  ],
  "revoked": ["3.5.0-dev4"],
  "rollout": {"percent": 100}
}
```

- Ключ артефакта — `GOARCH[/вариант]` бинарника, вариант — `GOARM` или `GOMIPS` из build info Go
  (`arm/7`, `mipsle/softfloat`, `mips/hardfloat`). Роутер вычисляет свой ключ тем же способом из
  собственного бинарника, `tools/release channel` — из бинарника внутри тарбола, поэтому ключ всегда
  совпадает с тем, как была собрана сборка. Запись без варианта (`"mipsle"`) подходит любому варианту.
  Имена тарболов (`aarch64`, `armv7`, `mipsel`, `mips`) на выбор не влияют.
- `format`: `binary` (по умолчанию: артефакт — сам бинарник) или `tar.gz` (из архива извлекается только
  обычный файл `entry` размера `unpackedSize`; остальные члены, ссылки, абсолютные пути и `..` не
  распаковываются вообще).
- `url` — абсолютный или относительный к манифесту.
- `revoked` — отозванные версии: не устанавливаются, на установленной показывается причина
  `CURRENT_VERSION_REVOKED`. `rollout` разбирается и проверяется, но пока **не применяется** (TODO).
- Ограничения: манифест ≤ 256 КиБ, артефакт ≤ 64 МиБ, распакованный бинарник ≤ 128 МиБ.
  Неизвестные поля игнорируются (их защищает подпись), неизвестный `schemaVersion` — отказ.

## 3. Подпись: minisign-совместимый Ed25519

Выбран формат minisign (как в 3.x — меньше нового для владельца). `tools/release sign` записывает
одинаковые `manifest.json.sig` (его скачивает BROray) и `manifest.json.minisig` (имя по умолчанию у
minisign). Поэтому проходит `minisign -Vm manifest.json -p broray-test.pub`; если есть только `.sig`,
укажите его явно: `minisign -Vm manifest.json -p broray-test.pub -x manifest.json.sig`.
Подписывать можно также самим minisign, в том числе зашифрованным ключом. Содержимое подписи:

```
untrusted comment: <текст>
base64("ED" | keyID[8] | Ed25519(BLAKE2b-512(манифест))[64])
trusted comment: <текст>
base64(Ed25519(подпись[64] | trusted comment))
```

Проверяются обе подписи (подделка trusted comment тоже отвергается); принимаются `ED` (по умолчанию в
minisign) и устаревший `Ed` (`minisign -l`). Подписываются точные байты манифеста.
Совместимость формата проверена независимой реализацией на Python (`cryptography` + `hashlib.blake2b`)
и настоящим minisign 0.12: он проверил подпись BROray, а BROray проверил подпись minisign.

**Доверенные ключи** — список `builtinKeys` в `internal/update/keys.go` (публичная строка minisign,
допустимые каналы, признак `Test`). Встроены отдельный production-ключ 3.5 `16FE8A3DA9ED98DC` для `stable`/`beta` и тестовый
`3A5C92992D3BB227` для `test`. Тестовый ключ принимается **только** при настроенном тестовом канале;
для production-каналов он недействителен. Закрытый production-ключ создан по явному решению владельца
на его существующем сервере подписи; в сборке присутствует только публичная часть. Ротация «заявлением, подписанным действующим ключом» (§3.2 проекта)
пока не реализована (TODO в `keys.go`): ротация — выпуск версии, добавляющей новый ключ, пока старый
ещё подписывает, затем удаление старого.

## 4. Отказы проверки

| Код | HTTP (WebUI) | Когда |
|---|---|---|
| `CHANNEL_UNAVAILABLE` | 503 | «Канал обновлений не настроен.» (заглушка адреса) или манифест не загрузился |
| `CHANNEL_INVALID` | 503 | недопустимый адрес или имя канала |
| `UPDATE_SIGNATURE_MISSING` | 503 | нет `<manifest>.sig` |
| `UPDATE_SIGNATURE_INVALID` | 503 | подпись повреждена или не совпала |
| `UPDATE_KEY_UNKNOWN` | 503 | подписано ключом, которому эта сборка не доверяет для этого канала |
| `UPDATE_INDEX_INVALID` | 502 | подписанный манифест не прошёл проверку схемы |
| `UPDATE_CHANNEL_MISMATCH` | 503 | `channel` в манифесте ≠ настроенный канал |
| `UPDATE_CLOCK_UNSET` | 503 | год часов роутера < 2025: срок действия проверить нельзя (ждать NTP) |
| `UPDATE_MANIFEST_FROM_FUTURE` | 503 | `issuedAt` позже часов роутера больше чем на 10 мин |
| `UPDATE_MANIFEST_EXPIRED` | 503 | `expiresAt` прошёл (защита от подсовывания старого манифеста) |
| `UPDATE_ARCH_UNSUPPORTED` | 503 | у новой версии нет сборки для этой архитектуры |
| `UPDATE_SOURCE_TOO_OLD` | 503 | установленная версия ниже `minimumSourceVersion` |
| `UPDATE_VERSION_REVOKED` | — | выбранная версия отозвана |
| `UPDATE_NOT_AVAILABLE` | 409 | нечего ставить (нет более новой версии) |
| `REINSTALL_SOURCE_UNAVAILABLE` | 503 | установленная версия не опубликована в канале |
| `UPDATE_SPACE_REQUIRED` | — | мало места в `/opt` (каталог `tmp` и `versions` суммируются, если они на одной ФС) + 8 МиБ запаса; проверяется **до** скачивания |
| `UPDATE_DOWNLOAD_SIZE_MISMATCH` / `UPDATE_DOWNLOAD_HASH_MISMATCH` / `UPDATE_DOWNLOAD_FAILED` | — | короткое/длинное скачивание, неверный SHA-256, обрыв |

Все тексты для пользователя — на русском (`update.Message`), подробная причина — в stderr/журналах.

## 5. Обновление на роутере

```text
current/broray launcher apply --channel <c> [--version V | --reinstall]   (загрузчик установленной версии)
  ├─ run/launcher.lock (одна операция)
  ├─ манифест + подпись + срок → выбор версии и артефакта → проверка места
  ├─ скачивание в /opt/broray/tmp/update-*/ → размер + SHA-256 (+ извлечение entry)
  └─ <скачанный> installer apply --abi 1 --from <v> --state /opt/broray --channel <c> --progress json
        ├─ run/installer.lock; незавершённый журнал → сначала recover
        ├─ staging: удалить версии кроме текущей и целевой; копия себя → versions/<new>/broray, проверка `version`
        ├─ prepared → stopping: POST /api/v1/daemon/shutdown с токеном state/control.token (0600),
        │     затем ожидание flock run/brorayd.lock (таймаут 60 с). Без ответа/без освобождения — отказ,
        │     ничего не изменено. Остановка по PID не выполняется никогда.
        ├─ switching: S24broray обновляется (старый сохранён), current → versions/<new> (symlink + rename + fsync)
        ├─ started: S24broray start; готовность — ping сокета с версией <new> (таймаут 90 с)
        └─ committed (переустановка: удалить broray.old) | rolling-back → rolled-back
              (current назад, старый S24broray, удаление versions/<new>, запуск старого демона)
```

- Прогресс — JSON-строки в stdout (`{"event":"stage|log|result",…}`), диагностика — stderr. Последняя
  строка — `result` загрузчика; выход без неё или ненулевой код — не успех.
- Переустановка той же версии: новый бинарник кладётся в `versions/<v>/broray.new`, на шаге switching —
  `broray → broray.old`, `broray.new → broray`; откат — обратно. Предыдущая версия сохраняется.
- Хранится не более двух версий (текущая и предыдущая).
- Сбой питания: `S24broray start` сначала вызывает `broray ctl recover`: `prepared/stopping` — убрать
  подготовленную копию (текущая версия не менялась); `switching/rolling-back` — откат; `started` —
  коммит, если демон уже отвечает новой версией (только при `--restart`, т. е. из WebUI), иначе откат.
  Пока установщик держит `installer.lock`, recover ничего не трогает.
- Перезагрузка роутера не запрашивается ни в одном исходе.
- Таймауты для диагностики: `BRORAY_UPDATE_STOP_TIMEOUT`, `BRORAY_UPDATE_READY_TIMEOUT` (секунды).

Команды: `broray launcher check [--channel C]` (только проверка, JSON), `broray launcher apply …`,
`broray installer apply …` (ABI v1; обычно запускает загрузчик), `broray ctl recover [--restart]`.

## 6. WebUI («О BROray»)

- «Проверить обновления» → `update-check.cgi`: проверка в процессе демона, кэш
  `state/update-check.json`; из свежего (≤ 1 ч, та же версия и канал) кэша `info.cgi` берёт
  `updateAvailable`, `availableVersion`, `lastCheckedAt`, `reinstallSupported`, `updateChannel`.
- «Установить обновление» / «Переустановить» → 202, загрузчик запускается отдельным сеансом
  (переживает остановку демона); его JSON-строки пишутся в `state/update/<id>.jsonl` и отражаются в
  «Последней операции» (`update-status.cgi`). Демон, поднявшийся после переключения (новый или старый
  после отката), продолжает чтение с сохранённого смещения и фиксирует итог. Пока страница
  недоступна (несколько секунд), JS показывает своё штатное «Ожидание WebUI…».
- `restore.cgi`: если журнал показывает прерванную операцию — `ctl recover --restart` (202, операция
  `restore`); иначе прежний 409 `RESTORE_UNAVAILABLE`. Кнопка на странице, как в 3.x, отключена.

## 7. Процедура производственного ключа (владелец)

1. На офлайн-машине (не на роутере, не в репозитории):
   `go run ./tools/release keygen -o <офлайн-каталог> -name broray-stable` — создаёт
   `broray-stable.key` (0600, minisign, **без пароля**) и `broray-stable.pub`, печатает публичный ключ и
   key id. Вариант с паролем: `minisign -G` (тогда подписывать самим minisign).
2. Добавить в `builtinKeys` (`internal/update/keys.go`) запись `{Name, PublicKey: "<строка из .pub>",
   Channels: []string{"stable", "beta"}}` (без `Test`) и выпустить версию с этим ключом.
3. Для каждого выпуска: собрать тарболы 4 архитектур, затем
   `go run ./tools/release channel -channel stable -version <v> -expires 720h -merge <прошлый manifest.json> -o manifest.json broray-<v>-*.tar.gz`
   (`-merge` сохраняет прежние релизы — нужны для «Переустановить» — и отзывы; `-revoke <v>` отзывает,
   `-min-source`, `-notes`, `-base-url` — по необходимости).
4. Подписать офлайн: `go run ./tools/release sign -k broray-stable.key manifest.json` (или
   `minisign -Sm manifest.json -s … -x manifest.json.sig` и скопировать те же байты в
   `manifest.json.minisig`). Проверить `go run ./tools/release verify -p broray-stable.pub manifest.json`
   и `minisign -Vm manifest.json -p broray-stable.pub`.
5. Опубликовать `manifest.json`, `manifest.json.sig`, `manifest.json.minisig` и тарболы. Переподписывать манифест до истечения
   `expiresAt` (иначе роутеры откажутся: `UPDATE_MANIFEST_EXPIRED`).

Ключ подписи манифестов не используется ни для чего другого (поддержка и прочие функции — свои ключи).

## 8. Тестовый канал (Codex, KN-1811)

Закрытая часть тестового ключа лежит вне репозитория (передаётся вместе с заданием). На машине сборки:

```sh
go run ./tools/release channel -channel test -version 3.5.0-dev6 -expires 168h -o manifest.json broray-3.5.0-dev6-*.tar.gz
go run ./tools/release sign -k broray-test.key manifest.json
```

Загрузить `manifest.json`, `manifest.json.sig`, `manifest.json.minisig` и тарбол aarch64 в один каталог на накопителе роутера,
в `config.v1.json` указать `"update": {"channel": "test", "channelUrl": "file:///<каталог>/manifest.json"}`
и перезапустить демон (`/opt/etc/init.d/S24broray restart`) или сразу запустить
`broray launcher check` / `broray launcher apply`. Нужна установленная через `installer install`
версия 3.5.0, в которой уже есть этот механизм (dev5 и ниже его не содержат: первое обновление на
версию с механизмом — терминальной командой `installer install`).
