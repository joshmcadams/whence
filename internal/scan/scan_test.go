package scan

import "testing"

func TestProtoOf(t *testing.T) {
	cases := []struct {
		family uint32
		want   string
	}{
		{2, "tcp"},   // AF_INET
		{10, "tcp6"}, // AF_INET6 on linux
		{23, "tcp6"}, // AF_INET6 on windows
		{30, "tcp6"}, // AF_INET6 on darwin
		{0, "tcp"},   // unknown families default to tcp
		{99, "tcp"},
	}
	for _, tc := range cases {
		if got := protoOf(tc.family); got != tc.want {
			t.Errorf("protoOf(%d) = %q, want %q", tc.family, got, tc.want)
		}
	}
}
