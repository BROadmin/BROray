# The installer verifies and repairs ProxyN synchronously immediately afterwards.
# Starting the delayed reconciler here would run a second writer concurrently.
BRORAY_STARTUP_RECONCILE_SKIP=1 /opt/bin/ash /opt/etc/init.d/S24broray start
