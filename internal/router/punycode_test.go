package router

import "testing"

func TestNormalizeHost(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"example.com", "example.com"},
		{"Example.COM", "example.com"},
		{"example.com.", "example.com"},
		{"рф", "xn--p1ai"},
		{"РФ", "xn--p1ai"},
		{"пример.рф", "xn--e1afmkfd.xn--p1ai"},
		{"Пример.РФ", "xn--e1afmkfd.xn--p1ai"},
		{"café.com", "xn--caf-dma.com"},
		{"почта.mail.ru", "xn--80a1acny.mail.ru"},
		{"xn--p1ai", "xn--p1ai"},
		{"", ""},
	}
	for _, tt := range tests {
		got, err := NormalizeHost(tt.in)
		if err != nil {
			t.Errorf("NormalizeHost(%q): unexpected error: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("NormalizeHost(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPunycodeEncodeRFCSample(t *testing.T) {
	in := "почемужеонинеговорятпорусски"
	want := "b1abfaaepdrnnbgefbadotcwatmq2g4l"
	got, err := punycodeEncode(in)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("punycodeEncode(%q) = %q, want %q", in, got, want)
	}
}
