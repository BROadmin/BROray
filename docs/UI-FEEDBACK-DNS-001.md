# UI feedback and DNS deletion clarity — stage 04

Based on b8aab47470d8a0cdee39be374c349723aac176e0. Eight UI runtime files only.
The DNS deletion backend and all router write policy are unchanged.

## Implemented
- Native collapsed technical sections; visible operational status remains outside.
- Exact list of eligible saved selections in deletion confirmation, including external entries.
- Unsaved checkbox changes block deletion; test-selected persists the choice without installation.
- Forced read before preview and again after confirmation; changed preview blocks POST.
- Whole-second TLS duration displayed approximately, never as exact millisecond ping.
- Recent failed TLS checks labelled failed, not stale; test completion uses warning for failures.
- Route preflight/verify errors retain codes and details and distinguish preflight from mutation.

## Boundaries
Deletion still uses server-side saved selection. The final read and deletion are NOT atomic.
Another client can change selection between them. Closing that race needs a separately approved
backend contract (selection-bound token/request); this UI patch does not claim to close it.
The existing backend rechecks exact live selectors, but cannot bind them to a UI preview.
Clock jumps and old duration data cannot be turned into precise timing by presentation changes.
No hardware test, real TLS measurement, router mutation or full route HTTP test is claimed.
