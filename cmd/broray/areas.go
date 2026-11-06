package main

// Every 3.x-compatible WebUI area registers itself (compat.Area) from init().
import (
	_ "github.com/BROadmin/broray/areas/dns"
	_ "github.com/BROadmin/broray/areas/keenetic"
	_ "github.com/BROadmin/broray/areas/ops"
	_ "github.com/BROadmin/broray/areas/routes"
	_ "github.com/BROadmin/broray/areas/servers"
	_ "github.com/BROadmin/broray/areas/subscriptions"
	_ "github.com/BROadmin/broray/areas/xray"
)
