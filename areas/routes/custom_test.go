package routes

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/keenetic"
)

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func previewBody(name string, files ...[2]string) map[string]any {
	fl := []any{}
	for _, f := range files {
		fl = append(fl, map[string]any{"name": f[0], "contentBase64": b64(f[1])})
	}
	return map[string]any{"name": name, "files": fl}
}

const discordBat = "@echo off\r\n:: Discord\r\nroute add 10.0.0.0 mask 255.255.255.0 0.0.0.0\r\nroute add 10.0.1.0 mask 255.255.255.0 0.0.0.0 & rem x\r\n" +
	"route add 010.000.001.000 mask 255.255.255.0 0.0.0.0\r\nroute add 1.2.3.4 mask 255.255.255.255 0.0.0.0\r\n"

func TestCustomPreviewValidation(t *testing.T) {
	env := newEnv(t)
	long := strings.Repeat("я", 81)
	cases := []struct {
		body          any
		status        int
		code, message string
	}{
		{"", 400, "REQUEST_BODY_REQUIRED", "Тело запроса отсутствует."},
		{"[1]", 400, "REQUEST_JSON_INVALID", "Запрос не является корректным JSON."},
		{"{bad", 400, "REQUEST_JSON_INVALID", "Запрос не является корректным JSON."},
		{map[string]any{"name": "x"}, 400, "REQUEST_INVALID", "Некорректный запрос загрузки BAT-файлов."},
		{map[string]any{"name": "x", "files": []any{}}, 400, "REQUEST_INVALID", "Некорректный запрос загрузки BAT-файлов."},
		{previewBody("  ", [2]string{"a.bat", discordBat}), 400, "NAME_INVALID", "Название набора должно содержать от 1 до 80 печатных символов."},
		{previewBody(long, [2]string{"a.bat", discordBat}), 400, "NAME_INVALID", "Название набора должно содержать от 1 до 80 печатных символов."},
		{previewBody("a\tb", [2]string{"a.bat", discordBat}), 400, "NAME_INVALID", "Название набора должно содержать от 1 до 80 печатных символов."},
		{previewBody("a\nb", [2]string{"a.bat", discordBat}), 400, "CUSTOM_ROUTES_OPERATION_FAILED", "Операция с пользовательскими маршрутами завершилась ошибкой."},
		{previewBody("x", [2]string{"dir/a.bat", discordBat}), 400, "FILENAME_INVALID", "Имя файла не должно содержать путь."},
		{previewBody("x", [2]string{strings.Repeat("a", 157) + ".bat", discordBat}), 400, "FILENAME_INVALID", "Некорректное имя загружаемого файла."},
		{previewBody("x", [2]string{"a.txt", discordBat}), 400, "FILE_EXTENSION_INVALID", "Разрешены только файлы с расширением .bat."},
		{map[string]any{"name": "x", "files": []any{map[string]any{"name": "a.bat", "contentBase64": "!!!"}}}, 400, "FILE_DECODE_FAILED", "Не удалось декодировать файл a.bat."},
		{previewBody("x", [2]string{"a.BAT", ""}), 400, "FILE_EMPTY", "Файл a.BAT пуст."},
		{previewBody("x", [2]string{"a.bat", "route add\x00"}), 400, "FILE_NOT_TEXT", "Файл a.bat содержит нулевые байты и не является текстовым BAT-файлом."},
		{previewBody("x", [2]string{"a.bat", "rem only\r\n"}), 400, "FILE_REJECTED", "Файл a.bat не прошёл безопасную проверку. В файле не найдено ни одной допустимой команды route add."},
		{previewBody("x", [2]string{"a.bat", "route add 0.0.0.0 mask 0.0.0.0 0.0.0.0\nfoo\n"}), 400, "FILE_REJECTED", "Файл a.bat не прошёл безопасную проверку. Строка 1: маршрут по умолчанию 0.0.0.0/0 запрещён"},
	}
	for i, tc := range cases {
		r := env.call("POST", "/api/routes/custom-preview.cgi", tc.body)
		if r.status != tc.status || r.code() != tc.code || r.message() != tc.message {
			t.Errorf("case %d: got %d %s", i, r.status, r.raw)
		}
	}
	// details carry the remaining error lines
	r := env.call("POST", "/api/routes/custom-preview.cgi", previewBody("x", [2]string{"a.bat", "foo\nbar\n"}))
	if r.details() != "Строка 2: "+msgOnlyRouteAdd {
		t.Fatal(r.details())
	}
	// 17 files
	var files [][2]string
	for i := 0; i < 17; i++ {
		files = append(files, [2]string{fmt.Sprintf("f%d.bat", i), discordBat})
	}
	r = env.call("POST", "/api/routes/custom-preview.cgi", previewBody("x", files...))
	env.wantErr(r, 400, "TOO_MANY_FILES")
	if r.message() != "Разрешено не более 16 BAT-файлов в одном наборе." {
		t.Fatal(r.message())
	}
	// 2 MiB total
	big := "route add 1.2.3.0 mask 255.255.255.0 0.0.0.0\n" + strings.Repeat("rem padding padding padding padding padding padding padding\n", 20000)
	r = env.call("POST", "/api/routes/custom-preview.cgi", previewBody("x", [2]string{"a.bat", big}, [2]string{"b.bat", big}))
	env.wantErr(r, 400, "SOURCE_TOO_LARGE")
	// > 4 MiB request
	r = env.call("POST", "/api/routes/custom-preview.cgi", strings.Repeat(" ", customBodyMax+1))
	env.wantErr(r, 413, "REQUEST_TOO_LARGE")
	// > 100000 route lines
	var sb strings.Builder
	for i := 0; i < 100001; i++ {
		fmt.Fprintf(&sb, "route add 10.%d.%d.0 mask 255.255.255.0 0.0.0.0\n", (i/256)%256, i%256)
	}
	if sb.Len() < userMaxBytes {
		r = env.call("POST", "/api/routes/custom-preview.cgi", previewBody("x", [2]string{"a.bat", sb.String()}))
		env.wantErr(r, 400, "TOO_MANY_ROUTES")
	}
}

func TestCustomPreviewReportAndWarning(t *testing.T) {
	env := newEnv(t)
	bat := discordBat + "route add 10.0.0.0 mask 255.255.255.0 0.0.0.0\r\nroute add 64.0.0.0 mask 255.0.0.0 0.0.0.0\r\n"
	rep := env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody(" Discord ", [2]string{"discord.bat", bat})))
	want := map[string]float64{"sourceRouteLineCount": 6, "canonicalRouteCount": 4, "exportRouteCount": 3, "normalizedNetworkCount": 1,
		"duplicateCount": 2, "broadRouteCount": 1, "fileCount": 1}
	for k, v := range want {
		if rep[k] != v {
			t.Errorf("%s = %v want %v", k, rep[k], v)
		}
	}
	if rep["name"] != "Discord" || rep["ready"] != true || !isHex64(rep["token"].(string)) ||
		rep["warning"] != "Набор содержит очень широкие сети /7–/8. Через Xray может направляться значительная часть интернет-трафика." {
		t.Fatal(rep)
	}
	opt, _ := os.ReadFile(filepath.Join(env.e.previewsDir(), rep["token"].(string), "optimized.txt"))
	if string(opt) != "1.2.3.4/32\n10.0.0.0/23\n64.0.0.0/8\n" || sha256Hex(opt) != rep["exportSha256"] {
		t.Fatalf("optimized: %q", opt)
	}
	sf := rep["sourceFiles"].([]any)[0].(map[string]any)
	if sf["storedName"] != "01.bat" || sf["originalName"] != "discord.bat" || sf["routeLineCount"].(float64) != 6 {
		t.Fatal(sf)
	}
}

func TestCustomLifecycle(t *testing.T) {
	env := newEnv(t)
	rep := env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("Discord", [2]string{"discord.bat", discordBat})))
	token := rep["token"].(string)
	// commit errors
	env.wantErr(env.call("POST", "/api/routes/custom-commit.cgi", map[string]any{"token": 1, "name": "x"}), 400, "REQUEST_INVALID")
	env.wantErr(env.call("POST", "/api/routes/custom-commit.cgi", map[string]any{"token": "abc", "name": "x", "bundleId": nil}), 400, "TOKEN_INVALID")
	env.wantErr(env.call("POST", "/api/routes/custom-commit.cgi", map[string]any{"token": token, "name": "", "bundleId": nil}), 400, "NAME_INVALID")
	env.wantErr(env.call("POST", "/api/routes/custom-commit.cgi", map[string]any{"token": strings.Repeat("a", 64), "name": "x", "bundleId": nil}), 400, "PREVIEW_INVALID")
	env.wantErr(env.call("POST", "/api/routes/custom-commit.cgi", map[string]any{"token": token, "name": "x", "bundleId": "user-nope"}), 404, "BUNDLE_NOT_FOUND")
	env.wantErr(env.call("POST", "/api/routes/custom-commit.cgi", map[string]any{"token": token, "name": "x", "bundleId": "bad"}), 400, "BUNDLE_INVALID")
	nowFn = func() time.Time { return time.Now().Add(1801 * time.Second) }
	env.wantErr(env.call("POST", "/api/routes/custom-commit.cgi", map[string]any{"token": token, "name": "x", "bundleId": nil}), 400, "PREVIEW_EXPIRED")
	nowFn = time.Now
	res := env.mustOK(env.call("POST", "/api/routes/custom-commit.cgi", map[string]any{"token": token, "name": "Мой Discord", "bundleId": nil}))
	id := res["bundleId"].(string)
	if id != "user-"+token[:16] || res["replaced"] != false || res["exportRouteCount"].(float64) != 2 || res["readyForExport"] != true {
		t.Fatal(res)
	}
	// single use
	env.wantErr(env.call("POST", "/api/routes/custom-commit.cgi", map[string]any{"token": token, "name": "x", "bundleId": nil}), 400, "PREVIEW_INVALID")
	// summary
	sum := env.mustOK(env.get("/api/routes/custom-summary.cgi"))
	b := sum["bundles"].([]any)[0].(map[string]any)
	if sum["scope"] != "custom" || b["id"] != id || b["metadata"].(map[string]any)["name"] != "Мой Discord" || b["keeneticUpdateRequired"] != true {
		t.Fatal(b["metadata"])
	}
	// list / status
	list := env.mustOK(env.get("/api/routes/custom-list.cgi"))
	if len(list["bundles"].([]any)) != 1 {
		t.Fatal(list)
	}
	env.wantErr(env.get("/api/routes/custom-status.cgi?bundleId=user-zzz"), 404, "ROUTES_BUNDLE_NOT_FOUND")
	env.wantErr(env.get("/api/routes/custom-status.cgi"), 400, "ROUTES_BUNDLE_REQUIRED")
	env.wantErr(env.get("/api/routes/custom-status.cgi?bundleId=telegram"), 400, "ROUTES_BUNDLE_INVALID")
	stat := env.mustOK(env.get("/api/routes/custom-status.cgi?bundleId=" + id))
	if stat["routerPresence"].(map[string]any)["status"] != "not_registered" {
		t.Fatal(stat["routerPresence"])
	}
	// validate
	v := env.mustOK(env.post("/api/routes/custom-validate.cgi?bundleId=" + id))
	if v["validatedAt"] == nil {
		t.Fatal(v)
	}
	env.wantErr(env.post("/api/routes/custom-validate.cgi?bundleId=user-zzz"), 404, "BUNDLE_NOT_FOUND")
	// install through the shared engine
	st := env.exportBundle(id)
	if st["status"] != "installed" || env.r.routeCount("Proxy0") != 2 {
		t.Fatalf("custom export: %v router=%d", st["status"], env.r.routeCount("Proxy0"))
	}
	if !strings.Contains(strings.Join(env.r.running(), "\n"), "ip route 1.2.3.4 Proxy0 1200") {
		t.Fatal("host route form")
	}
	os := env.mustOK(env.get("/api/routes/overview.cgi"))
	if os["custom"].(map[string]any)["installed"].(float64) != 1 {
		t.Fatal(os["custom"])
	}
	// remove refuses while installed
	env.wantErr(env.post("/api/routes/custom-remove.cgi?bundleId="+id), 409, "ROUTES_CUSTOM_REMOVE_REQUIRES_DELETE")
	// replace files: installedVersion kept, update pending
	rep2 := env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("Discord", [2]string{"d.bat", "route add 5.5.5.0 mask 255.255.255.0 0.0.0.0"})))
	res = env.mustOK(env.call("POST", "/api/routes/custom-commit.cgi", map[string]any{"token": rep2["token"], "name": "Discord 2", "bundleId": id}))
	if res["replaced"] != true || env.state(id)["installedVersion"] == nil {
		t.Fatal(res)
	}
	st = env.exportBundle(id)
	if st["exportResult"].(map[string]any)["operation"] != "update" || env.r.routeCount("Proxy0") != 1 {
		t.Fatalf("update: %v router=%d", st["exportResult"], env.r.routeCount("Proxy0"))
	}
	tok := env.preflightToken(id, "delete")
	env.mustOK(env.post("/api/routes/delete.cgi?bundleId=" + id + "&preflightToken=" + tok))
	rm := env.mustOK(env.post("/api/routes/custom-remove.cgi?bundleId=" + id))
	if rm["removed"] != true || !strings.Contains(rm["backupPath"].(string), "user-removed-"+id) {
		t.Fatal(rm)
	}
	if env.e.bundleInList(id) || env.e.userBundleExists(id) || fileExists(env.e.catalogDir(id)) {
		t.Fatal("not removed")
	}
	env.wantErr(env.post("/api/routes/custom-remove.cgi?bundleId="+id), 404, "BUNDLE_NOT_FOUND")
	env.wantErr(env.post("/api/routes/custom-remove.cgi?bundleId=x"), 400, "ROUTES_BUNDLE_INVALID")
	// busy message of custom endpoints
	rel, _ := env.d.Lock.TryAcquire("servers", "x", "")
	r := env.call("POST", "/api/routes/custom-preview.cgi", previewBody("Discord", [2]string{"d.bat", discordBat}))
	env.wantErr(r, 409, "ROUTES_OPERATION_BUSY")
	if r.message() != msgBusyCustom {
		t.Fatal(r.message())
	}
	r = env.post("/api/routes/custom-remove.cgi?bundleId=user-abc")
	if r.message() != msgBusyRemove {
		t.Fatal(r.raw)
	}
	rel()
}

func TestCustomValidateDetectsDamage(t *testing.T) {
	env := newEnv(t)
	rep := env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("D", [2]string{"d.bat", discordBat})))
	res := env.mustOK(env.call("POST", "/api/routes/custom-commit.cgi", map[string]any{"token": rep["token"], "name": "D", "bundleId": nil}))
	id := res["bundleId"].(string)
	_ = os.WriteFile(filepath.Join(env.e.catalogDir(id), "source", "01.bat"), []byte("tampered"), 0o600)
	r := env.post("/api/routes/custom-validate.cgi?bundleId=" + id)
	env.wantErr(r, 400, "BUNDLE_DAMAGED")
	if r.message() != "Исходные BAT-файлы пользовательского набора изменены или отсутствуют." {
		t.Fatal(r.message())
	}
}

// TestRealKeeneticSave runs one export with the production save
// (keenetic.Save: 1 s convergence polling).
func TestRealKeeneticSave(t *testing.T) {
	env := newEnv(t)
	saveFn = keenetic.Save
	defer func() { saveFn = fastSave }()
	rep := env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("D", [2]string{"d.bat", "route add 5.5.5.0 mask 255.255.255.0 0.0.0.0"})))
	res := env.mustOK(env.call("POST", "/api/routes/custom-commit.cgi", map[string]any{"token": rep["token"], "name": "D", "bundleId": nil}))
	st := env.exportBundle(res["bundleId"].(string))
	if st["status"] != "installed" || env.r.saveCount < 2 {
		t.Fatal(st["status"])
	}
	_ = context.Background
}
