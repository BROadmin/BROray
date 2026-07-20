#!/bin/sh

BRORAY_BASE="${BRORAY_BASE:-/opt/broray}"

. "$BRORAY_BASE/lib/interface-core.sh" ||
{
    echo "ОШИБКА: interface-core.sh не загружен" >&2
    return 1 2>/dev/null
}

. "$BRORAY_BASE/lib/interface-sync.sh" ||
{
    echo "ОШИБКА: interface-sync.sh не загружен" >&2
    return 1 2>/dev/null
}

. "$BRORAY_BASE/lib/interface-manage.sh" ||
{
    echo "ОШИБКА: interface-manage.sh не загружен" >&2
    return 1 2>/dev/null
}

broray_interface_usage()
{
    echo "Использование:"
    echo "  ash interface.sh status"
    echo "  ash interface.sh check"
    echo "  ash interface.sh expected-name"
    echo "  ash interface.sh sync-name"
    echo "  ash interface.sh create"
    echo "  ash interface.sh repair"
    echo "  ash interface.sh delete"
}

broray_interface_main()
{
    action="${1:-}"

    case "$action" in
        status)
            broray_interface_status
            ;;
        check)
            broray_interface_check
            ;;
        expected-name)
            broray_interface_expected_description
            ;;
        sync-name)
            broray_interface_sync_description
            ;;
        create)
            broray_interface_create
            ;;
        repair)
            broray_interface_repair
            ;;
        delete)
            broray_interface_delete
            ;;
        *)
            broray_interface_usage
            return 1
            ;;
    esac
}

broray_interface_main "$@"
