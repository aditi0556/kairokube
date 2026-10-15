package checkpoint

import "testing"

func TestParseRuntimeInfo(t *testing.T) {
	got := ParseRuntimeInfo("crio://1.35.0")
	if got.Name != "crio" || got.Version != "1.35.0" || !got.Supported {
		t.Fatalf("unexpected runtime info: %+v", got)
	}
}

func TestValidateCheckpointRuntime(t *testing.T) {
	for _, tc := range []struct {
		raw string
		ok  bool
	}{
		{raw: "crio://1.35.0", ok: true},
		{raw: "containerd://2.1.0", ok: true},
		{raw: "docker://29.0", ok: false},
		{raw: "", ok: false},
	} {
		err := ValidateCheckpointRuntime(tc.raw)
		if (err == nil) != tc.ok {
			t.Errorf("ValidateCheckpointRuntime(%q) error=%v, want ok=%v", tc.raw, err, tc.ok)
		}
	}
}
