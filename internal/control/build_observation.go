package control

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/BROadmin/broray/internal/platform"
)

// This endpoint is mounted only on the root-only Unix control socket.
const buildObservationPath = "/api/v1/daemon/build-observation"

type BuildObservation struct {
	Version      string `json:"version"`
	Revision     string `json:"revision"`
	Architecture string `json:"architecture"`
	SHA256       string `json:"sha256"`
	Model        string `json:"model,omitempty"`
	DeviceKey    string `json:"deviceKey,omitempty"`
}

func MountBuildObservation(mux *http.ServeMux, observe func(context.Context, string) (BuildObservation, bool)) {
	mux.HandleFunc(buildObservationPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		build, ok := observe(r.Context(), r.URL.Query().Get("sha256"))
		if !ok {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(build)
	})
}

func ObserveBuild(ctx context.Context, root platform.Root, digest string) (BuildObservation, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://broray"+buildObservationPath, nil)
	if err != nil {
		return BuildObservation{}, err
	}
	query := req.URL.Query()
	query.Set("sha256", digest)
	req.URL.RawQuery = query.Encode()
	httpClient := client(root, 3*time.Second)
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	defer httpClient.CloseIdleConnections()
	resp, err := httpClient.Do(req)
	if err != nil {
		return BuildObservation{}, err
	}
	defer resp.Body.Close()
	var result BuildObservation
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1025))
	if err != nil || len(data) > 1024 || resp.StatusCode != http.StatusOK || json.Unmarshal(data, &result) != nil {
		return BuildObservation{}, errors.New("current daemon build could not be confirmed")
	}
	return result, nil
}
