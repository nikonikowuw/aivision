package service

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// 受限 JSON Schema 校验器（自研，禁止引入第三方依赖）。
// 契约来源：.trellis/spec/engine/manifest-schema.md §3——
// 仅支持 Draft-07 的受限子集：
//   - 类型：object|array|string|number|integer|boolean；
//   - 关键字：required/properties/additionalProperties=false/items/
//     minimum/maximum/multipleOf/minItems/maxItems/uniqueItems/
//     minLength/maxLength/enum/pattern/default；
//   - 注解：title/description（仅声明，不参与校验）；
//   - 禁止：$ref/oneOf/anyOf/allOf/not/definitions/递归/x-* 等 UI 元数据。
//
// 对 schema 本身做严格合法性校验：未知关键字、禁用关键字、关键字形态错误
// 一律拒绝（对齐 manifest-schema「未知字段拒绝」精神）。
// 实例校验遵循 Draft-07 语义（additionalProperties 缺省放行未知字段、数值不做类型
// 强转、integer 要求整数值），pattern 用 Go 标准 regexp（RE2）。

// ParamSchema 受限 JSON Schema 编译后的校验器（纯数据，可复用、可并发读取）。
type ParamSchema struct {
	root *schemaNode
}

// schemaNode 单个 schema 节点的编译形态。Type 为空表示未声明（任意类型）。
type schemaNode struct {
	typ         string
	properties  map[string]*schemaNode
	allowExtras bool // additionalProperties 缺省时按 Draft-07 语义放行未知字段
	items       *schemaNode
	required    []string
	minimum     *float64
	maximum     *float64
	multipleOf  *float64
	minItems    *int64
	maxItems    *int64
	uniqueItems bool
	minLength   *int64
	maxLength   *int64
	enum        []any
	pattern     *regexp.Regexp
}

// SchemaCompileError 表示 schema 本身非法（未知/禁用关键字、关键字形态错误、
// pattern 不兼容 RE2 等）。service 层将其视为数据问题（内部错误）处理。
type SchemaCompileError struct {
	Path   string // 出错节点的 JSON 路径，根为 "$"
	Reason string
}

func (e *SchemaCompileError) Error() string {
	return fmt.Sprintf("invalid config schema at %s: %s", e.Path, e.Reason)
}

// SchemaValidationError 表示实例不满足 schema。service 层映射为 CodeInvalidParam。
type SchemaValidationError struct {
	Path   string
	Reason string
}

func (e *SchemaValidationError) Error() string {
	return fmt.Sprintf("params failed schema validation at %s: %s", e.Path, e.Reason)
}

// 受限子集允许出现的全部关键字与注解（"$schema" 为声明行，单独放行）。
var allowedSchemaKeywords = map[string]bool{
	"$schema": true,
	"type":    true, "properties": true, "additionalProperties": true,
	"required": true, "items": true,
	"minimum": true, "maximum": true, "multipleOf": true,
	"minItems": true, "maxItems": true, "uniqueItems": true,
	"minLength": true, "maxLength": true,
	"enum": true, "pattern": true, "default": true,
	"title": true, "description": true,
}

// 显式禁止的组合/引用关键字（manifest-schema.md §3）；禁用 $ref 后 schema 无环，
// 递归 schema 在结构上不可表达。
var bannedSchemaKeywords = []string{
	"$ref", "oneOf", "anyOf", "allOf", "not", "$id", "definitions", "$defs",
}

// 允许声明的六种实例类型。
var allowedSchemaTypes = map[string]bool{
	"object": true, "array": true, "string": true,
	"number": true, "integer": true, "boolean": true,
}

// CompileSchema 解析并校验受限 JSON Schema。raw 为空或 "{}" 时编译为
// 「接受任意实例」的 schema（对齐无 config.schema.json 的算法包）。
func CompileSchema(raw json.RawMessage) (*ParamSchema, error) {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, &SchemaCompileError{Path: "$", Reason: "schema is not valid JSON: " + err.Error()}
	}
	root, err := compileNode(doc, "$")
	if err != nil {
		return nil, err
	}
	return &ParamSchema{root: root}, nil
}

// Validate 校验实例是否符合本 schema。instance 必须先通过 json.Unmarshal 判定。
func (s *ParamSchema) Validate(instance json.RawMessage) error {
	if s == nil || s.root == nil {
		return &SchemaValidationError{Path: "$", Reason: "schema is empty"}
	}
	var value any
	if err := json.Unmarshal(instance, &value); err != nil {
		return &SchemaValidationError{Path: "$", Reason: "instance is not valid JSON: " + err.Error()}
	}
	return s.root.validate(value, "$")
}

// compileNode 递归编译一个 schema 节点：先做关键字白名单/黑名单与形态校验，
// 再做语义交叉校验（min<=max 等），最后编译 pattern。
func compileNode(doc any, path string) (*schemaNode, error) {
	obj, ok := doc.(map[string]any)
	if !ok {
		return nil, &SchemaCompileError{Path: path, Reason: "schema node must be a JSON object"}
	}
	node := &schemaNode{allowExtras: true} // Draft-07 缺省语义
	for key, value := range obj {
		switch {
		case key == "$schema":
			if _, ok := value.(string); !ok {
				return nil, &SchemaCompileError{Path: joinPath(path, key), Reason: "$schema must be a string"}
			}
			continue // 声明行，忽略
		case strings.HasPrefix(key, "x-"):
			return nil, &SchemaCompileError{Path: joinPath(path, key), Reason: "x-* UI metadata is forbidden"}
		case slices.Contains(bannedSchemaKeywords, key):
			return nil, &SchemaCompileError{Path: joinPath(path, key), Reason: "keyword is forbidden in restricted subset"}
		case !allowedSchemaKeywords[key]:
			return nil, &SchemaCompileError{Path: joinPath(path, key), Reason: "unknown keyword"}
		}

		var err error
		switch key {
		case "type":
			err = node.setType(value, joinPath(path, key))
		case "properties":
			err = node.setProperties(value, joinPath(path, key))
		case "additionalProperties":
			err = node.setAdditionalProperties(value, joinPath(path, key))
		case "items":
			err = node.setItems(value, joinPath(path, key))
		case "required":
			err = node.setRequired(value, joinPath(path, key))
		case "minimum":
			err = node.setNumberKeyword(value, joinPath(path, key), &node.minimum)
		case "maximum":
			err = node.setNumberKeyword(value, joinPath(path, key), &node.maximum)
		case "multipleOf":
			err = node.setMultipleOf(value, joinPath(path, key))
		case "minItems":
			err = node.setNonNegativeIntKeyword(value, joinPath(path, key), &node.minItems)
		case "maxItems":
			err = node.setNonNegativeIntKeyword(value, joinPath(path, key), &node.maxItems)
		case "minLength":
			err = node.setNonNegativeIntKeyword(value, joinPath(path, key), &node.minLength)
		case "maxLength":
			err = node.setNonNegativeIntKeyword(value, joinPath(path, key), &node.maxLength)
		case "uniqueItems":
			err = node.setBoolKeyword(value, joinPath(path, key), &node.uniqueItems)
		case "enum":
			err = node.setEnum(value, joinPath(path, key))
		case "pattern":
			err = node.setPattern(value, joinPath(path, key))
		case "title", "description":
			if _, ok := value.(string); !ok {
				err = &SchemaCompileError{Path: joinPath(path, key), Reason: "annotation must be a string"}
			}
		case "default":
			// 任意 JSON 值合法；仅前端表单用，不参与校验。
		}
		if err != nil {
			return nil, err
		}
	}

	// 语义交叉校验：上下界倒置的 schema 永远无法满足，编译期直接拒绝。
	if node.minimum != nil && node.maximum != nil && *node.minimum > *node.maximum {
		return nil, &SchemaCompileError{Path: path, Reason: "minimum must be <= maximum"}
	}
	if node.minItems != nil && node.maxItems != nil && *node.minItems > *node.maxItems {
		return nil, &SchemaCompileError{Path: path, Reason: "minItems must be <= maxItems"}
	}
	if node.minLength != nil && node.maxLength != nil && *node.minLength > *node.maxLength {
		return nil, &SchemaCompileError{Path: path, Reason: "minLength must be <= maxLength"}
	}
	return node, nil
}

func (n *schemaNode) setType(value any, path string) error {
	s, ok := value.(string)
	if !ok || !allowedSchemaTypes[s] {
		return &SchemaCompileError{Path: path, Reason: "type must be one of object|array|string|number|integer|boolean"}
	}
	n.typ = s
	return nil
}

func (n *schemaNode) setProperties(value any, path string) error {
	obj, ok := value.(map[string]any)
	if !ok {
		return &SchemaCompileError{Path: path, Reason: "properties must be an object"}
	}
	n.properties = make(map[string]*schemaNode, len(obj))
	for name, sub := range obj {
		child, err := compileNode(sub, joinPath(path, name))
		if err != nil {
			return err
		}
		n.properties[name] = child
	}
	return nil
}

func (n *schemaNode) setAdditionalProperties(value any, path string) error {
	b, ok := value.(bool)
	if !ok {
		return &SchemaCompileError{Path: path, Reason: "additionalProperties must be a boolean"}
	}
	if b {
		// 受限子集钉死 additionalProperties=false（manifest-schema.md §3），
		// 显式 true 说明 schema 偏离契约。
		return &SchemaCompileError{Path: path, Reason: "additionalProperties must be false"}
	}
	n.allowExtras = false
	return nil
}

func (n *schemaNode) setItems(value any, path string) error {
	sub, ok := value.(map[string]any)
	if !ok {
		return &SchemaCompileError{Path: path, Reason: "items must be a schema object (tuple form is not supported)"}
	}
	child, err := compileNode(sub, path)
	if err != nil {
		return err
	}
	n.items = child
	return nil
}

func (n *schemaNode) setRequired(value any, path string) error {
	arr, ok := value.([]any)
	if !ok {
		return &SchemaCompileError{Path: path, Reason: "required must be an array of strings"}
	}
	seen := make(map[string]bool, len(arr))
	for i, item := range arr {
		name, ok := item.(string)
		if !ok || name == "" || seen[name] {
			return &SchemaCompileError{Path: joinPath(path, fmt.Sprintf("[%d]", i)), Reason: "required entries must be unique non-empty strings"}
		}
		seen[name] = true
		n.required = append(n.required, name)
	}
	return nil
}

func (n *schemaNode) setNumberKeyword(value any, path string, target **float64) error {
	f, ok := value.(float64)
	if !ok {
		return &SchemaCompileError{Path: path, Reason: "keyword must be a number"}
	}
	*target = &f
	return nil
}

func (n *schemaNode) setMultipleOf(value any, path string) error {
	f, ok := value.(float64)
	if !ok || f <= 0 {
		return &SchemaCompileError{Path: path, Reason: "multipleOf must be a positive number"}
	}
	n.multipleOf = &f
	return nil
}

func (n *schemaNode) setNonNegativeIntKeyword(value any, path string, target **int64) error {
	f, ok := value.(float64)
	if !ok || f < 0 || f != math.Trunc(f) {
		return &SchemaCompileError{Path: path, Reason: "keyword must be a non-negative integer"}
	}
	v := int64(f)
	*target = &v
	return nil
}

func (n *schemaNode) setBoolKeyword(value any, path string, target *bool) error {
	b, ok := value.(bool)
	if !ok {
		return &SchemaCompileError{Path: path, Reason: "keyword must be a boolean"}
	}
	*target = b
	return nil
}

func (n *schemaNode) setEnum(value any, path string) error {
	arr, ok := value.([]any)
	if !ok || len(arr) == 0 {
		return &SchemaCompileError{Path: path, Reason: "enum must be a non-empty array"}
	}
	n.enum = arr
	return nil
}

func (n *schemaNode) setPattern(value any, path string) error {
	s, ok := value.(string)
	if !ok {
		return &SchemaCompileError{Path: path, Reason: "pattern must be a string"}
	}
	re, err := regexp.Compile(s)
	if err != nil {
		// Go regexp 是 RE2 语义：ECMA-262 的反向断言/反向引用等 pattern 无法编译。
		// 无法编译即无法校验，fail closed 拒绝该 schema。
		return &SchemaCompileError{Path: path, Reason: "pattern is not a valid RE2 regexp: " + err.Error()}
	}
	n.pattern = re
	return nil
}

// validate 按 Draft-07 语义校验实例节点；typ 为空表示任意类型。
func (n *schemaNode) validate(value any, path string) error {
	if err := n.checkType(value, path); err != nil {
		return err
	}
	switch v := value.(type) {
	case map[string]any:
		if err := n.validateObject(v, path); err != nil {
			return err
		}
	case []any:
		if err := n.validateArray(v, path); err != nil {
			return err
		}
	case string:
		if err := n.validateString(v, path); err != nil {
			return err
		}
	case float64:
		if err := n.validateNumber(v, path); err != nil {
			return err
		}
	case bool:
		// 无布尔专属关键字，仅 enum 可能命中（下方统一检查）。
	}
	return n.checkEnum(value, path)
}

// checkType 类型匹配：integer 要求数值为整（3.0 合法，3.5 非法），number 覆盖整数值。
func (n *schemaNode) checkType(value any, path string) error {
	if n.typ == "" {
		return nil
	}
	switch n.typ {
	case "object":
		if _, ok := value.(map[string]any); !ok {
			return n.typeError(value, path)
		}
	case "array":
		if _, ok := value.([]any); !ok {
			return n.typeError(value, path)
		}
	case "string":
		if _, ok := value.(string); !ok {
			return n.typeError(value, path)
		}
	case "number":
		if _, ok := value.(float64); !ok {
			return n.typeError(value, path)
		}
	case "integer":
		f, ok := value.(float64)
		if !ok || f != math.Trunc(f) {
			return n.typeError(value, path)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return n.typeError(value, path)
		}
	}
	return nil
}

func (n *schemaNode) typeError(value any, path string) error {
	return &SchemaValidationError{Path: path, Reason: fmt.Sprintf("value %s is not of type %s", jsonTypeOf(value), n.typ)}
}

func (n *schemaNode) validateObject(obj map[string]any, path string) error {
	for _, name := range n.required {
		if _, ok := obj[name]; !ok {
			return &SchemaValidationError{Path: path, Reason: fmt.Sprintf("missing required property %q", name)}
		}
	}
	for name, sub := range obj {
		child := n.properties[name]
		if child == nil {
			if !n.allowExtras {
				return &SchemaValidationError{Path: joinPath(path, name), Reason: "property not allowed by schema"}
			}
			continue
		}
		if err := child.validate(sub, joinPath(path, name)); err != nil {
			return err
		}
	}
	return nil
}

func (n *schemaNode) validateArray(arr []any, path string) error {
	if n.minItems != nil && int64(len(arr)) < *n.minItems {
		return &SchemaValidationError{Path: path, Reason: fmt.Sprintf("array has %d items, minItems %d", len(arr), *n.minItems)}
	}
	if n.maxItems != nil && int64(len(arr)) > *n.maxItems {
		return &SchemaValidationError{Path: path, Reason: fmt.Sprintf("array has %d items, maxItems %d", len(arr), *n.maxItems)}
	}
	for i, item := range arr {
		if n.items != nil {
			if err := n.items.validate(item, joinPath(path, fmt.Sprintf("[%d]", i))); err != nil {
				return err
			}
		}
		if n.uniqueItems {
			for j := 0; j < i; j++ {
				if reflect.DeepEqual(arr[i], arr[j]) {
					return &SchemaValidationError{Path: path, Reason: "array items must be unique"}
				}
			}
		}
	}
	return nil
}

func (n *schemaNode) validateString(s string, path string) error {
	length := int64(utf8.RuneCountInString(s)) // Draft-07 按 Unicode 码点计长
	if n.minLength != nil && length < *n.minLength {
		return &SchemaValidationError{Path: path, Reason: fmt.Sprintf("string length %d < minLength %d", length, *n.minLength)}
	}
	if n.maxLength != nil && length > *n.maxLength {
		return &SchemaValidationError{Path: path, Reason: fmt.Sprintf("string length %d > maxLength %d", length, *n.maxLength)}
	}
	if n.pattern != nil && !n.pattern.MatchString(s) {
		return &SchemaValidationError{Path: path, Reason: fmt.Sprintf("string does not match pattern %q", n.pattern.String())}
	}
	return nil
}

func (n *schemaNode) validateNumber(f float64, path string) error {
	if n.minimum != nil && f < *n.minimum {
		return &SchemaValidationError{Path: path, Reason: fmt.Sprintf("%v is below minimum %v", f, *n.minimum)}
	}
	if n.maximum != nil && f > *n.maximum {
		return &SchemaValidationError{Path: path, Reason: fmt.Sprintf("%v is above maximum %v", f, *n.maximum)}
	}
	if n.multipleOf != nil && !isMultipleOf(f, *n.multipleOf) {
		return &SchemaValidationError{Path: path, Reason: fmt.Sprintf("%v is not a multiple of %v", f, *n.multipleOf)}
	}
	return nil
}

// checkEnum 枚举按深度相等匹配（任意类型），Draft-07 语义。
func (n *schemaNode) checkEnum(value any, path string) error {
	if len(n.enum) == 0 {
		return nil
	}
	for _, candidate := range n.enum {
		if reflect.DeepEqual(candidate, value) {
			return nil
		}
	}
	return &SchemaValidationError{Path: path, Reason: "value is not in enum"}
}

// isMultipleOf 浮点容差的倍数判定：比值与最近整数的相对偏差 ≤ 1e-9。
func isMultipleOf(value, divisor float64) bool {
	quotient := value / divisor
	return math.Abs(quotient-math.Round(quotient)) <= 1e-9*math.Max(1, math.Abs(quotient))
}

// jsonTypeOf 返回实例值的 JSON 类型名（诊断用）。
func jsonTypeOf(value any) string {
	switch value.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	case nil:
		return "null"
	}
	return "unknown"
}

// joinPath 拼接 JSON 路径（根为 "$"，属性访问用 "."）。
func joinPath(parent, name string) string {
	if parent == "$" {
		return "$." + name
	}
	return parent + "." + name
}
