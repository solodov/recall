package searchv1

import "testing"

func TestSearchRequestV1KeepsStableQueryLimitAndSelectorHints(t *testing.T) {
	fields := File_proto_recall_search_v1_search_proto.Messages().ByName("SearchRequest").Fields()
	if fields.Len() != 3 {
		t.Fatalf("SearchRequest field count = %d, want query, limit, and selector_hints", fields.Len())
	}
	if field := fields.ByNumber(1); field == nil || string(field.Name()) != "query" {
		t.Fatalf("field 1 = %v, want query", field)
	}
	if field := fields.ByNumber(2); field == nil || string(field.Name()) != "limit" {
		t.Fatalf("field 2 = %v, want limit", field)
	} else if !field.HasPresence() {
		t.Fatal("limit should be optional so direct provider calls can omit it")
	}
	if field := fields.ByNumber(3); field == nil || string(field.Name()) != "selector_hints" || !field.IsList() {
		t.Fatalf("field 3 = %v, want repeated selector_hints", field)
	}
}
