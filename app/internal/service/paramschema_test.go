package service

import (
	"encoding/json"
	"errors"
	"testing"
)

func mustCompileSchema(t *testing.T, raw string) *ParamSchema {
	t.Helper()
	schema, err := CompileSchema(json.RawMessage(raw))
	if err != nil {
		t.Fatalf("compile schema %s: %v", raw, err)
	}
	return schema
}

func mustValidateOK(t *testing.T, schema *ParamSchema, instance string) {
	t.Helper()
	if err := schema.Validate(json.RawMessage(instance)); err != nil {
		t.Errorf("instance %s unexpectedly rejected: %v", instance, err)
	}
}

func mustValidateFail(t *testing.T, schema *ParamSchema, instance string) {
	t.Helper()
	if err := schema.Validate(json.RawMessage(instance)); err == nil {
		t.Errorf("instance %s unexpectedly accepted", instance)
	}
}

// TestParamsSchemaCompileRejectsUnknownKeyword schema 本身的合法性校验：
// 未知关键字 / 禁止关键字 / x-* UI 元数据一律拒绝（manifest-schema「未知字段拒绝」精神）。
func TestParamsSchemaCompileRejectsUnknownKeyword(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"unknown keyword", `{"type":"object","foo":1}`},
		{"$ref forbidden", `{"type":"object","$ref":"#/definitions/x"}`},
		{"oneOf forbidden", `{"oneOf":[{"type":"string"}]}`},
		{"allOf forbidden", `{"allOf":[{"type":"object"}]}`},
		{"not forbidden", `{"type":"object","not":{"type":"string"}}`},
		{"x-ui metadata", `{"type":"object","x-ui":{"widget":"slider"}}`},
		{"definitions forbidden", `{"definitions":{"x":{"type":"string"}}}`},
		{"type outside subset", `{"type":"null"}`},
		{"type array form", `{"type":["string","null"]}`},
		{"schema not object", `[]`},
		{"additionalProperties true", `{"type":"object","additionalProperties":true}`},
		{"items tuple form", `{"type":"array","items":[]}`},
		{"minItems negative", `{"type":"array","minItems":-1}`},
		{"minItems fractional", `{"type":"array","minItems":1.5}`},
		{"minLength exceeds maxLength", `{"type":"string","minLength":5,"maxLength":3}`},
		{"minItems exceeds maxItems", `{"type":"array","minItems":3,"maxItems":1}`},
		{"minimum exceeds maximum", `{"type":"number","minimum":2,"maximum":1}`},
		{"multipleOf zero", `{"type":"number","multipleOf":0}`},
		{"enum empty", `{"type":"string","enum":[]}`},
		{"pattern invalid RE2", `{"type":"string","pattern":"(?=lookahead)"}`},
		{"title not string", `{"type":"string","title":42}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CompileSchema(json.RawMessage(tc.raw))
			if err == nil {
				t.Fatalf("schema %s unexpectedly compiled", tc.raw)
			}
			var sce *SchemaCompileError
			if !errors.As(err, &sce) {
				t.Fatalf("error type = %T, want *SchemaCompileError", err)
			}
		})
	}
}

// TestParamsSchemaCompileAcceptsRestrictedSubset 受限子集内的完整关键字组合可编译。
func TestParamsSchemaCompileAcceptsRestrictedSubset(t *testing.T) {
	raw := `{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"type": "object",
		"additionalProperties": false,
		"title": "检测参数",
		"description": "YOLOv8n 推理参数",
		"required": ["confidence_threshold", "classes"],
		"properties": {
			"confidence_threshold": {
				"type": "number",
				"minimum": 0,
				"maximum": 1,
				"multipleOf": 0.05,
				"default": 0.5
			},
			"mode": {"type": "string", "enum": ["fast", "balanced", "accurate"], "default": "balanced"},
			"roi_name": {"type": "string", "minLength": 1, "maxLength": 16, "pattern": "^[a-z0-9_-]+$"},
			"classes": {
				"type": "array",
				"items": {"type": "string"},
				"minItems": 1,
				"maxItems": 10,
				"uniqueItems": true
			},
			"nested": {
				"type": "object",
				"additionalProperties": false,
				"properties": {"ratio": {"type": "number", "minimum": 0}}
			},
			"enabled": {"type": "boolean", "default": true},
			"stride": {"type": "integer", "minimum": 1}
		}
	}`
	schema := mustCompileSchema(t, raw)

	valid := `{
		"confidence_threshold": 0.55,
		"mode": "fast",
		"roi_name": "gate-01",
		"classes": ["person", "helmet"],
		"nested": {"ratio": 0.5},
		"enabled": true,
		"stride": 2
	}`
	mustValidateOK(t, schema, valid)
}

// TestParamsSchemaValidationTypes 六种类型与 integer 整值语义。
func TestParamsSchemaValidationTypes(t *testing.T) {
	schema := mustCompileSchema(t, `{"type":"object","additionalProperties":false,"properties":{
		"count":{"type":"integer"},
		"score":{"type":"number"},
		"label":{"type":"string"},
		"active":{"type":"boolean"},
		"tags":{"type":"array","items":{"type":"string"}},
		"meta":{"type":"object","additionalProperties":false,"properties":{"v":{"type":"integer"}}}
	}}`)

	// integer：3.0 合法（JSON 数值 3 反序列化为 float64 3.0），3.5 非法。
	mustValidateOK(t, schema, `{"count":3,"score":0.5,"label":"x","active":true,"tags":[],"meta":{"v":1}}`)
	mustValidateOK(t, schema, `{"count":3,"score":3,"label":"x"}`)
	mustValidateFail(t, schema, `{"count":3.5}`)
	mustValidateFail(t, schema, `{"count":"3"}`)
	mustValidateFail(t, schema, `{"score":"0.5"}`)
	mustValidateFail(t, schema, `{"label":1}`)
	mustValidateFail(t, schema, `{"active":"true"}`)
	mustValidateFail(t, schema, `{"tags":"a"}`)
	mustValidateFail(t, schema, `{"meta":[]}`)
}

// TestParamsSchemaValidationObject 对象：required、additionalProperties=false、嵌套。
func TestParamsSchemaValidationObject(t *testing.T) {
	schema := mustCompileSchema(t, `{"type":"object","additionalProperties":false,
		"required":["a"],
		"properties":{"a":{"type":"string"},"nested":{"type":"object","additionalProperties":false,"properties":{"b":{"type":"integer"}}}}}`)

	mustValidateOK(t, schema, `{"a":"x"}`)
	mustValidateOK(t, schema, `{"a":"x","nested":{"b":1}}`)
	mustValidateFail(t, schema, `{}`)                             // 缺 required a
	mustValidateFail(t, schema, `{"a":"x","extra":1}`)            // 根级未知字段
	mustValidateFail(t, schema, `{"a":"x","nested":{"extra":1}}`) // 嵌套未知字段
	mustValidateFail(t, schema, `{"a":1}`)                        // 类型不符
	mustValidateFail(t, schema, `null`)                           // 根类型不符
}

// TestParamsSchemaValidationArray 数组：items 递归、minItems/maxItems、uniqueItems。
func TestParamsSchemaValidationArray(t *testing.T) {
	schema := mustCompileSchema(t, `{"type":"array",
		"items":{"type":"object","additionalProperties":false,"properties":{"n":{"type":"integer"}}},
		"minItems":2,"maxItems":4,"uniqueItems":true}`)

	mustValidateOK(t, schema, `[{"n":1},{"n":2}]`)
	mustValidateOK(t, schema, `[{"n":1},{"n":2},{"n":3},{"n":4}]`)
	mustValidateFail(t, schema, `[{"n":1}]`)                                 // < minItems
	mustValidateFail(t, schema, `[{"n":1},{"n":2},{"n":3},{"n":4},{"n":5}]`) // > maxItems
	mustValidateFail(t, schema, `[{"n":1},{"n":1}]`)                         // uniqueItems 深度相等
	mustValidateFail(t, schema, `[{"n":"x"},{"n":2}]`)                       // items 类型
}

// TestParamsSchemaValidationNumber 数值关键字：minimum/maximum/multipleOf 与 enum 深度相等。
func TestParamsSchemaValidationNumber(t *testing.T) {
	schema := mustCompileSchema(t, `{"type":"number","minimum":0,"maximum":1,"multipleOf":0.25}`)
	mustValidateOK(t, schema, `0`)
	mustValidateOK(t, schema, `0.25`)
	mustValidateOK(t, schema, `1`)
	mustValidateFail(t, schema, `-0.1`)
	mustValidateFail(t, schema, `1.1`)
	mustValidateFail(t, schema, `0.3`)

	enumSchema := mustCompileSchema(t, `{"type":"object","properties":{"mode":{"enum":["a",1,true,null]}}}`)
	mustValidateOK(t, enumSchema, `{"mode":"a"}`)
	mustValidateOK(t, enumSchema, `{"mode":1}`)
	mustValidateOK(t, enumSchema, `{"mode":true}`)
	mustValidateFail(t, enumSchema, `{"mode":"b"}`)
}

// TestParamsSchemaValidationString 字符串关键字：minLength/maxLength（码点计长）与 pattern。
func TestParamsSchemaValidationString(t *testing.T) {
	schema := mustCompileSchema(t, `{"type":"string","minLength":2,"maxLength":4,"pattern":"^[a-z]+$"}`)
	mustValidateOK(t, schema, `"ab"`)
	mustValidateOK(t, schema, `"abcd"`)
	mustValidateFail(t, schema, `"a"`)
	mustValidateFail(t, schema, `"abcde"`)
	mustValidateFail(t, schema, `"AB"`)
	mustValidateFail(t, schema, `"a1"`)

	// 多字节字符按 Unicode 码点计长（Draft-07 语义）。
	unicodeSchema := mustCompileSchema(t, `{"type":"string","minLength":2}`)
	mustValidateOK(t, unicodeSchema, `"检测"`)
}

// TestParamsSchemaAcceptsEmptySchema 空 schema 接受任意实例（对齐无 config.schema.json 的算法包）。
func TestParamsSchemaAcceptsEmptySchema(t *testing.T) {
	schema := mustCompileSchema(t, `{}`)
	mustValidateOK(t, schema, `{"anything":1}`)
	mustValidateOK(t, schema, `[1,2,3]`)
	mustValidateOK(t, schema, `"x"`)

	nilSchema := mustCompileSchema(t, ``)
	mustValidateOK(t, nilSchema, `42`)
}

// TestParamsSchemaInvalidInstanceJSON 非法 JSON 实例按校验失败处理。
func TestParamsSchemaInvalidInstanceJSON(t *testing.T) {
	schema := mustCompileSchema(t, `{"type":"object"}`)
	err := schema.Validate(json.RawMessage(`{bad`))
	if err == nil {
		t.Fatal("invalid json instance unexpectedly accepted")
	}
	if _, ok := err.(*SchemaValidationError); !ok {
		t.Fatalf("error type = %T, want *SchemaValidationError", err)
	}
}
