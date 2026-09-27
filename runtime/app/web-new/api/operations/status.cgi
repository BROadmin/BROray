#!/opt/bin/ash
. /opt/broray/web-new/api/operations/common.sh
if [ "${REQUEST_METHOD:-}" = POST ]; then
    broray_operations_api POST queue-lookup
else
    broray_operations_api GET status
fi
