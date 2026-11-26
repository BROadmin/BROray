package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/update"
)

func TestBootstrapProcess(t *testing.T) {
	end := slices.Index(os.Args, "--")
	if end == -1 {
		return
	}
	os.Args = append([]string{"release"}, os.Args[end+1:]...)
	main()
	os.Exit(0)
}

func newBootstrapArchive(t *testing.T, key string) ([]byte, int64) {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	payload := "#!/bin/sh\nset -eu\nif [ \"$*\" = version ]; then printf 'BROray %s\\n' \"${INSTALL_VERSION:-3.5.10}\"; exit 0; fi\n[ \"$*\" = 'installer install --first-install' ]\nprintf '%s\\n' '" + key + "' >> \"$INSTALL_LOG\"\n[ \"${INSTALL_FAIL:-0}\" = 0 ] || exit 7\nprintf '%s\\n' 'local WebUI: https://fixture.example/'\n"
	if err := tw.WriteHeader(&tar.Header{Name: "package/broray", Mode: 0755, Size: int64(len(payload))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(payload)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes(), int64(len(payload))
}

func TestBootstrapCLI(t *testing.T) {
	const version = "3.5.10-r1"
	const scriptURL = "https://release.example/3.5.10-r1/install-broray.sh"
	dir := t.TempDir()
	write := func(name string, b []byte, mode os.FileMode) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, b, mode); err != nil {
			t.Fatal(err)
		}
	}
	architectures := []struct{ key, entware, name string }{
		{"arm64", "aarch64-3.10", "aarch64.tar.gz"},
		{"arm/7", "armv7-3.2", "armv7.tar.gz"},
		{"mips/softfloat", "mips-3.4", "mips.tar.gz"},
		{"mipsle/softfloat", "mipsel-3.4", "mipsel.tar.gz"},
	}
	release := update.Release{Version: version, Installer: map[string]update.Artifact{}}
	fixtures := filepath.Join(dir, "fixtures")
	for _, arch := range architectures {
		b, size := newBootstrapArchive(t, arch.key)
		write(filepath.Join(fixtures, arch.name), b, 0600)
		release.Installer[arch.key] = update.Artifact{URL: arch.name, Size: int64(len(b)), SHA256: fmt.Sprintf("%x", sha256.Sum256(b)),
			Format: "tar.gz", Entry: "package/broray", UnpackedSize: size}
	}
	key, err := update.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	manifest := update.Manifest{SchemaVersion: update.SchemaVersion, Channel: "test", IssuedAt: time.Now().Add(-time.Hour),
		ExpiresAt: time.Now().Add(time.Hour), Releases: []update.Release{release}}
	saveManifest := func() {
		t.Helper()
		b, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		write(manifestPath, b, 0600)
		write(manifestPath+".sig", key.Sign(b, "fixture manifest", "temporary test key"), 0600)
	}
	saveManifest()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	invoke := func(dst, sourceURL, version string) ([]byte, error) {
		return exec.Command(executable, "-test.run=^TestBootstrapProcess$", "--", "bootstrap", "-manifest", manifestPath,
			"-p", key.Public().Encode(), "-version", version, "-script-url", sourceURL, "-o", dst).CombinedOutput()
	}
	out := filepath.Join(dir, "generated")
	if report, err := invoke(out, scriptURL, version); err != nil {
		t.Fatalf("signed release generator: %v\n%s", err, report)
	}
	read := func(path string) []byte {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	script := read(filepath.Join(out, "install-broray.sh"))
	command := string(read(filepath.Join(out, "install-command.txt")))
	if strings.Count(command, "\n") != 2 || !strings.Contains(command, fmt.Sprintf("%x", sha256.Sum256(script))) ||
		bytes.Contains(script, []byte("\r")) {
		t.Fatal("two-line command is not pinned to exact generated LF bytes")
	}
	snippet := string(read(filepath.Join(out, "install-command.html")))
	_, snippetCode, ok := strings.Cut(snippet, "<code>")
	displayed, _, closed := strings.Cut(snippetCode, "</code>")
	if !ok || !closed || strings.Count(snippet, `<button class="copy"`) != 1 || html.UnescapeString(displayed) != command {
		t.Fatal("installation page would copy bytes different from the verified two lines")
	}
	for _, invalid := range []struct{ label, url string }{{"output already exists", scriptURL}, {"insecure URL", "http://release.example/install-broray.sh"}, {"wrong signature", scriptURL}, {"missing ABI", scriptURL}, {"older release", scriptURL}} {
		t.Run(invalid.label, func(t *testing.T) {
			dst := filepath.Join(dir, strings.ReplaceAll(invalid.label, " ", "-"))
			target := version
			switch invalid.label {
			case "output already exists":
				dst = out
			case "wrong signature":
				write(manifestPath+".sig", []byte("signature does not match"), 0600)
				defer saveManifest()
			case "missing ABI":
				old := release.Installer["mips/softfloat"]
				delete(release.Installer, "mips/softfloat")
				saveManifest()
				defer func() { release.Installer["mips/softfloat"] = old; saveManifest() }()
			case "older release":
				target = "3.5.9-r9"
				manifest.Releases[0].Version = target
				saveManifest()
				defer func() { manifest.Releases[0].Version = version; saveManifest() }()
			}
			if report, err := invoke(dst, invalid.url, target); err == nil {
				t.Fatalf("accepted invalid release input: %s", report)
			}
			if dst != out {
				if _, err := os.Lstat(dst); !os.IsNotExist(err) {
					t.Fatalf("rejected generation created an output: %v", err)
				}
			} else if !bytes.Equal(read(filepath.Join(out, "install-broray.sh")), script) {
				t.Fatal("existing release script was overwritten")
			}
		})
	}

	for _, scenario := range []struct {
		name, arch, archive, key, change string
	}{
		{"ARM64", architectures[0].entware, architectures[0].name, architectures[0].key, ""},
		{"ARMv7", architectures[1].entware, architectures[1].name, architectures[1].key, ""},
		{"MIPS", architectures[2].entware, architectures[2].name, architectures[2].key, ""},
		{"MIPSEL", architectures[3].entware, architectures[3].name, architectures[3].key, ""},
		{"unknown arch", "unknown-1", "", "", ""},
		{"ambiguous arch", "aarch64-3.10 160\narch mips-3.4", "", "", ""},
		{"already installed", architectures[0].entware, "", "", "installed"},
		{"dangling current link", architectures[0].entware, "", "", "link"},
		{"legacy service", architectures[0].entware, "", "", "legacy"},
		{"bad archive hash", architectures[0].entware, architectures[0].name, "", "archive"},
		{"native install failed", architectures[0].entware, architectures[0].name, "", "install"},
		{"mislabeled executable", architectures[0].entware, architectures[0].name, "", "version"},
		{"bad script hash", architectures[0].entware, "", "", "script"},
		{"interrupted script download", architectures[0].entware, "", "", "download"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			sandbox := t.TempDir()
			opt := filepath.Join(sandbox, "opt")
			installLog := filepath.Join(sandbox, "install.log")
			fetchLog := filepath.Join(sandbox, "fetch.log")
			files := filepath.Join(sandbox, "files")
			adapt := strings.NewReplacer("/opt/", opt+"/", "/tmp/broray-", sandbox+"/broray-")
			adapted := []byte(adapt.Replace(string(script)))
			command := adapt.Replace(command)
			command = strings.Replace(command, fmt.Sprintf("%x", sha256.Sum256(script)), fmt.Sprintf("%x", sha256.Sum256(adapted)), 1)
			write(filepath.Join(files, "install-broray.sh"), adapted, 0600)
			write(filepath.Join(opt, "bin", "ash"), []byte("#!/bin/sh\nexec /bin/sh \"$@\"\n"), 0700)
			write(filepath.Join(opt, "bin", "opkg"), []byte("#!/bin/sh\n[ \"$*\" = print-architecture ] || exit 2\nprintf 'arch all 1\\narch noarch 1\\narch %s 160\\n' \"$OPKG_ARCH\"\n"), 0700)
			write(filepath.Join(opt, "bin", "curl"), []byte(`#!/bin/sh
set -eu
url= out=
while [ "$#" -gt 0 ]; do
    case "$1" in
        -o) out=$2; shift ;;
        https://*) url=$1 ;;
    esac
    shift
done
[ -n "$url" ] && [ -n "$out" ]
name=${url##*/}
printf '%s\n' "$name" >> "$FETCH_LOG"
if [ -f "$FIXTURES/$name" ]; then cp "$FIXTURES/$name" "$out"; else cp "$ARCHIVES/$name" "$out"; fi
[ "${FAIL_FETCH:-}" != "$name" ]
`), 0700)
			switch scenario.change {
			case "installed":
				write(filepath.Join(opt, "etc", "init.d", "S24broray"), []byte("existing service"), 0600)
			case "link":
				dir := filepath.Join(opt, "broray")
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("missing-version", filepath.Join(dir, "current")); err != nil {
					t.Fatal(err)
				}
			case "legacy":
				write(filepath.Join(opt, "etc", "init.d", "S22broray-updater"), []byte("previous branch"), 0600)
			case "archive":
				write(filepath.Join(files, scenario.archive), []byte("untrusted tar bytes"), 0600)
			case "script":
				write(filepath.Join(files, "install-broray.sh"), []byte("untrusted script bytes"), 0600)
			}
			cmd := exec.Command("/bin/sh", "-c", command)
			cmd.Env = append(os.Environ(), "PATH="+filepath.Join(opt, "bin")+":/usr/bin:/bin", "OPKG_ARCH="+scenario.arch,
				"FIXTURES="+files, "ARCHIVES="+fixtures, "INSTALL_LOG="+installLog, "FETCH_LOG="+fetchLog)
			if scenario.change == "download" {
				cmd.Env = append(cmd.Env, "FAIL_FETCH=install-broray.sh")
			}
			if scenario.change == "install" {
				cmd.Env = append(cmd.Env, "INSTALL_FAIL=1")
			}
			if scenario.change == "version" {
				cmd.Env = append(cmd.Env, "INSTALL_VERSION=3.5.9")
			}
			log, err := cmd.CombinedOutput()
			expectedFetches := []string{"install-broray.sh"}
			if scenario.archive != "" {
				expectedFetches = append(expectedFetches, scenario.archive)
			}
			if fetches := strings.Fields(string(read(fetchLog))); !slices.Equal(fetches, expectedFetches) {
				t.Fatalf("unexpected artifact traffic: %v, %s", fetches, log)
			}
			for _, pattern := range []string{"broray-script.*", "broray-package.*"} {
				if debris, err := filepath.Glob(filepath.Join(sandbox, pattern)); err != nil || len(debris) != 0 {
					t.Fatalf("bootstrap kept a temporary directory: %v %v", debris, err)
				}
			}
			if scenario.change == "install" {
				if err == nil || bytes.Contains(log, []byte(version)) || string(read(installLog)) != "arm64\n" {
					t.Fatalf("native failure reported success or retried execution: %v %s", err, log)
				}
				return
			}
			if scenario.key == "" {
				if err == nil {
					t.Fatalf("invalid installation reported success: %s", log)
				}
				if _, err := os.Lstat(installLog); !os.IsNotExist(err) {
					t.Fatalf("invalid bootstrap invoked native installer: %v", err)
				}
				return
			}
			if err != nil || strings.TrimSpace(string(read(installLog))) != scenario.key ||
				!bytes.Contains(log, []byte(version)) || !bytes.Contains(log, []byte("local WebUI: https://fixture.example/")) {
				t.Fatalf("wrong exact match, call or output: %v, %s", err, log)
			}
		})
	}
}
