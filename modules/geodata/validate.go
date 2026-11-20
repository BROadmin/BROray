package geodata

import (
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Native schema: Xray v26.11.9 common/geodata/geodat.proto.
// Walk slices the bounded original bytes; it never materializes a full list
// of CIDRs or domains. Real-core config validation is a separate gate.
type wireField struct {
	number, wire uint64
	data         []byte
	value        uint64
}

var errWire = errors.New("malformed native geodata")

func walk(data []byte, visit func(wireField) error) error {
	for len(data) > 0 {
		key, n := binary.Uvarint(data)
		if n <= 0 || key>>3 == 0 || key>>3 > 536870911 {
			return errWire
		}
		data = data[n:]
		f := wireField{number: key >> 3, wire: key & 7}
		switch f.wire {
		case 0:
			v, n := binary.Uvarint(data)
			if n <= 0 {
				return errWire
			}
			f.value = v
			data = data[n:]
		case 1:
			if len(data) < 8 {
				return errWire
			}
			f.data = data[:8]
			data = data[8:]
		case 2:
			size, n := binary.Uvarint(data)
			if n <= 0 {
				return errWire
			}
			data = data[n:]
			if size > uint64(len(data)) {
				return errWire
			}
			f.data = data[:int(size)]
			data = data[int(size):]
		case 5:
			if len(data) < 4 {
				return errWire
			}
			f.data = data[:4]
			data = data[4:]
		default:
			return errWire
		}
		if err := visit(f); err != nil {
			return err
		}
	}
	return nil
}

func validCIDR(data []byte) error {
	var ip []byte
	var prefix uint64
	var seenIP, seenPrefix bool
	err := walk(data, func(f wireField) error {
		switch f.number {
		case 1:
			if f.wire != 2 || seenIP {
				return errWire
			}
			seenIP = true
			ip = f.data
		case 2:
			if f.wire != 0 || seenPrefix {
				return errWire
			}
			seenPrefix = true
			prefix = f.value
		}
		return nil
	})
	if err != nil {
		return err
	}
	if (len(ip) != 4 && len(ip) != 16) || prefix > uint64(len(ip)*8) {
		return fmt.Errorf("invalid CIDR")
	}
	return nil
}

func validAttribute(data []byte) error {
	var key bool
	var value bool
	err := walk(data, func(f wireField) error {
		switch f.number {
		case 1:
			if key || f.wire != 2 || len(f.data) == 0 || !utf8.Valid(f.data) {
				return errWire
			}
			key = true
		case 2, 3:
			if value || f.wire != 0 || (f.number == 2 && f.value > 1) {
				return errWire
			}
			value = true
		}
		return nil
	})
	if err == nil && !key {
		return errWire
	}
	return err
}

func validDomain(data []byte) error {
	_, err := decodeDomain(data)
	return err
}

func decodeDomain(data []byte) (DomainMatcher, error) {
	var typ uint64
	var value []byte
	var seenType, seenValue bool
	err := walk(data, func(f wireField) error {
		switch f.number {
		case 1:
			if f.wire != 0 || seenType {
				return errWire
			}
			seenType = true
			typ = f.value
		case 2:
			if f.wire != 2 || seenValue {
				return errWire
			}
			seenValue = true
			value = f.data
		case 3:
			if f.wire != 2 {
				return errWire
			}
			return validAttribute(f.data)
		}
		return nil
	})
	if err != nil {
		return DomainMatcher{}, err
	}
	if typ > 3 || len(value) == 0 || len(value) > 16384 || !utf8.Valid(value) || strings.ContainsRune(string(value), 0) {
		return DomainMatcher{}, fmt.Errorf("invalid domain value")
	}
	if typ == 1 {
		if _, err := regexp.Compile(string(value)); err != nil {
			return DomainMatcher{}, fmt.Errorf("invalid domain regex: %w", err)
		}
	}
	return DomainMatcher{Kind: DomainMatchKind(typ), Value: string(value)}, nil
}

func validateEntry(kind Kind, data []byte) (string, error) {
	var code string
	count := 0
	seenCode := false
	seenReverse := false
	err := walk(data, func(f wireField) error {
		switch f.number {
		case 1:
			if f.wire != 2 || seenCode {
				return errWire
			}
			seenCode = true
			code = strings.ToLower(string(f.data))
			if !tagPattern.MatchString(code) {
				return fmt.Errorf("invalid native code")
			}
		case 2:
			if f.wire != 2 {
				return errWire
			}
			count++
			if kind == GeoIP {
				return validCIDR(f.data)
			}
			return validDomain(f.data)
		case 3:
			if kind == GeoIP {
				if f.wire != 0 || seenReverse || f.value > 1 {
					return errWire
				}
				seenReverse = true
			}
		}
		return nil
	})
	if err == nil && (!seenCode || count == 0) {
		err = fmt.Errorf("empty native entry")
	}
	return code, err
}

func Validate(kind Kind, data []byte, required []string) error {
	if kind != GeoIP && kind != GeoSite {
		return fmt.Errorf("unknown geodata kind")
	}
	if len(data) == 0 || len(data) > MaxAssetBytes {
		return fmt.Errorf("geodata size outside limit")
	}
	tags, err := normalizeTags(required, false)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	err = walk(data, func(f wireField) error {
		if f.number != 1 {
			return nil
		}
		if f.wire != 2 {
			return errWire
		}
		code, err := validateEntry(kind, f.data)
		if err != nil {
			return err
		}
		if seen[code] {
			return fmt.Errorf("duplicate native code %s", code)
		}
		seen[code] = true
		if len(seen) > 32768 {
			return fmt.Errorf("too many native entries")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(seen) == 0 {
		return fmt.Errorf("no native entries")
	}
	for _, tag := range tags {
		if !seen[tag] {
			return fmt.Errorf("native tag %s missing", tag)
		}
	}
	return nil
}
