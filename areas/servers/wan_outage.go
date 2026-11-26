package servers

import (
	"context"
	"time"

	"github.com/BROadmin/broray/internal/keenetic"
)

const wanUnavailable = "WAN_UNAVAILABLE"
const wanUnavailableMessage = "Keenetic подтверждает сбой интернет-подключения. Активный VPN-сервер сохранён."

func confirmedWANOutage(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return keenetic.NewNDMC().InternetOutage(ctx)
}
