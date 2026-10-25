package update

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.2.21", "0.2.20", 1},
		{"v0.2.20", "0.2.21", -1},
		{"0.2.21", "0.2.21", 0},
		{"1.0.0", "0.9.99", 1},
		{"", "0.1.0", -1},
	}
	for _, tc := range cases {
		got := CompareVersions(tc.a, tc.b)
		if got != tc.want {
			t.Fatalf("CompareVersions(%q, %q) = %d; want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
