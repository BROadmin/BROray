package keenetic

import (
	"errors"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
)

type sourceDescriptionPreview interface{ PreviewOrdinaryDescription(string) (string, error) }

func TestSourceDescriptionPreviewIsReadOnly(t *testing.T) {
	h := newHarness(t)
	registerReserveDescriptionServices(h.s)
	preview, ok := any(h.d.Svc).(sourceDescriptionPreview)
	if !ok {
		t.Fatal("SOURCE_DESCRIPTION_PREVIEW_ABSENT")
	}
	for _, tc := range []struct {
		name, want string
		fail       bool
	}{
		{"B", "BROray - B", false}, {"New\nServer", "BROray - New Server", false},
		{"", "BROray", false}, {"Имя сервера", "BROray - Имя сервера", false}, {"bad\x00name", "", true},
	} {
		h.r.resetCommands()
		got, err := preview.PreviewOrdinaryDescription(tc.name)
		if (err != nil) != tc.fail || got != tc.want || len(h.r.cmds) != 0 {
			t.Fatal("preview changed contract/router", tc.name, got, tc.want, err)
		}
		current, err := h.d.Svc.OrdinaryInterfaceDescription()
		if err != nil || current != "BROray - Netherlands" {
			t.Fatal("preview changed active selection", current, err)
		}
	}
}

func TestSourceDescriptionPreviewRequiresProvider(t *testing.T) {
	services := &compat.Services{}
	if _, err := services.PreviewOrdinaryDescription("B"); !errors.Is(err, compat.ErrServiceUnavailable) {
		t.Fatal("unavailable owner did not refuse preview", err)
	}
}

func TestSourceDescriptionControlValidation(t *testing.T) {
	for _, suffix := range []string{"\x00", "\t", "\r", "\n", "\x7f", "\u0085", "\xff"} {
		if validDescription("BROray - bad" + suffix + "name") {
			t.Errorf("accepted invalid description suffix %q", suffix)
		}
	}
	for _, name := range []string{"BROray", "BROray - Name", "BROray - Имя сервера"} {
		if !validDescription(name) {
			t.Errorf("rejected ordinary description %q", name)
		}
	}
}
