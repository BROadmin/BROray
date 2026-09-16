# Restoring DoT after clean replacement

The first r12 bootstrap preserved BROray files byte-for-byte but did not reapply
the three managed DNS-over-TLS records removed by normal uninstall. Physical
running-config comparison reproduced the missing lines. That installer is not
accepted for release.

Installer revision 2 calls the existing transactional DoT manager only after a
verified preserved archive has been restored. It uses the existing exact-receipt
rollback mode, checks the final live selection, and fails installation if apply
or verification fails. Fresh installation and saved unapplied selections do not
gain a DNS change. Foreign/ambiguous selectors retain the manager's safety rules.

The compact app r12 payload is unchanged. The installer has its own source
commit, SHA-256 and separate physical acceptance. Seven Linux wiring/negative
tests use a fixture manager; physical tests use the real installed manager.
