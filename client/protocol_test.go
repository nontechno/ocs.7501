package main

import (
	"encoding/base64"
	"strings"
	"testing"
)

// parse splits a body into key/value pairs the way a receiver would.
func parse(t *testing.T, body string) map[string]string {
	t.Helper()
	m := map[string]string{}
	for _, p := range strings.Split(body, ":") {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			t.Fatalf("malformed pair %q in %q", p, body)
		}
		m[k] = v
	}
	return m
}

// maxID is the longest legal id: 128 bytes total (125 chars + 3 slashes).
var maxID = strings.Repeat("a", 32) + "/" + strings.Repeat("b", 32) + "/" + strings.Repeat("c", 32) + "/" + strings.Repeat("d", 29)

func TestSpecExamples(t *testing.T) {
	tests := []struct {
		name string
		r    Report
		want map[string]string
	}{
		{
			"terraform blocked (spec header example)",
			Report{State: "blocked", Kind: "permission", App: "terraform", Msg: "Apply 3 to add, 1 to change, 0 to destroy?"},
			map[string]string{"state": "blocked", "kind": "permission", "app": "terraform",
				"msg": "QXBwbHkgMyB0byBhZGQsIDEgdG8gY2hhbmdlLCAwIHRvIGRlc3Ryb3k/"},
		},
		{
			"brew working",
			Report{State: "working", App: "brew", Msg: "Installing updates"},
			map[string]string{"state": "working", "app": "brew", "msg": "SW5zdGFsbGluZyB1cGRhdGVz"},
		},
		{
			"brew done",
			Report{State: "done", App: "brew", Msg: "Upgraded 12 packages"},
			map[string]string{"state": "done", "app": "brew", "msg": "VXBncmFkZWQgMTIgcGFja2FnZXM="},
		},
		{
			"child record with title and progress",
			Report{State: "working", ID: "us-east", Title: "US East", Progress: "40", Msg: "Pushing image"},
			map[string]string{"state": "working", "id": "us-east", "title": "VVMgRWFzdA==", "progress": "40",
				"msg": "UHVzaGluZyBpbWFnZQ=="},
		},
		{
			"child record blocked",
			Report{State: "blocked", Kind: "permission", ID: "eu-west", Title: "EU West", Msg: "Approve deploy to eu-west (production)?"},
			map[string]string{"state": "blocked", "kind": "permission", "id": "eu-west", "title": "RVUgV2VzdA==",
				"msg": "QXBwcm92ZSBkZXBsb3kgdG8gZXUtd2VzdCAocHJvZHVjdGlvbik/"},
		},
		{
			"clear one record",
			Report{State: "clear", ID: "us-east", Msg: "ignored", App: "ignored"},
			map[string]string{"state": "clear", "id": "us-east"},
		},
		{
			"clear everything",
			Report{State: "clear"},
			map[string]string{"state": "clear"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, errs := BuildReport(tc.r)
			if len(errs) > 0 {
				t.Fatalf("unexpected errors: %v", errs)
			}
			got := parse(t, body)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("%s = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestIgnoredKeysNotEmitted(t *testing.T) {
	body, errs := BuildReport(Report{State: "done", Kind: "auth", Progress: "50", Msg: "x"})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	got := parse(t, body)
	if _, ok := got["kind"]; ok {
		t.Error("kind must only accompany blocked")
	}
	if _, ok := got["progress"]; ok {
		t.Error("progress must only accompany working/blocked")
	}
}

func TestIndeterminateProgress(t *testing.T) {
	body, _ := BuildReport(Report{State: "working", Progress: ""})
	if strings.Contains(body, "progress") {
		t.Errorf("empty progress should be omitted, got %q", body)
	}
}

func TestValidation(t *testing.T) {
	tests := []struct {
		name string
		r    Report
	}{
		{"unknown state", Report{State: "paused"}},
		{"empty state", Report{}},
		{"progress too big", Report{State: "working", Progress: "101"}},
		{"progress negative", Report{State: "working", Progress: "-1"}},
		{"progress not a number", Report{State: "working", Progress: "half"}},
		{"bad kind", Report{State: "blocked", Kind: "nope"}},
		{"bad app", Report{State: "idle", App: "has space"}},
		{"app too long", Report{State: "idle", App: strings.Repeat("a", 33)}},
		{"bad id char", Report{State: "idle", ID: "a:b"}},
		{"empty id segment", Report{State: "idle", ID: "a//b"}},
		{"id segment too long", Report{State: "idle", ID: strings.Repeat("a", 33)}},
		{"id too deep", Report{State: "idle", ID: "a/b/c/d/e/f/g/h/i"}},
		{"newline in msg", Report{State: "idle", Msg: "a\nb"}},
		{"tab in title", Report{State: "idle", Title: "a\tb"}},
		{"C1 control in msg", Report{State: "idle", Msg: "a\u0085b"}},
		{"DEL in msg", Report{State: "idle", Msg: "a\x7fb"}},
		{"msg too long", Report{State: "idle", Msg: strings.Repeat("x", maxMsgDec+1)}},
		{"title too long", Report{State: "idle", Title: strings.Repeat("x", maxTitleDec+1)}},
		{"clear with bad id", Report{State: "clear", ID: "bad id"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, errs := BuildReport(tc.r); len(errs) == 0 {
				t.Error("expected a validation error")
			}
		})
	}
}

func TestLimitsAtBoundary(t *testing.T) {
	ok := []Report{
		{State: "idle", Msg: strings.Repeat("x", maxMsgDec)},
		{State: "idle", Title: strings.Repeat("x", maxTitleDec)},
		{State: "idle", App: strings.Repeat("a", 32)},
		{State: "idle", ID: "a/b/c/d/e/f/g/h"},
		{State: "idle", ID: maxID},
		{State: "working", Progress: "0"},
		{State: "working", Progress: "100"},
		{State: "idle", Msg: "héllo ✓ 日本語 🚀"},
	}
	for i, r := range ok {
		if _, errs := BuildReport(r); len(errs) > 0 {
			t.Errorf("case %d: %v", i, errs)
		}
	}
}

func TestLargestLegalReportFitsInSequence(t *testing.T) {
	// The spec says the largest legal report is under 3300 bytes.
	r := Report{
		State: "blocked", Kind: "permission", Progress: "100",
		ID:    maxID,
		App:   strings.Repeat("a", 32),
		Title: strings.Repeat("t", maxTitleDec),
		Msg:   strings.Repeat("m", maxMsgDec),
	}
	body, errs := BuildReport(r)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	seq, err := Sequence(body, TermST)
	if err != nil {
		t.Fatal(err)
	}
	if len(seq) >= 3300 {
		t.Errorf("largest legal report is %d bytes, spec says under 3300", len(seq))
	}
}

func TestSequenceFraming(t *testing.T) {
	s, err := Sequence("state=idle", TermST)
	if err != nil || s != "\x1b]7501;state=idle\x1b\\" {
		t.Errorf("ST framing: %q, %v", s, err)
	}
	s, err = Sequence("state=idle", TermBEL)
	if err != nil || s != "\x1b]7501;state=idle\x07" {
		t.Errorf("BEL framing: %q, %v", s, err)
	}
	s, err = Sequence(QueryBody, TermST)
	if err != nil || s != "\x1b]7501;?\x1b\\" {
		t.Errorf("query framing: %q, %v", s, err)
	}
	if _, err := Sequence(strings.Repeat("x", maxSequence), TermST); err == nil {
		t.Error("expected an over-4096 error")
	}
}

func TestRoundTripBase64(t *testing.T) {
	msg := "naïve ☃ 日本語"
	body, errs := BuildReport(Report{State: "done", Msg: msg})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	dec, err := base64.StdEncoding.DecodeString(parse(t, body)["msg"])
	if err != nil || string(dec) != msg {
		t.Errorf("round trip: %q, %v", dec, err)
	}
}
