package routes

import (
	"strings"
	"testing"
)

func TestParseBatFiltersBogonsAndDuplicates(t *testing.T) {
	src := "# Netflix\r\nroute add 2.17.64.0 mask 255.255.224.0 0.0.0.0\r\nroute add 100.64.0.0 mask 255.192.0.0 0.0.0.0\r\nroute add 2.17.64.5 mask 255.255.224.0 0.0.0.0\r\nroute add 10.0.0.0 mask 255.0.0.0 0.0.0.0\r\nroute add 0.0.0.0 mask 0.0.0.0 0.0.0.0\r\nroute add 1.2.3.4 mask 255.255.255.255 0.0.0.0\r\nroute ADD 8.8.8.0 MASK 255.255.255.0 0.0.0.0 metric 5\r\nroute add 1.2.3 mask 255.0.0.0 0.0.0.0\r\n"
	rep, err := ParseBat(strings.NewReader(src), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Routes) != 3 {
		t.Fatalf("routes: %+v", rep.Routes)
	}
	if len(rep.Bogons) != 2 || rep.Duplicates != 1 || len(rep.Invalid) != 2 {
		t.Fatalf("report: bogons=%v dup=%d invalid=%v", rep.Bogons, rep.Duplicates, rep.Invalid)
	}
	if rep.Routes[0].Key() != "1.2.3.4 255.255.255.255" || rep.Routes[1].Key() != "2.17.64.0 255.255.224.0" {
		t.Fatalf("order/normalisation: %+v", rep.Routes)
	}
}

func TestPlanSharedRoutes(t *testing.T) {
	a := Route{Network: "1.0.0.0", Mask: "255.0.0.0"}
	b := Route{Network: "2.0.0.0", Mask: "255.0.0.0"}
	c := Route{Network: "3.0.0.0", Mask: "255.0.0.0"}
	in := Installed{Bundles: map[string][]Route{"x": {a, b}}}
	add, rem := Plan(in, "y", []Route{b, c}, true)
	if len(add) != 1 || add[0] != c || len(rem) != 0 {
		t.Fatalf("apply y: add=%v rem=%v", add, rem)
	}
	in.Bundles["y"] = []Route{b, c}
	add, rem = Plan(in, "x", nil, false)
	if len(add) != 0 || len(rem) != 1 || rem[0] != a {
		t.Fatalf("remove x: add=%v rem=%v", add, rem)
	}
	add, rem = Plan(in, "y", []Route{c}, true)
	if len(add) != 0 || len(rem) != 0 {
		t.Fatalf("re-apply y without b (b still owned by x): add=%v rem=%v", add, rem)
	}
}
