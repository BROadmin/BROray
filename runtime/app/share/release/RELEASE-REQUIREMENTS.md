# BROray release contract 3.0

The canonical lifecycle for this payload is `compact-app-rename/1`. The initial
metadata package is a clean bootstrap; application update and reinstall use the
persistent updater. Historical package transaction state and a previous IPK are
not required.

The protected state consists of user configuration, subscriptions, servers,
routes and user backups. It is shared outside immutable slots. Normal uninstall
exports it using the canonical protected-backup manifest; the next clean install
may restore only a complete, safe, checksummed archive selected by the protected
pointer. Full uninstall removes it.

Every operation is fail closed on malformed identity, incomplete input,
ambiguous ownership, stale or conflicting locks, insufficient space, missing
capability, invalid Xray configuration, unhealthy WebUI, damaged OPKG
registration or failed rollback. Foreign Keenetic objects and foreign service
processes are never adopted by name alone.

The release is reproducible from the frozen source tree. Promotion requires
byte-identical independent builds, repeated validation of the final carrier and
the physical compatible-Keenetic-aarch64 gates listed in the candidate-specific
requirements.
