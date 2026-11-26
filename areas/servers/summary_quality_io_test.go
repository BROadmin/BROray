package servers

import (
	"context"
	"encoding/binary"
	"testing"

	"golang.org/x/sys/unix"
)

func TestSummarySkipsUnsupportedQualityFile(t *testing.T) {
	e := newEnv(t)
	unsupported := e.addServer(t, awgLink(t, true, nil))
	supported := e.addServer(t, "trojan://pw@fixture.test:443#Supported")
	for _, id := range []string{supported, unsupported} {
		if err := writeQuality(e.d, id, map[string]any{"status": "available", "ping": 42}); err != nil {
			t.Fatal(err)
		}
	}
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	watches := map[uint32]string{}
	for _, id := range []string{supported, unsupported} {
		wd, err := unix.InotifyAddWatch(fd, QualityPath(e.d, id), unix.IN_OPEN)
		if err != nil {
			t.Fatal(err)
		}
		watches[uint32(wd)] = id
	}
	summary, err := e.s.summary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	status := map[string]any{}
	for _, r := range summary["servers"].([]any) {
		row := r.(map[string]any)
		status[asString(row["id"])] = get(row, "quality", "status")
	}
	if status[supported] != "available" || status[unsupported] != "unsupported" {
		t.Fatal("summary status changed", status)
	}
	buffer := make([]byte, unix.SizeofInotifyEvent*4)
	n, err := unix.Read(fd, buffer)
	if err != nil || n == 0 || n%unix.SizeofInotifyEvent != 0 {
		t.Fatal("expected file-open evidence", n, err)
	}
	var opened []string
	for offset := 0; offset < n; offset += unix.SizeofInotifyEvent {
		event := buffer[offset : offset+unix.SizeofInotifyEvent]
		if binary.NativeEndian.Uint32(event[4:8]) != unix.IN_OPEN || binary.NativeEndian.Uint32(event[12:16]) != 0 {
			t.Fatal("unexpected inotify event")
		}
		opened = append(opened, watches[binary.NativeEndian.Uint32(event[:4])])
	}
	if len(opened) != 1 || opened[0] != supported {
		t.Fatal("quality files opened", opened)
	}
}
