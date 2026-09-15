# Точный фазовый расчёт места BROray

Версия контракта: **`broray-space/2`**  
Requirements: **`1.7.2`**  
Единица всех вычислений: **KiB = 1024 bytes**

Этот контракт заменяет фиксированные бюджеты r12 и старый расчёт «полный target
поверх source». Compression ratio никогда не является входом gate.

## Измерения

```text
ceil_kib(bytes) = (bytes + 1023) / 1024
ceil_mib(kib)   = ((kib + 1023) / 1024) * 1024
max0(x...)      = max(0, x...)
```

`LC_ALL=C df -Pk PATH` предоставляет `Available` в 1024-blocks. Regular-file
bytes измеряются `wc -c`; allocated tree/inode data строятся из
функционально доказанного `find -P -xdev -printf`. Source allocation manifest:

```text
type | device | inode | nlink | blocks512 | logical_bytes | mode | uid | gid | path | symlink_target
```

Внутренние regular-file и symlink hardlink groups поддерживаются и считаются
ровно один раз по `(device,inode)`, только когда factual source/delete scope
содержит все `nlink` путей группы. Любая ссылка этого inode вне factual scope
делает ownership и delete topology структурно неоднозначными: операция
отклоняется до mutation с
`source-external-hardlink-topology-unsupported`. Такая группа не является
«поддерживаемой, но нерекламируемой». На PASS
`sourceExternalHardlinkGroups == 0`. Candidate hardlinks запрещены. Symlink не
разыменовывается; directory blocks и inode demand учитываются.

## `/opt`

Обозначения:

- `F_opt` — актуальный `Available` после cleanup или непосредственно перед
  mutation;
- `R_src` — allocation, гарантированно освобождаемая exact source delete set;
- `P_forward` — максимум allocation target во всех forward-фазах;
- `P_rollback` — максимум allocation при порядке delete candidate → restore
  source;
- `G_pre` — рост `/opt` до mutation (marker/lock не входят, если они в `/tmp`);
- `R_opt` — system safety reserve;
- `I_*` — соответствующие inode terms.

```text
OPT_FORWARD_DELTA = P_forward - R_src
OPT_ROLLBACK_DELTA = P_rollback - R_src

OPT_REQUIRED_FREE_KIB = ceil_mib(
    R_opt + max0(G_pre, OPT_FORWARD_DELTA, OPT_ROLLBACK_DELTA)
)

PASS_opt ⇔ F_opt >= OPT_REQUIRED_FREE_KIB
```

Candidate metadata содержит byte-exact `protectedDefaultEntries` для каждого
candidate-root, который clean migration может заменить пользовательским root.
Для фактического `protected-source-roots.list` вычисляются:

```text
TARGET_PROTECTED_OVERLAP = sum(candidate default entries whose root is restored)
TARGET_NON_PROTECTED     = TARGET_FULL - TARGET_PROTECTED_OVERLAP

P_forward = max(
  TARGET_FULL,
  TARGET_NON_PROTECTED + PROTECTED_SOURCE_EXACT
                       + OPKG_STATUS_TRANSIENT + OPKG_METADATA
                       + SETUP_ALLOC_UPPER + DURABLE_ALLOC_UPPER
)
```

Вычитание выполняется отдельно для allocation и inode. Оно exact и fail-closed:
список candidate defaults связан с payload manifest, отсортирован, уникален и
повторно пересчитывается после extraction. Полный target и protected source
никогда не складываются: candidate defaults удаляются до восстановления
соответствующих protected roots. При этом короткая extraction-фаза полного
target остаётся отдельным членом `max`, поэтому малый protected source не может
дать недооценку.

`P_forward` — максимум, не сумма:

```text
max(
  candidate raw extraction,
  full candidate extraction,
  candidate non-protected bytes + exact protected bytes after replacement,
  post-setup tree + bounded setup growth,
  committed tree + BROray-only OPKG metadata + bounded durable evidence
)
```

`P_rollback` учитывает, что candidate сначала полностью удаляется и только
затем source извлекается из snapshot. Source и candidate нельзя считать
одновременно. Manual/protected backup включается, только если он действительно
создаётся этой операцией; update/reinstall его автоматически не создаёт.

Rollback allocation не равна исходному `du` для sparse-файлов. Нормативная
верхняя граница восстановления строится из logical bytes каждого regular file,
округлённых к доказанному allocation unit, плюс один allocation unit на каждый
объект для directory entries, directory blocks, symlink storage и вариации
метаданных. В forward и rollback peak также входит dense transient-копия всего
текущего `/opt/lib/opkg/status`; фиксированный BROray metadata allowance не
может подменять размер foreign status.

Параллельно:

```text
PASS_opt_inode ⇔ free_inodes_opt >= reserve_inodes_opt
                  + max_phase(target_non_protected_inodes
                              + exact_protected_inodes
                              + setup/opkg/durable/status_inode_caps
                              - reclaimable_source_inodes, 0)
```

`exact_protected_inodes` берётся из factual protected manifest. Candidate
objects под восстановленными roots сначала вычитаются из target object count.
Эти пользовательские inode нельзя
вычитать вместе со всем source и не вернуть в forward phase: эти пользовательские
объекты не удаляются clean replacement. Setup logical write-set (`2048 KiB`)
округляется к измеренному `/opt` allocation unit и получает metadata term на
64 возможных inode. Durable terminal evidence имеет предел `256 KiB` на файл,
ровно семь одновременно сохраняемых bounded-файлов и восемь inode (семь файлов
плюс каталог); его allocation upper входит одновременно
в forward и rollback peaks.

`cleanupReclaimedKiB` не прибавляется к `F_opt`: повторный `df` уже содержит
освобождённое место. Значение сохраняется только как evidence.

## `/tmp`

На каждой контрольной точке актуальный `df` уже отражает живые объекты. Поэтому
прибавляется только будущий прирост относительно текущей операции:

```text
TMP_REQUIRED_NOW_KIB = R_tmp + max_future_phase(
    max0(PHASE_OPERATION_ALLOC_KIB - CURRENT_OPERATION_ALLOC_KIB)
)

PASS_tmp ⇔ F_tmp_now >= TMP_REQUIRED_NOW_KIB
```

### До snapshot

До сравнения с `df` read-only planner строит factual scope, content/allocation
manifest, service/external baseline и recovery capsule. Это входы space gate,
а не действия после успешного gate. Для manifest строится точная upper bound
явно выбранного GNU tar format с `--blocking-factor=1`. Для каждого объекта учитывается 512-byte tar
header, rounded file data, консервативно зарезервированные GNU.longname и
GNU.longlink records фактической byte-length и два terminal blocks.

```text
TAR_BOUND = 1024
          + Σ(object_header_bound
              + ceil_512(regular_logical_bytes)
              + path_extension_bound)

GZIP_BOUND = TAR_BOUND
           + ceil(TAR_BOUND / 8)
           + 65536

SNAPSHOT_STREAM_LIMIT_KIB = ceil_kib(GZIP_BOUND)
SNAPSHOT_ALLOC_UPPER_KIB  = ceil_alloc(GZIP_BOUND, tmp_allocation_unit)

TMP_REQUIRED_BEFORE_SNAPSHOT = R_tmp
                             + bounded workspace/evidence growth
                             + SNAPSHOT_ALLOC_UPPER_KIB
                             + dense recovery-capsule verification copy
```

Добавка `12.5% + 65536 bytes` консервативнее worst-case stored-deflate
overhead и не предполагает сжатие. Snapshot writer жёстко ограничивается этим
bound: `dd iflag=fullblock` удерживает точный KiB ceiling при коротких pipe
writes, persistent FIFO descriptor выполняет one-byte look-ahead и дренирует
остаток. Наличие look-ahead byte — `FAIL` до mutation, даже если producer
завершился с `0`.

После `gzip -t`, полного tar-read, member/type/mode/owner/manifest gates и
atomic rename выполняется новый `df`. Дальше используется:

```text
snapshot_actual_kib = ceil_kib(wc -c < backup.tar.gz)
```

`SNAPSHOT_ALLOC_UPPER_KIB` больше не прибавляется: фактический snapshot уже
занимает место и отражён в новом `df`.

### Candidate verification

```text
P_candidate = max(
  candidate.ipk.part,
  candidate.ipk + outer members + unpacked control
                + bounded unpacked data + bounded evidence,
  candidate.ipk + retained outer/control + candidate ancillary
                + setup outputs + future evidence + durable streams
)
```

До download metadata, связанная тем же candidate SHA-256, обязана содержать и
после extraction byte-exact подтверждать `outerMembersBytes`,
`outerMemberCount`, `controlAllocatedUpperKB` и `controlObjectCount`. Поэтому
совместный peak считается как allocation(candidate) + allocation(all outer
members) + unpacked control + unpacked data + ancillary cap; множитель `2*C`
не является допустимой заменой `C + outer`.

`.part → final` — atomic rename одной копии. Portable FIFO writer использует
`iflag=fullblock`, 1-KiB records, persistent descriptor и one-byte look-ahead;
после bounded write он отклоняет overflow и фактический размер больше
`sizeBytes`, а фазовый план до записи учитывает allocation
`ceil(sizeBytes/1024)*1024`; поэтому даже `sizeBytes+1` не создаёт
неучтённого блока. Если реализация одновременно
хранит `candidate.ipk` и извлечённый `data.tar.gz`, учитываются оба. Download
writer во время записи ограничен доказанным rounded upper и дренирует
oversized producer без дополнительной disk allocation, затем до atomic
rename требует фактический размер `<= sizeBytes`; chunked/oversized response
отклоняется. OPKG `opkg-*` temporary objects считаются
на фактическом destination даже за пределами workspace.

После полной проверки и удаления expanded validation tree выполняется третий
`df`. Rollback revalidation, capsule/member manifests и bounded logs/evidence
включены в будущий phase peak. Каждый writer имеет отдельный byte cap.
Для unpacked `control` и `data` к manifest allocation обязательно добавляется
по одной измеренной allocation unit за корень staging-каталога: эти два root
inode/block не являются строками control/payload manifest.
Input-derived precheck обязан доказать, что manifest/path writers помещаются в
cap; после snapshot/candidate/setup измеряется фактический phase growth.
Setup допускается только для byte-exact candidate-bound `package-setup.sh` в
профиле `preserve + skip Keenetic/services/maintenance/DNS`. До запуска
проверяется payload-manifest binding и marker
`candidate-bound-preserve-no-services-max64x32KiB/1`. Child получает soft
`RLIMIT_FSIZE=64` POSIX-блока (32 KiB на regular file); контракт допускает не
более 64 записываемых regular files, то есть максимум `64 × 32 = 2048 KiB`.
Его stdout и stderr до потребления ограничены отдельными FIFO writer по
`2048 KiB`; службы запускаются transaction engine уже после выхода bounded
child и не наследуют RLIMIT. Durable JSON также создаётся через FIFO writer с
точным пределом `256 KiB` на каждый из stdout/stderr, а не проверяется только
post factum. После candidate gate нормативный будущий прирост равен:

```text
TMP_POST_CANDIDATE_WRITERS = generic_evidence_alloc(4096 KiB)
                           + 2 * setup_output_alloc(2048 KiB each)
                           + 2 * durable_stream_alloc(256 KiB each)

TMP_REQUIRED_AFTER_CANDIDATE = R_tmp + TMP_POST_CANDIDATE_WRITERS
```

Каждый logical limit перед сложением округляется к измеренному `/tmp`
allocation unit. Setup output, durable streams и прочие evidence нельзя считать
одним общим `4096 KiB` term: они могут сосуществовать. Превышение individual
writer cap или input-derived aggregate allocation upper завершает операцию
fail-closed.

## Один backing filesystem

Mount identity определяется по longest match `/proc/self/mountinfo`. Если
`/opt` и `/tmp` имеют разные backing filesystems, применяются независимые gates
выше. Если identity одна:

```text
R_shared = R_opt + R_tmp

SHARED_REQUIRED_KIB = R_shared
                    + max_phase(delta_opt_phase + delta_tmp_phase)

R_shared_inode = R_opt_inode + R_tmp_inode

SHARED_REQUIRED_INODES = R_shared_inode
                       + max_phase(delta_opt_inode_phase
                                   + delta_tmp_inode_phase)

PASS_shared ⇔ F_shared >= SHARED_REQUIRED_KIB
```

Два независимых сравнения на одной FS запрещены. На каждой checkpoint оба пути
повторно измеряются и должны вернуть одинаковые `Available`/free-inode samples;
mount identities и одинаковый device остаются связанными с capability evidence.
Планировщик выполняет совместные gates:

- `T0`: максимум non-mutating snapshot delta и предварительного `/opt` delta;
- `T1`: максимум `candidate-validation tmp delta` и
  `post-candidate tmp delta + opt forward/rollback delta`;
- `T2` и pre-mutation: `post-candidate writer delta + opt peak delta`;
- post-delete: `post-candidate writer delta + max(full forward, full rollback)`.

Каждая следующая checkpoint rebased на новый factual `df`: уже записанные
snapshot/candidate/workspace blocks не прибавляются повторно. Для shared blocks
и inode обязательны границы `required` PASS и `required-1` FAIL.

## Точки обязательного повторного измерения

1. После allowlist cleanup и factual scope: `/opt`, `/tmp`, free inodes,
   mount identity и полная hardlink closure. Любой F/L inode с
   `scope_links != nlink` немедленно отклоняется.
2. Непосредственно перед snapshot.
3. После verified snapshot и compaction его временных потоков.
4. После verified candidate и удаления expanded validation tree.
5. Перед mutation barrier: полный source manifest и exact hardlink topology,
   `/opt`, `/tmp`, inodes, mount identity, allocation units и совместный peak
   при общей FS. Любое отличие от snapshot-bound inputs или появление внешней
   ссылки — `FAIL` до mutation.
6. После success либо rollback — evidence фактических peaks.

Недостаток места, inode, изменение mount identity, внешний concurrent расход,
переполнение bounded writer или неоднозначное измерение завершают операцию до
удаления source tree.

## Machine evidence

`space.json` содержит формулу `broray-space/2`, candidate SHA-256,
`sameBackingFs`, все measured/bound/actual variables, phase table, free-space
samples, inode samples, margins и `mutationStartedAtFailure`. Каждое значение
имеет единицу и источник (`df`, `find`, `wc`, exact candidate manifest).

Boundary tests обязательны для каждого gate: `required` проходит,
`required-1` отклоняется. Дополнительно тестируются incompressible snapshot,
gzip expansion, source больше/меньше target, hardlinks, symlinks, inode
exhaustion, concurrent space drift, OPKG temp вне workspace, общая/разная FS,
partial extraction с сохранением rollback reserve и evidence/log cap.
