package update

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/BROadmin/broray/internal/platform"
)

// ABI is the launcher → installer contract version (design §3). The
// command line built by runInstaller is frozen by a golden test.
const ABI = 1

// ApplyOptions drive one launcher run.
type ApplyOptions struct {
	Root        platform.Root
	Source      Source
	Current     string // installed version (the running binary)
	Version     string // target; "" = newest release of the channel
	Reinstall   bool   // reinstall Current from the channel
	OperationID string
	Progress    *Emitter
	Stderr      io.Writer
	Fetcher     *Fetcher
	Now         func() time.Time
}

// InstallerArgs is the frozen ABI v1 command line of the installer.
func InstallerArgs(from, state, channel string) []string {
	return []string{"installer", "apply", "--abi", "1", "--from", from, "--state", state, "--channel", channel, "--progress", "json"}
}

// Apply runs the launcher: check → space → download → verify → installer.
// It always ends with a "result" event and returns an error unless the
// installer reported a committed update.
func Apply(ctx context.Context, o ApplyOptions) (err error) {
	e := o.Progress
	if e == nil {
		e = NewEmitter(io.Discard, "launcher")
	}
	if o.Stderr == nil {
		o.Stderr = io.Discard
	}
	op := "update"
	if o.Reinstall {
		op = "reinstall"
	}
	var res *Event
	defer func() {
		if res == nil {
			res = &Event{Event: "result", OK: Bool(err == nil), State: "failed", Code: Code(err), Message: Message(err)}
		}
		res.Source = "launcher"
		e.Emit(*res)
	}()
	lock, err := platform.TryLock(o.Root.LauncherLock())
	if errors.Is(err, platform.ErrLocked) {
		return fail(ErrBusy, nil, "Другая операция обновления BROray уже выполняется.")
	}
	if err != nil {
		return fail(ErrInstallerFailed, err, "Не удалось взять блокировку обновления.")
	}
	defer lock.Release()
	now := time.Now
	if o.Now != nil {
		now = o.Now
	}
	f := o.Fetcher
	if f == nil {
		f = NewFetcher()
	}
	e.Stage("check", 5, fmt.Sprintf("Проверяется подписанный манифест канала «%s».", o.Source.Channel))
	chk, err := f.Check(ctx, o.Source, now())
	if err != nil {
		return err
	}
	platformKey := Platform()
	rel, art, err := chk.Manifest.Target(o.Current, o.Version, platformKey, o.Reinstall)
	if err != nil {
		return err
	}
	e.Log(fmt.Sprintf("Манифест канала %s подтверждён (ключ %s, SHA-256 %s); %s: %s → %s, сборка %s, %d байт.",
		o.Source.Channel, chk.KeyID, chk.SHA256[:16], op, o.Current, rel.Version, platformKey, art.Size))
	tmp := o.Root.Tmp()
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return err
	}
	tmpNeed := art.Size
	if art.Format == "tar.gz" {
		tmpNeed += art.UnpackedSize
	}
	// tmp and versions/ are usually on the same /opt filesystem: the sizes add up.
	if err := CheckSpace(map[string]int64{tmp: tmpNeed, o.Root.Dir: art.InstalledSize()}); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(tmp, "update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	e.Stage("download", 10, fmt.Sprintf("Скачивается установщик BROray %s.", rel.Version))
	bin, err := f.Download(ctx, art, chk.URL, dir, func(done, total int64) {
		e.Emit(Event{Event: "stage", Stage: "download", Progress: 10 + int(18*done/max64(total, 1)),
			Message: fmt.Sprintf("Скачивается установщик BROray %s: %d из %d КБ.", rel.Version, done>>10, total>>10)})
	})
	if err != nil {
		return err
	}
	e.Stage("verify", 30, "Размер и SHA-256 установщика совпали с подписанным манифестом.")
	res, err = runInstaller(o, e, bin, op)
	return err
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// runInstaller starts the downloaded installer, relays its progress lines
// and returns its result event. A run without a result event, or with a
// non-zero exit, is a failure even if the last stage looked good.
func runInstaller(o ApplyOptions, e *Emitter, bin, op string) (*Event, error) {
	// Not tied to ctx: once started the installer must finish (or roll
	// back) on its own; killing it mid-switch is what the journal avoids.
	cmd := exec.Command(bin, InstallerArgs(o.Current, o.Root.Dir, o.Source.Channel)...)
	cmd.Env = append(os.Environ(), "BRORAY_ROOT="+o.Root.Dir, "BRORAY_UPDATE_OPERATION_ID="+o.OperationID, "BRORAY_UPDATE_OPERATION="+op)
	cmd.Dir = filepath.Dir(bin)
	cmd.Stderr = o.Stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fail(ErrInstallerFailed, err, "Скачанный установщик не запускается на этом роутере.")
	}
	var last *Event
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var ev Event
		if json.Unmarshal(sc.Bytes(), &ev) != nil || ev.Event == "" {
			fmt.Fprintf(o.Stderr, "installer: %s\n", sc.Text())
			continue
		}
		if ev.Event == "result" {
			ev := ev
			last = &ev
			continue
		}
		if ev.Source == "" {
			ev.Source = "installer"
		}
		e.Emit(ev)
	}
	scanErr := sc.Err()
	if scanErr != nil {
		// Scanner stops consuming stdout after an overlong or unreadable line.
		// Close the pipe before Wait so the installer cannot block writing to it.
		_ = out.Close()
	}
	werr := cmd.Wait()
	if scanErr != nil {
		return nil, fail(ErrInstallerFailed, errors.Join(scanErr, werr), "Вывод установщика повреждён или слишком длинный: результат обновления не подтверждён.")
	}
	if last == nil || last.OK == nil {
		return nil, fail(ErrInstallerFailed, werr, "Установщик завершился без итогового события: результат обновления не подтверждён. Проверьте состояние установки.")
	}
	if werr != nil && *last.OK {
		return nil, fail(ErrInstallerFailed, werr, "Установщик сообщил об успехе, но завершился с ошибкой: результат обновления не подтверждён.")
	}
	if !*last.OK {
		code := last.Code
		if code == "" {
			code = ErrInstallerFailed
		}
		return last, &Error{Code: code, Message: last.Message}
	}
	return last, nil
}
