package subscriptions

import "github.com/BROadmin/broray/areas/servers"

// importNode is a validated private record. Native transports never pass
// through a fabricated share link or public preview object.
type importNode struct {
	Record   servers.Record
	Warnings []string
}

type nativeJSONInput struct {
	Outbound map[string]any
	Name     string
}

func parseJSONNode(out map[string]any, name, id string, source map[string]any) (importNode, error) {
	rec, err := servers.ParseNativeOutbound(out, name, id, source)
	if err != nil {
		return importNode{}, err
	}
	return importNode{Record: rec}, nil
}

func isNativeOutbound(o map[string]any) bool {
	st, _ := o["streamSettings"].(map[string]any)
	return o["protocol"] == "masque" || st["network"] == "xdrive" || st["network"] == "masque"
}
