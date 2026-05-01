package normalize

import (
	"time"

	searchv1 "github.com/solodov/recall/proto/recall/search/v1"
)

// FieldKind identifies the concrete typed value carried by a structured result
// field after protobuf oneof decoding.
type FieldKind string

const (
	FieldKindText      FieldKind = "text"
	FieldKindInteger   FieldKind = "integer"
	FieldKindTimestamp FieldKind = "timestamp"
)

// Field is a provider-owned result fact in a small internal shape so recall core
// code does not need to repeatedly inspect generated protobuf oneof wrappers.
type Field struct {
	Key       string
	Kind      FieldKind
	Text      string
	Integer   int64
	Timestamp time.Time
}

// Fields returns the structured fields for result in provider order. Invalid or
// unset value kinds are skipped because normalization reports those as provider
// response errors before rendering or ranking uses this helper.
func Fields(result *searchv1.SearchResponse_Result) []Field {
	if result == nil {
		return nil
	}
	fields := make([]Field, 0, len(result.GetFields()))
	for _, protoField := range result.GetFields() {
		field, ok := DecodeField(protoField)
		if ok {
			fields = append(fields, field)
		}
	}
	return fields
}

// FieldByKey returns the first structured field with key. Normalized provider
// responses reject duplicate field keys, so callers can treat a match as unique.
func FieldByKey(result *searchv1.SearchResponse_Result, key string) (Field, bool) {
	if result == nil {
		return Field{}, false
	}
	for _, protoField := range result.GetFields() {
		if protoField.GetKey() != key {
			continue
		}
		return DecodeField(protoField)
	}
	return Field{}, false
}

// DecodeField converts one protobuf field to the small internal field shape.
func DecodeField(protoField *searchv1.SearchResponse_Result_Field) (Field, bool) {
	if protoField == nil {
		return Field{}, false
	}
	field := Field{Key: protoField.GetKey()}
	switch value := protoField.GetValue().(type) {
	case *searchv1.SearchResponse_Result_Field_Text:
		field.Kind = FieldKindText
		field.Text = value.Text
	case *searchv1.SearchResponse_Result_Field_Integer:
		field.Kind = FieldKindInteger
		field.Integer = value.Integer
	case *searchv1.SearchResponse_Result_Field_Timestamp:
		if value.Timestamp == nil || !value.Timestamp.IsValid() {
			return Field{}, false
		}
		field.Kind = FieldKindTimestamp
		field.Timestamp = value.Timestamp.AsTime()
	default:
		return Field{}, false
	}
	return field, true
}
