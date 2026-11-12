package amnezia

import (
	"bytes"
	"errors"
)

var ErrTooManyContainers = errors.New("AmneziaWG: превышено число клиентских контейнеров")

// ParseDocument recognizes native Amnezia client JSON, without root policy or
// remote provider resolution. It returns only independently validated profiles.
func ParseDocument(data []byte, maxNodes int) ([]Imported, bool, error) {
	data = bytes.TrimPrefix(data, []byte("\ufeff"))
	if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
		return nil, false, nil
	}
	candidate := bytes.Contains(data, []byte(`"containers"`))
	if len(data) > MaxProfileBytes {
		return nil, candidate, invalid("превышен размер профиля")
	}
	root, err := object(data)
	if err != nil {
		return nil, candidate, err
	}
	value, recognized := root["containers"]
	if !recognized {
		return nil, false, nil
	}
	for _, field := range []string{"proxies", "protocol", "outbounds"} {
		if _, exists := root[field]; exists {
			return nil, true, invalid("смешаны форматы клиентской конфигурации")
		}
	}
	containers, ok := value.([]any)
	if !ok || len(containers) == 0 {
		return nil, true, invalid("ожидался список клиентских контейнеров")
	}
	if maxNodes < 1 || len(containers) > maxNodes {
		return nil, true, ErrTooManyContainers
	}
	name := "AmneziaWG"
	if value, exists := root["description"]; exists {
		var ok bool
		name, ok = value.(string)
		if !ok {
			return nil, true, invalid("неправильное имя профиля")
		}
		if name == "" {
			name = "AmneziaWG"
		}
	}
	if len(name) > 1024 || !plain(name) {
		return nil, true, invalid("неправильное имя профиля")
	}
	profiles := make([]Imported, 0, len(containers))
	for _, value := range containers {
		container, ok := value.(map[string]any)
		if !ok {
			return nil, true, invalid("неправильный контейнер")
		}
		parsed, err := parseContainer(container, name)
		if err != nil {
			return nil, true, err
		}
		profiles = append(profiles, parsed)
	}
	return profiles, true, nil
}
