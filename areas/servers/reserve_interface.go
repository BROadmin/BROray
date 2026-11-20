package servers

func (s *svc) reserveInterfaceDescription() (string, error) {
	prefs, err := readReservePreferences(s.d)
	if err != nil {
		return "", err
	}
	if prefs.Enabled {
		return "BROray - mix-auto", nil
	}
	return "", nil
}
