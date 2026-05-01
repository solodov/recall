package searchv1

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestSearchRequestV1KeepsStableQueryLimitAndSelectorHints(t *testing.T) {
	fields := messageDescriptor(t, "SearchRequest").Fields()
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

func TestSearchResponseV1KeepsStructuredResultsAndWarnings(t *testing.T) {
	searchResponse := messageDescriptor(t, "SearchResponse")
	fields := searchResponse.Fields()
	if fields.Len() != 2 {
		t.Fatalf("SearchResponse field count = %d, want results and warnings", fields.Len())
	}
	if field := fields.ByNumber(1); field == nil || string(field.Name()) != "results" || !field.IsList() || string(field.Message().Name()) != "Result" {
		t.Fatalf("field 1 = %v, want repeated Result results", field)
	}
	if field := fields.ByNumber(2); field == nil || string(field.Name()) != "warnings" || !field.IsList() || string(field.Message().Name()) != "Warning" {
		t.Fatalf("field 2 = %v, want repeated Warning warnings", field)
	}

	result := nestedMessageDescriptor(t, searchResponse, "Result")
	resultFields := result.Fields()
	if resultFields.Len() != 7 {
		t.Fatalf("Result field count = %d, want id, selector, fields, targets, group, score, and format", resultFields.Len())
	}
	wantNames := map[protoreflect.FieldNumber]string{
		1: "id",
		2: "selector",
		3: "fields",
		4: "targets",
		5: "group",
		6: "score",
		7: "format",
	}
	for number, name := range wantNames {
		if field := resultFields.ByNumber(number); field == nil || string(field.Name()) != name {
			t.Fatalf("Result field %d = %v, want %s", number, field, name)
		}
	}
	if field := resultFields.ByNumber(3); !field.IsList() || string(field.Message().Name()) != "Field" {
		t.Fatalf("Result.fields = %v, want repeated Field", field)
	}
	if field := resultFields.ByNumber(4); !field.IsList() || string(field.Message().Name()) != "OpenTarget" {
		t.Fatalf("Result.targets = %v, want repeated OpenTarget", field)
	}
	if field := resultFields.ByNumber(5); field.IsList() || string(field.Message().Name()) != "SearchGroup" {
		t.Fatalf("Result.group = %v, want SearchGroup", field)
	}
	if field := resultFields.ByNumber(6); !field.HasPresence() {
		t.Fatalf("Result.score = %v, want optional score", field)
	}
	if field := resultFields.ByNumber(7); field.IsList() || string(field.Message().Name()) != "Format" {
		t.Fatalf("Result.format = %v, want Format", field)
	}

	warning := nestedMessageDescriptor(t, searchResponse, "Warning")
	warningFields := warning.Fields()
	if warningFields.Len() != 2 {
		t.Fatalf("Warning field count = %d, want message and code", warningFields.Len())
	}
	if field := warningFields.ByNumber(1); field == nil || string(field.Name()) != "message" {
		t.Fatalf("Warning field 1 = %v, want message", field)
	}
	if field := warningFields.ByNumber(2); field == nil || string(field.Name()) != "code" || !field.HasPresence() {
		t.Fatalf("Warning field 2 = %v, want optional code", field)
	}
}

func TestResultFieldV1KeepsTypedValues(t *testing.T) {
	result := nestedMessageDescriptor(t, messageDescriptor(t, "SearchResponse"), "Result")
	fieldMessage := nestedMessageDescriptor(t, result, "Field")
	fields := fieldMessage.Fields()
	if fields.Len() != 4 {
		t.Fatalf("Field field count = %d, want key and three typed value fields", fields.Len())
	}
	if field := fields.ByNumber(1); field == nil || string(field.Name()) != "key" || field.ContainingOneof() != nil {
		t.Fatalf("Field field 1 = %v, want key outside value oneof", field)
	}
	for number, name := range map[protoreflect.FieldNumber]string{2: "text", 3: "integer", 4: "timestamp"} {
		field := fields.ByNumber(number)
		if field == nil || string(field.Name()) != name {
			t.Fatalf("Field value %d = %v, want %s", number, field, name)
		}
		if oneof := field.ContainingOneof(); oneof == nil || string(oneof.Name()) != "value" {
			t.Fatalf("Field value %s is in oneof %v, want value", name, oneof)
		}
	}
}

func TestResultFormatV1KeepsTitleAndDetailFields(t *testing.T) {
	result := nestedMessageDescriptor(t, messageDescriptor(t, "SearchResponse"), "Result")
	format := nestedMessageDescriptor(t, result, "Format")
	fields := format.Fields()
	if fields.Len() != 2 {
		t.Fatalf("Format field count = %d, want title_fields and detail_fields", fields.Len())
	}
	if field := fields.ByNumber(1); field == nil || string(field.Name()) != "title_fields" || !field.IsList() {
		t.Fatalf("Format field 1 = %v, want repeated title_fields", field)
	}
	if field := fields.ByNumber(2); field == nil || string(field.Name()) != "detail_fields" || !field.IsList() {
		t.Fatalf("Format field 2 = %v, want repeated detail_fields", field)
	}
}

func TestURITargetV1HasOnlyURI(t *testing.T) {
	fields := messageDescriptor(t, "UriTarget").Fields()
	if fields.Len() != 1 {
		t.Fatalf("UriTarget field count = %d, want only uri", fields.Len())
	}
	if field := fields.ByNumber(1); field == nil || string(field.Name()) != "uri" {
		t.Fatalf("field 1 = %v, want uri", field)
	}
	if field := fields.ByName("timestamp"); field != nil {
		t.Fatalf("UriTarget has timestamp field %v, want display timestamps represented as Result fields", field)
	}
}

func messageDescriptor(t *testing.T, name protoreflect.Name) protoreflect.MessageDescriptor {
	t.Helper()
	message := File_proto_recall_search_v1_search_proto.Messages().ByName(name)
	if message == nil {
		t.Fatalf("message %s not found", name)
	}
	return message
}

func nestedMessageDescriptor(t *testing.T, parent protoreflect.MessageDescriptor, name protoreflect.Name) protoreflect.MessageDescriptor {
	t.Helper()
	message := parent.Messages().ByName(name)
	if message == nil {
		t.Fatalf("nested message %s not found in %s", name, parent.FullName())
	}
	return message
}
