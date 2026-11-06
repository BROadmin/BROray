// release is the developer-side tooling of the BROray release channel
// (docs/RELEASE-CHANNEL.md). It is not part of the router binary.
//
//	go run ./tools/release keygen -o <dir> [-name stable]
//	go run ./tools/release channel -channel test -version 3.5.0-dev6 -o manifest.json [flags] <tarball|binary>...
//	go run ./tools/release sign -k <dir>/stable.key manifest.json      → manifest.json.sig + .minisig
//	go run ./tools/release verify -p <public key> manifest.json
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/update"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "keygen":
		err = keygen(os.Args[2:])
	case "sign":
		err = sign(os.Args[2:])
	case "verify":
		err = verify(os.Args[2:])
	case "channel":
		err = channel(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "release: "+err.Error())
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: release keygen|sign|verify|channel (see docs/RELEASE-CHANNEL.md)")
	os.Exit(2)
}

func keygen(args []string) error {
	fs := flag.NewFlagSet("keygen", flag.ExitOnError)
	dir := fs.String("o", "", "output directory (created 0700)")
	name := fs.String("name", "broray-release", "file name prefix")
	_ = fs.Parse(args)
	if *dir == "" {
		return errors.New("-o is required")
	}
	if err := os.MkdirAll(*dir, 0o700); err != nil {
		return err
	}
	keyPath := filepath.Join(*dir, *name+".key")
	if _, err := os.Stat(keyPath); err == nil {
		return fmt.Errorf("%s exists; refusing to overwrite a key", keyPath)
	}
	sk, err := update.GenerateKey()
	if err != nil {
		return err
	}
	// O_EXCL: never replace an existing secret key.
	f, err := os.OpenFile(keyPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(sk.EncodeFile()); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	pk := sk.Public()
	pub := "untrusted comment: minisign public key " + pk.IDString() + "\n" + pk.Encode() + "\n"
	if err := os.WriteFile(filepath.Join(*dir, *name+".pub"), []byte(pub), 0o644); err != nil {
		return err
	}
	fmt.Printf("secret key: %s (0600, keep offline)\npublic key: %s\nkey id:     %s\n", keyPath, pk.Encode(), pk.IDString())
	return nil
}

func sign(args []string) error {
	fs := flag.NewFlagSet("sign", flag.ExitOnError)
	keyPath := fs.String("k", "", "secret key file (minisign, unencrypted)")
	comment := fs.String("c", "", "trusted comment (default: timestamp and file name)")
	_ = fs.Parse(args)
	if *keyPath == "" || fs.NArg() != 1 {
		return errors.New("usage: release sign -k <key> <file>")
	}
	st, err := os.Stat(*keyPath)
	if err != nil {
		return err
	}
	if st.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s is readable by others (mode %o); chmod 600 it", *keyPath, st.Mode().Perm())
	}
	kb, err := os.ReadFile(*keyPath)
	if err != nil {
		return err
	}
	sk, err := update.ParseSecretKeyFile(kb)
	if err != nil {
		return err
	}
	file := fs.Arg(0)
	msg, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	if _, err := update.ParseManifest(msg); err != nil && strings.HasSuffix(file, ".json") {
		return fmt.Errorf("%s is not a valid channel manifest: %v", file, err)
	}
	tc := *comment
	if tc == "" {
		tc = fmt.Sprintf("timestamp:%d\tfile:%s\thashed", time.Now().Unix(), filepath.Base(file))
	}
	sig := sk.Sign(msg, tc, "signature from BROray release key "+sk.Public().IDString())
	// The updater fetches .sig; the official minisign CLI without -x looks
	// for .minisig. Keep the two byte-identical for both consumers.
	if err := os.WriteFile(file+".minisig", sig, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(file+".sig", sig, 0o644); err != nil {
		return err
	}
	fmt.Printf("%s.sig and %s.minisig (key %s)\n", file, file, sk.Public().IDString())
	return nil
}

func verify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	pub := fs.String("p", "", "public key (base64 line or .pub file)")
	_ = fs.Parse(args)
	if *pub == "" || fs.NArg() != 1 {
		return errors.New("usage: release verify -p <public key> <file>")
	}
	ps := *pub
	if b, err := os.ReadFile(ps); err == nil {
		ps = string(b)
	}
	pk, err := update.ParsePublicKey(ps)
	if err != nil {
		return err
	}
	msg, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	sb, err := os.ReadFile(fs.Arg(0) + ".sig")
	if err != nil {
		return err
	}
	sig, err := update.ParseSignature(sb)
	if err != nil {
		return err
	}
	if err := sig.Verify(pk, msg); err != nil {
		return err
	}
	fmt.Printf("signature OK (key %s); trusted comment: %s\n", pk.IDString(), sig.TrustedComment)
	return nil
}

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(s string) error { *m = append(*m, s); return nil }

func channel(args []string) error {
	fs := flag.NewFlagSet("channel", flag.ExitOnError)
	name := fs.String("channel", "", "channel name (stable, beta, test)")
	version := fs.String("version", "", "release version of the artefacts")
	out := fs.String("o", "manifest.json", "output manifest")
	merge := fs.String("merge", "", "existing manifest whose other releases and revocations are kept")
	base := fs.String("base-url", "", "URL prefix of the artefacts (empty: relative to the manifest)")
	notes := fs.String("notes", "", "release notes")
	minSrc := fs.String("min-source", "", "minimumSourceVersion")
	expires := fs.Duration("expires", 30*24*time.Hour, "validity of the manifest")
	var revoke multi
	fs.Var(&revoke, "revoke", "revoked version (repeatable)")
	_ = fs.Parse(args)
	if !update.ValidChannel(*name) || !update.ValidVersion(*version) || fs.NArg() == 0 {
		return errors.New("usage: release channel -channel <c> -version <v> [-o manifest.json] <tarball|binary>...")
	}
	m := update.Manifest{SchemaVersion: update.SchemaVersion, Channel: *name, Revoked: []string{}}
	if *merge != "" {
		b, err := os.ReadFile(*merge)
		if err != nil {
			return err
		}
		old, err := update.ParseManifest(b)
		if err != nil {
			return fmt.Errorf("%s: %v", *merge, err)
		}
		if old.Channel != *name {
			return fmt.Errorf("%s is for channel %s", *merge, old.Channel)
		}
		for _, r := range old.Releases {
			if r.Version != *version {
				m.Releases = append(m.Releases, r)
			}
		}
		m.Revoked = append(m.Revoked, old.Revoked...)
		m.Rollout = old.Rollout
	}
	rel := update.Release{Version: *version, Installer: map[string]update.Artifact{}, Notes: *notes, MinimumSourceVersion: *minSrc}
	for _, file := range fs.Args() {
		key, art, err := artefact(file)
		if err != nil {
			return fmt.Errorf("%s: %v", file, err)
		}
		if _, dup := rel.Installer[key]; dup {
			return fmt.Errorf("%s: second artefact for %s", file, key)
		}
		art.URL = filepath.Base(file)
		if *base != "" {
			art.URL = strings.TrimRight(*base, "/") + "/" + art.URL
		}
		rel.Installer[key] = art
		fmt.Fprintf(os.Stderr, "%-18s %s (%d bytes, sha256 %s)\n", key, art.URL, art.Size, art.SHA256[:16])
	}
	m.Releases = append(m.Releases, rel)
	sort.Slice(m.Releases, func(i, j int) bool { return update.CompareVersions(m.Releases[i].Version, m.Releases[j].Version) > 0 })
	for _, v := range revoke {
		m.Revoked = append(m.Revoked, v)
	}
	now := time.Now().UTC().Truncate(time.Second)
	m.IssuedAt, m.ExpiresAt = now, now.Add(*expires)
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if _, err := update.ParseManifest(b); err != nil {
		return fmt.Errorf("generated manifest invalid: %v", err)
	}
	if err := os.WriteFile(*out, b, 0o644); err != nil {
		return err
	}
	fmt.Printf("%s: channel %s, %d release(s), expires %s — now sign it: release sign -k <key> %s\n", *out, *name, len(m.Releases), m.ExpiresAt.Format(time.RFC3339), *out)
	return nil
}

// artefact describes a release tarball (*.tar.gz with a member named
// broray) or a bare binary. The platform key is read from the Go build
// info of the binary itself, so it matches update.Platform() on the router.
func artefact(file string) (string, update.Artifact, error) {
	var a update.Artifact
	b, err := os.ReadFile(file)
	if err != nil {
		return "", a, err
	}
	sum := sha256.Sum256(b)
	a.Size, a.SHA256 = int64(len(b)), hex.EncodeToString(sum[:])
	bin := b
	if strings.HasSuffix(file, ".tar.gz") || strings.HasSuffix(file, ".tgz") {
		a.Format = "tar.gz"
		gz, err := gzip.NewReader(bytes.NewReader(b))
		if err != nil {
			return "", a, err
		}
		tr := tar.NewReader(gz)
		bin = nil
		for {
			hdr, err := tr.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return "", a, err
			}
			if hdr.Typeflag == tar.TypeReg && path.Base(hdr.Name) == "broray" {
				if bin != nil {
					return "", a, errors.New("more than one member named broray")
				}
				a.Entry = path.Clean(hdr.Name)
				a.UnpackedSize = hdr.Size
				if bin, err = io.ReadAll(tr); err != nil {
					return "", a, err
				}
			}
		}
		if bin == nil {
			return "", a, errors.New("no member named broray")
		}
	}
	bi, err := buildinfo.Read(bytes.NewReader(bin))
	if err != nil {
		return "", a, fmt.Errorf("not a Go binary: %v", err)
	}
	settings := map[string]string{}
	for _, s := range bi.Settings {
		settings[s.Key] = s.Value
	}
	if settings["GOOS"] != "linux" {
		return "", a, fmt.Errorf("binary is for %s, not linux", settings["GOOS"])
	}
	return update.PlatformKey(settings["GOARCH"], settings), a, nil
}
