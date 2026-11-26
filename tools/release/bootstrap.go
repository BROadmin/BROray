package main

import (
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"html"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BROadmin/broray/internal/update"
)

const bootstrapName = "install-broray.sh"

// These are Entware's opkg identifiers, paired with the exact updater
// platform keys. Neither the kernel architecture nor a generic GOARCH entry
// distinguishes all of the variants the Entware binary must support.
var bootstrapPlatforms = []struct{ entware, platform string }{
	{"aarch64-3.10", "arm64"},
	{"armv7-3.2", "arm/7"},
	{"mips-3.4", "mips/softfloat"},
	{"mipsel-3.4", "mipsle/softfloat"},
}

var bootstrapEntry = regexp.MustCompile(`^(?:[A-Za-z0-9_][A-Za-z0-9._-]*/)*broray$`)

func bootstrap(args []string) error {
	fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	manifestFile := fs.String("manifest", "", "signed local release manifest (with .sig)")
	publicKey := fs.String("p", "", "trusted public key (base64 line or .pub file)")
	version := fs.String("version", "", "exact version in the manifest")
	rawURL := fs.String("script-url", "", "release-specific HTTPS URL for install-broray.sh")
	out := fs.String("o", "", "new output directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || *manifestFile == "" || *publicKey == "" || !update.ValidVersion(*version) || *out == "" {
		return errors.New("usage: release bootstrap -manifest <signed file> -p <public key> -version <version> -script-url <https URL> -o <new directory>")
	}
	series, _, _ := strings.Cut(*version, "-")
	if update.CompareVersions(series, "3.5.10") < 0 {
		return errors.New("two-line bootstrap requires BROray 3.5.10 or newer")
	}
	scriptURL, err := url.Parse(*rawURL)
	if err != nil || !bootstrapHTTPS(scriptURL) || path.Base(scriptURL.Path) != bootstrapName {
		return errors.New("-script-url must identify the HTTPS release file install-broray.sh")
	}
	data, _, _, err := readSignedFile(*manifestFile, *publicKey)
	if err != nil {
		return err
	}
	manifest, err := update.ParseManifest(data)
	if err != nil {
		return err
	}
	release, listed := manifest.Find(*version)
	if !listed || manifest.IsRevoked(*version) {
		return fmt.Errorf("release %s is absent or revoked", *version)
	}

	script, err := renderBootstrap(scriptURL, release)
	if err != nil {
		return err
	}
	command := bootstrapCommand(scriptURL.String(), fmt.Sprintf("%x", sha256.Sum256([]byte(script))))
	if err := os.Mkdir(*out, 0755); err != nil {
		return err
	}
	scriptPath := filepath.Join(*out, bootstrapName)
	commandPath := filepath.Join(*out, "install-command.txt")
	snippetPath := filepath.Join(*out, "install-command.html")
	published := false
	defer func() {
		if !published {
			_ = os.Remove(scriptPath)
			_ = os.Remove(commandPath)
			_ = os.Remove(snippetPath)
			_ = os.Remove(*out)
		}
	}()
	if err := os.WriteFile(scriptPath, []byte(script), 0644); err != nil {
		return err
	}
	if err := os.WriteFile(commandPath, []byte(command), 0644); err != nil {
		return err
	}
	snippet := "<p>Скопируйте обе строки и вставьте их в терминал Entware.</p>\n" +
		`<pre><button class="copy" type="button" aria-label="Скопировать команды установки BROray">Скопировать</button><code>` +
		html.EscapeString(command) + "</code></pre>\n"
	if err := os.WriteFile(snippetPath, []byte(snippet), 0644); err != nil {
		return err
	}
	published = true
	fmt.Printf("%s: %s, install-command.txt and install-command.html\n", release.Version, scriptPath)
	return nil
}

func bootstrapHTTPS(u *url.URL) bool {
	return u != nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil &&
		u.Path != "" && !u.ForceQuery && u.RawQuery == "" && u.Fragment == ""
}

func quoteBootstrap(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func renderBootstrap(scriptURL *url.URL, release update.Release) (string, error) {
	var script strings.Builder
	script.WriteString(bootstrapHeader)
	series, _, _ := strings.Cut(release.Version, "-")
	fmt.Fprintf(&script, "release_version=%s\nrelease_series=%s\n\ncase \"$arch\" in\n", quoteBootstrap(release.Version), quoteBootstrap(series))
	seenURLs := make(map[string]bool, len(bootstrapPlatforms))
	for _, platform := range bootstrapPlatforms {
		artifact, ok := release.Installer[platform.platform]
		if !ok || artifact.Format != "tar.gz" || !bootstrapEntry.MatchString(artifact.Entry) {
			return "", fmt.Errorf("no suitable pinned archive for %s", platform.platform)
		}
		ref, err := url.Parse(artifact.URL)
		if err != nil || ref.Host != "" && ref.Scheme == "" {
			return "", fmt.Errorf("invalid archive URL for %s", platform.platform)
		}
		archiveURL := scriptURL.ResolveReference(ref)
		if !bootstrapHTTPS(archiveURL) || seenURLs[archiveURL.String()] {
			return "", fmt.Errorf("archive URL for %s must be a distinct HTTPS address", platform.platform)
		}
		seenURLs[archiveURL.String()] = true
		fmt.Fprintf(&script, "    %s) url=%s; sha=%s; entry=%s; size=%d; unpacked=%d ;;\n",
			platform.entware, quoteBootstrap(archiveURL.String()), quoteBootstrap(artifact.SHA256),
			quoteBootstrap(artifact.Entry), artifact.Size, artifact.UnpackedSize)
	}
	script.WriteString(bootstrapFooter)
	return script.String(), nil
}

func bootstrapCommand(scriptURL, digest string) string {
	return "broray_install_dir=; broray_install_dir=$(mktemp -d /tmp/broray-script.XXXXXXXX) && curl -q -fL --proto '=https' --proto-redir '=https' --connect-timeout 15 --max-time 120 -o \"$broray_install_dir/install-broray.sh\" " + quoteBootstrap(scriptURL) +
		" || { if [ -n \"$broray_install_dir\" ]; then rm -f \"$broray_install_dir/install-broray.sh\"; rmdir \"$broray_install_dir\"; fi; unset broray_install_dir; (exit 1); }\n" +
		`( set -eu; : "${broray_install_dir:?Сначала скачайте скрипт первой строкой}"; trap 'rm -f "$broray_install_dir/install-broray.sh"; rmdir "$broray_install_dir"' 0; actual=$(sha256sum "$broray_install_dir/install-broray.sh"); [ "${actual%% *}" = ` + quoteBootstrap(digest) +
		` ] || { printf '%s\n' 'SHA-256 скрипта не совпал.' >&2; exit 1; }; /opt/bin/ash "$broray_install_dir/install-broray.sh" )` + "\n"
}

const bootstrapHeader = `#!/bin/sh
set -eu
umask 077
export PATH=/opt/bin:/opt/sbin:/usr/bin:/bin:/sbin

fail() { printf '%s\n' "$1" >&2; exit 1; }
[ -x /opt/bin/opkg ] || fail 'Для первой установки нужен Entware.'
for tool in curl awk sha256sum wc tar mktemp chmod; do
    command -v "$tool" >/dev/null || fail "В Entware не найдена команда $tool."
done
for marker in /opt/broray/current /opt/bin/broray /opt/etc/init.d/S24broray /opt/etc/init.d/S22broray-updater /opt/bin/broray-system /opt/libexec/broray-updater; do
    if [ -e "$marker" ] || [ -L "$marker" ]; then
        fail 'BROray уже установлен. Для 3.5 используйте обновление в WebUI, для 3.2 — инструкцию по переходу.'
    fi
done

arch_list=$(/opt/bin/opkg print-architecture) || fail 'Не удалось определить архитектуру Entware.'
arch=$(printf '%s\n' "$arch_list" | awk '
    NF != 0 && ($1 != "arch" || NF != 3 || $3 !~ /^[0-9]+$/) { exit 1 }
    NF == 3 && $2 != "all" && $2 != "noarch" {
        if (selected != "" && selected != $2) exit 1
        selected = $2
    }
    END { if (selected == "") exit 1; print selected }
') || fail 'Архитектура Entware не определена однозначно.'

`

const bootstrapFooter = `    *) fail "Архитектура Entware не поддерживается: $arch" ;;
esac

stage=$(mktemp -d /tmp/broray-package.XXXXXXXX) || fail 'Не удалось создать временный каталог.'
trap 'rm -f "$stage/archive.tar.gz" "$stage/broray"; rmdir "$stage"' 0
archive=$stage/archive.tar.gz
binary=$stage/broray
curl -q -fL --proto '=https' --proto-redir '=https' --connect-timeout 15 --max-time 300 --max-filesize "$size" -o "$archive" "$url"
actual=$(sha256sum "$archive")
[ "${actual%% *}" = "$sha" ] || fail 'SHA-256 архива не совпал.'
[ "$(wc -c < "$archive")" -eq "$size" ] || fail 'Размер архива не совпал.'
tar -xOzf "$archive" "$entry" > "$binary" || fail 'Не удалось извлечь установщик из архива.'
[ "$(wc -c < "$binary")" -eq "$unpacked" ] || fail 'Размер установщика не совпал.'
chmod 700 "$binary"
unset BRORAY_ROOT BRORAY_OPT
reported=$("$binary" version) || fail 'Установщик не сообщил свою версию.'
if [ "$reported" != "BROray $release_version" ] && [ "$reported" != "BROray $release_series" ]; then
    fail 'Версия установщика не совпала с выпуском.'
fi
"$binary" installer install --first-install
printf 'BROray %s\n' "$release_version"
`
