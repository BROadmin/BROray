package web

import "time"

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }
