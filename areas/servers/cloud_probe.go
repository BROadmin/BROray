package servers

import (
	"context"
	"sort"
	"time"
)

// Each cloud measurement uses the same running temporary core: one warm-up
// and three samples. Retries are bounded, cancellable and never start cores.
func measureCloudPath(ctx context.Context, request func(context.Context) (int, error)) probeResult {
	fail := func(stage string) probeResult {
		return probeResult{Stage: stage, Error: "Запрос через облачный транспорт не выполнен. Подробности ответа хранилища скрыты, чтобы не раскрыть реквизиты."}
	}
	started := time.Now()
	ready := false
	for attempt := 0; attempt < 2; attempt++ {
		if ctx.Err() != nil {
			return fail("proxy-cancelled")
		}
		code, err := request(ctx)
		if err == nil && code >= 200 && code < 400 {
			ready = true
			break
		}
		// Authentication and other permanent HTTP failures cannot be repaired
		// by repeating the same credentials. Only transient failures back off.
		if err == nil && code != 429 && code < 500 {
			return fail("proxy-warmup")
		}
		if attempt == 0 {
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return fail("proxy-cancelled")
			case <-timer.C:
			}
		}
	}
	if !ready {
		return fail("proxy-warmup")
	}
	warmup := time.Since(started).Milliseconds()
	samples := make([]int64, 0, 3)
	for i := 0; i < 3; i++ {
		if ctx.Err() != nil {
			return fail("proxy-cancelled")
		}
		start := time.Now()
		code, err := request(ctx)
		if err != nil || code < 200 || code >= 400 {
			return fail("proxy-https")
		}
		samples = append(samples, time.Since(start).Milliseconds())
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	latency, jitter := samples[1], samples[2]-samples[0]
	return probeResult{Success: true, Stage: "proxy-https", WarmupMs: &warmup, LatencyMs: &latency, JitterMs: &jitter}
}

// PrivateTransportConfig identifies the two transports carrying nested cloud
// credentials. Public core output is deliberately bounded to a result message.
func PrivateTransportConfig(cfg []byte) bool {
	m, ok := decodeObject(cfg)
	if !ok {
		return false
	}
	out, _ := m["outbounds"].([]any)
	for _, o := range out {
		if get(o, "protocol") == "masque" || get(o, "streamSettings", "network") == "xdrive" {
			return true
		}
	}
	return false
}

func SafeCoreOutput(cfg []byte, output string, success bool) string {
	if !PrivateTransportConfig(cfg) {
		return output
	}
	if success {
		return "Конфигурация Xray проверена."
	}
	return "Ядро отклонило конфигурацию облачного транспорта. Проверьте версию ядра и параметры; подробный ответ скрыт для защиты реквизитов."
}
