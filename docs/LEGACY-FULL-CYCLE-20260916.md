# Physical legacy recovery and signed update

Status: the controlled blocked original 3.1.0-r09c02 installation recovered
through signed c04 delivery and updated normally to 3.1.1-r08c01.

- Test router: verified Keenetic Peak KN-2710, KeeneticOS 5.01.C.4.0-1, ARM64.
- Real update API refused the controlled fence with HTTP 409 before recovery.
- c04 ran through Keenetic Web CLI after native minisign verification.
- Full installed-process inventory passed; no fixture process exclusions.
- The exact five-file fence was archived; no blind removal or business rollback.
- Xray identity stayed unchanged throughout recovery and the subsequent update.
- Running/startup Keenetic configuration and BROray business files matched.
- Automation pause remained set; the existing lifecycle controller explicitly
  started and verified monitor, Home snapshots, switching and subscriptions
  after the update. This final step is part of the legacy recovery runbook.

Source: a432c27a71b0948bd013721cb8aba28a9630458b for c04. The application
runtime in r08c01 is unchanged by the later compatibility-only commits.
Evidence: `docs/evidence/legacy-full-cycle-20260916/` at workspace root;
private configuration/session archives are in `.private/legacy-full-cycle-20260916/`.

Preserved failures explain the compatibility fixes: unsupported BusyBox `od`
options, the exact native-auth nginx master title without a terminal NUL, and
completed coordinator history retaining self-contained retired-fence links.
Current c04 has 45 isolated Linux checks, three native ARM bundle checks and
the full installed application cycle above. Earlier eight ARM prefix checks
cover unchanged native primitives and were not rerun against exact c04.

Limitations: the initial downgrade used an explicit laboratory target selector;
there is no normal product downgrade API. The controlled fence does not prove
the original incident's cause (ROOT_CAUSE_NOT_PROVEN). No real VPN traffic,
route recovery, fault/endurance or final-release acceptance is claimed here.
