package zoom

import "testing"

func TestPhoneToDigits(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"09037241917", "9037241917"},
		{"+819037241917", "9037241917"},
		{"090-3724-1917", "9037241917"},
		{"090 3724 1917", "9037241917"},
		{"(03) 1234-5678", "0312345678"},
		{"+81 (03) 1234-5678", "0312345678"},
		{"0312345678", "0312345678"},
		{"", ""},
		{"1234", ""}, // < 10 digits
		{"1234567890", "1234567890"},
		{"abc", ""},
	}
	for _, c := range cases {
		got := PhoneToDigits(c.in)
		if got != c.want {
			t.Errorf("PhoneToDigits(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
