package xray

// Evidence registry (owner decision 2026-11-02, CODEX-REPORT-DEV5): for every
// official archive (tag + architecture + archive SHA-256) BROray shows which
// checks were actually done with this build, where and when — not one green
// word. «Совместима с BROray» needs all three levels on that architecture:
//
//	config     — BROray-generated configurations of every supported protocol
//	             pass `xray run -test` (off-router: VM / host)
//	router-tcp — on a Keenetic: Xray starts, SOCKS answers, a real server
//	             connects and HTTPS goes through it
//	router-udp — on a Keenetic: UDP through ProxyN → Xray → server
//	             (`broray diag udp` = UDP_VIA_BROray)
//
// An extra, informational level does not count towards «Совместима»:
//
//	emulator   — QEMU system VM of that architecture: the official binary
//	             runs, BROray configurations of 11 protocol/transport variants
//	             carry UDP (and their TCP handshakes) client Xray → server Xray
//
// A version with some checks but not all is «Проверка совместимости не
// завершена» and still needs the owner's consent like an untested one.

// Check is one recorded check of one archive.
type Check struct {
	XrayTag       string
	Architecture  string // Xray asset arch: arm64-v8a, arm32-v7a, mips32le, mips32, 64
	ArchiveSHA256 string
	Level         string // config | router-tcp | router-udp
	Date          string // YYYY-MM-DD
	Where         string
	Details       string
	Receipt       string // artifact name + SHA-256 prefix
}

var levelOrder = []string{"config", "router-tcp", "router-udp"}

var extraLevels = []string{"emulator"}

var levelTitle = map[string]string{
	"emulator":   "Трафик TCP/UDP через Xray в эмуляторе этой архитектуры",
	"config":     "Конфигурации BROray проходят xray run -test",
	"router-tcp": "Подключение к серверу и HTTPS через SOCKS на Keenetic",
	"router-udp": "UDP через ProxyN на Keenetic",
}

const (
	arm64Xray261031 = "1811c8f1da0c3b6dc707ebaf1c8390e987ba39859a8f0a7aadd8526157f2ec8d"
	arm64Xray261021 = "d1304946c486d98e6b886e9b931eaaaebb23835b30e217638037b7f2721d4e9c"
	vmFormats       = "VLESS REALITY XHTTP, VMess RAW, Trojan TLS, Shadowsocks, Hysteria2"
	vmWhere         = "ARM64 VM (QEMU, без сети)"
	vmReceipt       = "XRAY-COMPAT-VM-MATRIX.json, SHA-256 70f83c92…"
	emuDetails      = "11 вариантов (VLESS RAW/Vision/WS/gRPC/HTTPUpgrade/XHTTP/mKCP, VMess, Trojan TLS, Shadowsocks, Hysteria2): UDP через клиентский и серверный Xray"
	emuReceipt      = "CODEX-REPORT-EMU.md (E2)"
)

// Evidence is compiled into the build. Sources: CODEX-REPORT-DEV5 (KN-1811,
// KeeneticOS 5.02.B.2.0-2, aarch64) and its receipts.
var Evidence = []Check{
	{"v26.10.31", "arm64-v8a", arm64Xray261031, "config", "2026-11-02", vmWhere, vmFormats, vmReceipt},
	{"v26.10.31", "arm64-v8a", arm64Xray261031, "router-tcp", "2026-11-02", "Keenetic KN-1811",
		"VLESS REALITY XHTTP: сервер подключён, HTTPS через SOCKS проходит", "CODEX-REPORT-DEV3/DEV5"},
	{"v26.10.31", "arm64-v8a", arm64Xray261031, "router-udp", "2026-11-02", "Keenetic KN-1811",
		"STUN через маршрут BROray и ProxyN: UDP_VIA_BROray", "U3-DEV5-UDP-REDACTED.json, SHA-256 d47e3903…"},

	{"v26.10.21", "arm64-v8a", arm64Xray261021, "config", "2026-11-02", vmWhere, vmFormats, vmReceipt},
	{"v26.10.21", "arm64-v8a", arm64Xray261021, "router-tcp", "2026-11-02", "Keenetic KN-1811",
		"VLESS REALITY XHTTP: установка штатной кнопкой, сервер подключён, HTTPS через SOCKS проходит", "CODEX-REPORT-DEV5"},
	{"v26.10.21", "arm64-v8a", arm64Xray261021, "router-udp", "2026-11-05", "Keenetic KN-1811",
		"Официальный архив: штатная установка, UDP через Proxy0 — UDP_VIA_BROray; после проверки возвращена 26.10.31",
		"dev7/artifacts/stage-d/xray261021-router-udp-evidence.json, SHA-256 4d87aafa47906c46…"},

	{"v26.9.30", "arm64-v8a", "9886f077f9fd8e6713b84c377c1c7db4e53b9bfa8c276a5bd12561139522b473", "config", "2026-11-02", vmWhere, vmFormats, vmReceipt},
	{"v26.9.9", "arm64-v8a", "3e38d72dfc5eb65c91df0e5583e9b6676c32232041da47de6ae73946b526d66c", "config", "2026-11-02", vmWhere, vmFormats, vmReceipt},
	{"v26.7.28", "arm64-v8a", "f5698bb218ada3b4022db26fafc39601c5f53b46b19eb76c9616325985807501", "config", "2026-11-02", vmWhere, vmFormats, vmReceipt},
	{"v26.7.11", "arm64-v8a", "89cfe01674d7c9f6847b7dd9389537be9acb3b9dc3c6cb9fdeba87a3e4e57fc1", "config", "2026-11-02", vmWhere, vmFormats, vmReceipt},
	{"v26.6.27", "arm64-v8a", "13a251379bea366c2cf10363ad71e75734193d401f26f518bf0c25e5c8f8c931", "config", "2026-11-02", vmWhere, vmFormats, vmReceipt},

	// CODEX-REPORT-EMU (2026-11-02): QEMU system, no network; Keenetic not involved.
	{"v26.10.31", "arm32-v7a", "2387f55239ee7799fd67d5382e79f589c1da07a97d993e86b058f2290b8e456b", "emulator", "2026-11-02", "QEMU ARM virt, Linux 6.12", emuDetails, emuReceipt},
	{"v26.10.21", "arm32-v7a", "89978508fa2ebb4a1e661f2c6ed51d241c4091b3cd17f4c65fdf20baae7dedac", "emulator", "2026-11-02", "QEMU ARM virt, Linux 6.12", emuDetails, emuReceipt},
	{"v26.9.30", "arm32-v7a", "0b9719471c7c69752857714e9711d4da57cf38a6beb75dcddfb21425f7919908", "emulator", "2026-11-02", "QEMU ARM virt, Linux 6.12", emuDetails, emuReceipt},
	{"v26.9.9", "arm32-v7a", "5b2a9e2767c0197f7bd41c2db133f91440e845771e621178e0fd2b8520228e5f", "emulator", "2026-11-02", "QEMU ARM virt, Linux 6.12", emuDetails, emuReceipt},
	{"v26.7.28", "arm32-v7a", "a23cae6991ea1224e4e3381cae4ada8c3d5afc438a851ab0b2eda2f391dc4072", "emulator", "2026-11-02", "QEMU ARM virt, Linux 6.12", emuDetails, emuReceipt},
	{"v26.7.11", "arm32-v7a", "c623b8d08d02d7a20be697619fde11c6745efb08ac5cb332ab0ff15fe567aad6", "emulator", "2026-11-02", "QEMU ARM virt, Linux 6.12", emuDetails, emuReceipt},
	{"v26.6.27", "arm32-v7a", "c77245cf9bfd6b8ae0fa8247cd65a3478f1951e9712fcde0f0dda4a31e2ac26c", "emulator", "2026-11-02", "QEMU ARM virt, Linux 6.12", emuDetails, emuReceipt},
	{"v26.10.31", "mips32le", "7f3e56ea73f2e2ce12556ba3720e8894abd89c17aec5c70ea70ae6f48552cfb5", "emulator", "2026-11-02", "QEMU MIPS Malta (LE), Linux 6.1", emuDetails, emuReceipt},
	{"v26.10.21", "mips32le", "1e9953aecee4024d9da6427a42929817b53d1660f5122ec6b22629e11cab4012", "emulator", "2026-11-02", "QEMU MIPS Malta (LE), Linux 6.1", emuDetails, emuReceipt},
	{"v26.9.30", "mips32le", "c41a4d7b7fafbf3ea345eef0b51c3dd68f4894e15069c19f3432670b13cbc160", "emulator", "2026-11-02", "QEMU MIPS Malta (LE), Linux 6.1", emuDetails, emuReceipt},
	{"v26.9.9", "mips32le", "e572d2cdd819318383460443140898e6117e8e0da5f0c359b25f6c890b8d81a2", "emulator", "2026-11-02", "QEMU MIPS Malta (LE), Linux 6.1", emuDetails, emuReceipt},
	{"v26.7.28", "mips32le", "4779e1afba7dea12c64a72380f1d9737a12359f354014625a7f9d96f8d31e3fa", "emulator", "2026-11-02", "QEMU MIPS Malta (LE), Linux 6.1", emuDetails, emuReceipt},
	{"v26.7.11", "mips32le", "39564b010eaae23af8c84e8ee4f0911c23301dfdd07ec75408809dbe1e71960f", "emulator", "2026-11-02", "QEMU MIPS Malta (LE), Linux 6.1", emuDetails, emuReceipt},
	{"v26.6.27", "mips32le", "48569b62fdda2a03d2d3890968da982cb73fe05001514470388eb63d5f0218de", "emulator", "2026-11-02", "QEMU MIPS Malta (LE), Linux 6.1", emuDetails, emuReceipt},
	{"v26.10.31", "mips32", "45a84c48da48e7f7663c4c0030977ba0d56ba24a5c855de6a1d898be904e16db", "emulator", "2026-11-02", "QEMU MIPS Malta (BE), Linux 4.19", emuDetails, emuReceipt},
	{"v26.10.21", "mips32", "3de6ee6a5142fb1372334ad1411f61f1b0a3f9d622a748a6080a29dc007ba207", "emulator", "2026-11-02", "QEMU MIPS Malta (BE), Linux 4.19", emuDetails, emuReceipt},
	{"v26.9.30", "mips32", "8e753eaad5147115a327c7084a2f26aee42149c0f16a962a04ea103a055db555", "emulator", "2026-11-02", "QEMU MIPS Malta (BE), Linux 4.19", emuDetails, emuReceipt},
	{"v26.9.9", "mips32", "4b9838c570f283d7104e0c775df3562a4f12f33c06e6c77ddcf2319845bf47ba", "emulator", "2026-11-02", "QEMU MIPS Malta (BE), Linux 4.19", emuDetails, emuReceipt},
	{"v26.7.28", "mips32", "aec600118fd1e7ee42e8d5e8d5c82cc5e8139e82ff1da029e9b81b7170fc028c", "emulator", "2026-11-02", "QEMU MIPS Malta (BE), Linux 4.19", emuDetails, emuReceipt},
	{"v26.7.11", "mips32", "ab444eb02d95948ef11dd8369ac04197c73f9d5d90208d7b015a1a2494c31474", "emulator", "2026-11-02", "QEMU MIPS Malta (BE), Linux 4.19", emuDetails, emuReceipt},
	{"v26.6.27", "mips32", "9f78ed13743630a2a69882715a07cfa50623e58feb48192b1d3858982bffbfa7", "emulator", "2026-11-02", "QEMU MIPS Malta (BE), Linux 4.19", emuDetails, emuReceipt},
	{"v26.10.31", "arm64-v8a", arm64Xray261031, "emulator", "2026-11-02", "QEMU aarch64, Linux 6.12", emuDetails, "CODEX-REPORT-EMU.md (E1)"},
}

// checksFor returns the recorded checks of one archive: one entry per
// required level (passed or not-run) in levelOrder, then the passed extra
// levels; the count is of required levels passed.
func checksFor(tag, arch, sum string) ([]map[string]any, int) {
	byLevel := map[string]Check{}
	for _, c := range Evidence {
		if c.XrayTag == tag && c.Architecture == arch && c.ArchiveSHA256 == sum {
			byLevel[c.Level] = c
		}
	}
	out := make([]map[string]any, 0, len(levelOrder))
	passed := 0
	for _, l := range levelOrder {
		m := map[string]any{"level": l, "title": levelTitle[l]}
		if c, ok := byLevel[l]; ok {
			passed++
			m["state"], m["date"], m["where"], m["details"], m["receipt"] = "passed", c.Date, c.Where, c.Details, c.Receipt
		} else {
			m["state"] = "not-run"
		}
		out = append(out, m)
	}
	for _, l := range extraLevels {
		if c, ok := byLevel[l]; ok {
			out = append(out, map[string]any{"level": l, "title": levelTitle[l], "state": "passed", "date": c.Date,
				"where": c.Where, "details": c.Details, "receipt": c.Receipt})
		}
	}
	return out, passed
}

// hasEvidence: any recorded check (required or extra) for this archive.
func hasEvidence(tag, arch, sum string) bool {
	for _, c := range Evidence {
		if c.XrayTag == tag && c.Architecture == arch && c.ArchiveSHA256 == sum {
			return true
		}
	}
	return false
}

// registryFromEvidence: an archive with all levels passed is "compatible".
func registryFromEvidence() []Compatibility {
	type k struct{ tag, arch, sum string }
	seen := map[k]bool{}
	var out []Compatibility
	for _, c := range Evidence {
		key := k{c.XrayTag, c.Architecture, c.ArchiveSHA256}
		if seen[key] {
			continue
		}
		seen[key] = true
		checks, passed := checksFor(c.XrayTag, c.Architecture, c.ArchiveSHA256)
		if passed != len(levelOrder) {
			continue
		}
		date := ""
		for _, ch := range checks {
			if d, _ := ch["date"].(string); d > date {
				date = d
			}
		}
		out = append(out, Compatibility{CandidateID: "evidence", Architecture: c.Architecture, XrayTag: c.XrayTag,
			ArchiveSHA256: c.ArchiveSHA256, TestedAt: date + "T00:00:00Z", Status: "compatible",
			Evidence: "Проверены конфигурации, TCP и UDP на Keenetic"})
	}
	return out
}
