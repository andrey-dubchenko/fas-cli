package flink_test

import (
	"fas-cli/internal/flink"
	"sort"
	"testing"
)

func TestCompareVersionsNatural(t *testing.T) {
	got := []string{"1.10", "1.9", "10", "2", "1.2.10", "1.2.9", "v2", "v10"}
	sort.Slice(got, func(i, j int) bool { return flink.CompareVersions(got[i], got[j]) < 0 })
	want := []string{"1.2.9", "1.2.10", "1.9", "1.10", "2", "10", "v2", "v10"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestFindIndexAndGroupSchemas(t *testing.T) {
	aliases := map[string][]string{"foo-v1": {"live"}, "Bar_2": nil}
	if idx, al := flink.FindIndex("Foo", "V1", aliases); idx != "foo-v1" || len(al) != 1 {
		t.Fatalf("flink.FindIndex = %q %v", idx, al)
	}
	if idx, _ := flink.FindIndex("Bar", "2", aliases); idx != "Bar_2" {
		t.Fatalf("flink.FindIndex exact-case = %q", idx)
	}
	groups := flink.GroupSchemas([]flink.SchemaShort{{Name: "b", Version: "10"}, {Name: "a", Version: "2"}, {Name: "b", Version: "9"}}, nil)
	if len(groups) != 2 || groups[0].Name != "a" || groups[1].Versions[0].Version != "9" {
		t.Fatalf("flink.GroupSchemas = %+v", groups)
	}
}

func TestSchemaPathEscapes(t *testing.T) {
	if got := flink.SchemaPath("/v2/schema/{name}/{version}", "a b", "1/2"); got != "/v2/schema/a%20b/1%2F2" {
		t.Fatalf("flink.SchemaPath = %q", got)
	}
}
