# Shared Lighttpd during compact removal

Physical r11 uninstall refused in preflight before any mutation: the legacy
Lighttpd guard required an ownership receipt which compact clean bootstrap never
creates. The compact installer runs a separate BROray web service and leaves the
shared Entware init/config intact.

The new classification requires an absent (not symlinked) guard directory, a
regular package control with unique compact lifecycle and metadata-only bootstrap
fields, the enabled stock init, and the existing complete asset validator. Only
then does uninstall preserve the shared dependency without running legacy restore
or package removal. Existing or malformed ownership records retain the legacy
fail-closed path. The classification is captured before OPKG removes metadata.

Baseline: one failed / six passed Linux checks plus physical preflight refusal.
Final tests: isolated classification/negative cases and real ARM asset validation;
installed rollback and full removal/reinstall acceptance are recorded separately.
The classification tests substitute only the asset validator; physical evidence
uses the actual Entware package, configuration, init, permissions and checksums.
