package router

import (
	"errors"
	"strings"
)

const (
	base        = 36
	tmin        = 1
	tmax        = 26
	skew        = 38
	damp        = 700
	initialBias = 72
	initialN    = 128
)

var errPunycodeOverflow = errors.New("punycode: overflow")

func NormalizeHost(host string) (string, error) {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if isASCII(host) {
		return host, nil
	}
	labels := strings.Split(host, ".")
	for i, label := range labels {
		if isASCII(label) {
			continue
		}
		encoded, err := punycodeEncode(label)
		if err != nil {
			return "", err
		}
		labels[i] = "xn--" + encoded
	}
	return strings.Join(labels, "."), nil
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func punycodeEncode(label string) (string, error) {
	runes := []rune(label)
	var out []byte
	for _, r := range runes {
		if r < initialN {
			out = append(out, byte(r))
		}
	}
	basicCount := len(out)
	if basicCount > 0 {
		out = append(out, '-')
	}
	n, delta, bias := initialN, 0, initialBias
	for h := basicCount; h < len(runes); {
		m := rune(0x7FFFFFFF)
		for _, r := range runes {
			if int(r) >= n && r < m {
				m = r
			}
		}
		delta += (int(m) - n) * (h + 1)
		if delta < 0 {
			return "", errPunycodeOverflow
		}
		n = int(m)
		for _, r := range runes {
			if int(r) < n {
				delta++
				if delta < 0 {
					return "", errPunycodeOverflow
				}
			}
			if int(r) == n {
				q := delta
				for k := base; ; k += base {
					t := k - bias
					switch {
					case t < tmin:
						t = tmin
					case t > tmax:
						t = tmax
					}
					if q < t {
						break
					}
					out = append(out, encodeDigit(t+(q-t)%(base-t)))
					q = (q - t) / (base - t)
				}
				out = append(out, encodeDigit(q))
				bias = adapt(delta, h+1, h == basicCount)
				delta = 0
				h++
			}
		}
		delta++
		n++
	}
	return string(out), nil
}

func encodeDigit(d int) byte {
	if d < 26 {
		return byte('a' + d)
	}
	return byte('0' + d - 26)
}

func adapt(delta, numPoints int, firstTime bool) int {
	if firstTime {
		delta /= damp
	} else {
		delta /= 2
	}
	delta += delta / numPoints
	k := 0
	for delta > (base-tmin)*tmax/2 {
		delta /= base - tmin
		k += base
	}
	return k + (base-tmin+1)*delta/(skew+delta)
}
