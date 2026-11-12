package amnezia

// ClashInterfaceField maps the spelling used by provider exports to the AWG
// INI model. Unknown fields remain in the original profile, never executed.
func ClashInterfaceField(name string) (string, bool) {
	fields := map[string]string{
		"jc": "Jc", "jmin": "Jmin", "jmax": "Jmax", "s1": "S1", "s2": "S2", "s3": "S3", "s4": "S4",
		"h1": "H1", "h2": "H2", "h3": "H3", "h4": "H4", "i1": "I1", "i2": "I2", "i3": "I3", "i4": "I4", "i5": "I5",
		"j1": "J1", "j2": "J2", "j3": "J3", "itime": "Itime",
		"header-protection-key": "HeaderProtectionKey", "content-padding-addition": "ContentPaddingAddition",
		"rekey-after-time": "RekeyAfterTime", "rekey-timeout": "RekeyTimeout", "reject-after-time": "RejectAfterTime",
		"keepalive-timeout": "KeepaliveTimeout", "max-handshake-attempts": "MaxHandshakeAttempts",
		"random-trailers": "RandomTrailers", "disable-cookies": "DisableCookies",
	}
	value, ok := fields[name]
	return value, ok
}

func nativeAWGField(name string) bool {
	switch name {
	case "Jc", "Jmin", "Jmax", "S1", "S2", "S3", "S4", "H1", "H2", "H3", "H4",
		"I1", "I2", "I3", "I4", "I5", "J1", "J2", "J3", "Itime",
		"HeaderProtectionKey", "ContentPaddingAddition", "RekeyAfterTime", "RekeyTimeout",
		"RejectAfterTime", "KeepaliveTimeout", "MaxHandshakeAttempts", "RandomTrailers", "DisableCookies":
		return true
	}
	return false
}

func emptyNativeAWGField(name string) bool {
	switch name {
	case "I1", "I2", "I3", "I4", "I5", "J1", "J2", "J3":
		return true
	}
	return false
}
