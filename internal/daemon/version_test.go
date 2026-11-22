package daemon

import "testing"

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v0.3.0", "v0.2.0", true},
		{"v0.2.1", "v0.2.0", true},
		{"v1.0.0", "v0.9.9", true},
		{"v0.10.0", "v0.9.0", true},
		{"v0.2.0", "v0.2.0", false},
		{"v0.1.9", "v0.2.0", false},
		// 源码构建的版本带 +source.<目录> 元数据，比较时忽略。
		{"v0.3.0", "v0.2.0+source.abc123", true},
		{"v0.2.0", "v0.2.0+source.abc123", false},
		{"v0.3.0", "dev", false},
		{"", "v0.2.0", false},
		{"v0.3", "v0.2.0", false},
		{"v0.3.0-rc1", "v0.2.0", false},
	}
	for _, c := range cases {
		if got := NewerVersion(c.latest, c.current); got != c.want {
			t.Errorf("NewerVersion(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}
