package update

import (
	"regexp"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
)

// versionRe accepts BROray versions: 3.5.0, 3.5.0-dev5, 3.5.0-rc1,
// 3.2.0-r12 (3.x), 3.5.1-test.2. It is also the allow-list for directory
// names under versions/, so it must not admit "/" or "..".
var versionRe = regexp.MustCompile(`^[0-9]{1,4}\.[0-9]{1,4}\.[0-9]{1,4}(-[0-9A-Za-z]+(\.[0-9A-Za-z]+)*)?$`)

// ValidVersion reports whether v is a well-formed BROray version.
func ValidVersion(v string) bool { return len(v) <= 64 && versionRe.MatchString(v) }

// CompareVersions orders versions like semver (a pre-release sorts before
// its release) with natural ordering inside pre-release parts, so dev9 <
// dev10 < rc1. Returns -1, 0 or 1.
func CompareVersions(a, b string) int {
	ac, ap, _ := strings.Cut(a, "-")
	bc, bp, _ := strings.Cut(b, "-")
	as, bs := strings.Split(ac, "."), strings.Split(bc, ".")
	for i := 0; i < 3; i++ {
		if c := cmpInt(part(as, i), part(bs, i)); c != 0 {
			return c
		}
	}
	switch {
	case ap == "" && bp == "":
		return 0
	case ap == "":
		return 1
	case bp == "":
		return -1
	}
	return naturalCompare(ap, bp)
}

func part(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return "0"
}

func cmpInt(a, b string) int {
	x, _ := strconv.Atoi(a)
	y, _ := strconv.Atoi(b)
	switch {
	case x < y:
		return -1
	case x > y:
		return 1
	}
	return 0
}

// naturalCompare compares strings chunk by chunk: digit runs numerically,
// everything else lexically.
func naturalCompare(a, b string) int {
	for a != "" && b != "" {
		ca, ra := chunk(a)
		cb, rb := chunk(b)
		da, db := isDigit(ca[0]), isDigit(cb[0])
		var c int
		switch {
		case da && db:
			c = cmpInt(ca, cb)
		case da:
			c = -1 // numeric identifiers sort before alphanumeric (semver)
		case db:
			c = 1
		default:
			c = strings.Compare(ca, cb)
		}
		if c != 0 {
			return c
		}
		a, b = ra, rb
	}
	switch {
	case a == "" && b == "":
		return 0
	case a == "":
		return -1
	}
	return 1
}

func chunk(s string) (string, string) {
	d := isDigit(s[0])
	i := 1
	for i < len(s) && isDigit(s[i]) == d {
		i++
	}
	return s[:i], s[i:]
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// Platform is the manifest installer key of this binary: GOARCH, plus the
// GOARM / GOMIPS variant the binary was built with ("arm/7",
// "mipsle/softfloat"). The variant is read from the build info so it is
// exactly what the release build used; tools/release reads the same field
// from the binaries in the release tarballs.
func Platform() string {
	settings := map[string]string{}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			settings[s.Key] = s.Value
		}
	}
	return PlatformKey(runtime.GOARCH, settings)
}

// PlatformKey builds the key from GOARCH and build settings.
func PlatformKey(goarch string, settings map[string]string) string {
	variant := ""
	switch goarch {
	case "arm":
		variant = settings["GOARM"]
		if variant == "" {
			variant = "7" // the Go default when cross-compiling for linux/arm
		}
		variant, _, _ = strings.Cut(variant, ",") // "7,softfloat" → "7"
	case "mips", "mipsle":
		variant = settings["GOMIPS"]
		if variant == "" {
			variant = "hardfloat"
		}
	}
	if variant == "" {
		return goarch
	}
	return goarch + "/" + variant
}

// TarballArch maps the release tarball architecture names to GOARCH (the
// Entware names of BROray release archives).
var TarballArch = map[string]string{"aarch64": "arm64", "armv7": "arm", "mipsel": "mipsle", "mips": "mips", "x86_64": "amd64"}
