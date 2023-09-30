package jsonschema

import (
	"context"
	"fmt"
	"reflect"

	"github.com/metal-toolbox/governor-api/internal/models"
	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/volatiletech/sqlboiler/v4/boil"
	"github.com/volatiletech/sqlboiler/v4/queries/qm"
)

// JSONSchemaUniqueConstrain is a JSON schema extension that provides a
// "unique" property of type array
var JSONSchemaUniqueConstrain = jsonschema.MustCompileString(
	"https://governor/json-schemas/unique.json",
	`{
		"properties": {
			"unique": {
				"type": "array",
				"items": {
					"type": "string"
				}
			}
		}
	}`,
)

// UniqueConstrainSchema is the schema struct for the unique constrain JSON schema extension
type UniqueConstrainSchema struct {
	UniqueFieldTypesMap map[string]string
	ERD                 *models.ExtensionResourceDefinition
	ctx                 context.Context
	db                  boil.ContextExecutor
}

// UniqueConstrainSchema implements jsonschema.ExtSchema
var _ jsonschema.ExtSchema = (*UniqueConstrainSchema)(nil)

// Validate checks the uniqueness of the provided value against a database
// to ensure the unique constraint is satisfied.
func (s *UniqueConstrainSchema) Validate(_ jsonschema.ValidationContext, v interface{}) error {
	// Skip validation if no database is provided
	if s.db == nil {
		return nil
	}

	// Try to assert the provided value as a map, skip validation otherwise
	mappedValue, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}

	var qms []qm.QueryMod

	for key, value := range mappedValue {
		// Convert the key and value to string
		k := key
		v := fmt.Sprint(value)

		fieldType, exists := s.UniqueFieldTypesMap[k]
		if !exists {
			continue
		}

		if fieldType == "string" {
			v = fmt.Sprintf(`"%s"`, v)
		}

		qms = append(qms, qm.Where(`resource->? = ?`, k, v))
	}

	exists, err := s.ERD.SystemExtensionResources(qms...).Exists(s.ctx, s.db)
	if err != nil {
		return &jsonschema.ValidationError{
			Message: err.Error(),
		}
	}

	if exists {
		return &jsonschema.ValidationError{
			InstanceLocation: s.ERD.Name,
			KeywordLocation:  "unique",
			Message:          ErrUniqueConstrainViolation.Error(),
		}
	}

	return nil
}

// UniqueConstrainCompiler is the compiler struct for the unique constrain JSON schema extension
type UniqueConstrainCompiler struct {
	ERD *models.ExtensionResourceDefinition
	ctx context.Context
	db  boil.ContextExecutor
}

// UniqueConstrainCompiler implements jsonschema.ExtCompiler
var _ jsonschema.ExtCompiler = (*UniqueConstrainCompiler)(nil)

func (uc *UniqueConstrainCompiler) Compile(
	_ jsonschema.CompilerContext, m map[string]interface{},
) (jsonschema.ExtSchema, error) {
	unique, ok := m["unique"]
	if !ok {
		// If "unique" is not in the map, skip processing
		return nil, nil
	}

	uniqueFields, err := assertStringSlice(unique)
	if err != nil {
		return nil, err
	}

	if len(uniqueFields) == 0 {
		// unique property is not provided, skip
		return nil, nil
	}

	requiredFields, err := assertStringSlice(m["required"])
	if err != nil {
		return nil, err
	}

	propertiesMap, ok := m["properties"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf(
			`%w: cannot apply unique constraint when "properties" is not provided or invalid`,
			ErrInvalidUniqueProperty,
		)
	}

	return uc.compileUniqueConstrain(uniqueFields, requiredFields, propertiesMap)
}

func (uc *UniqueConstrainCompiler) compileUniqueConstrain(uniqueFields, requiredFields []string, propertiesMap map[string]interface{}) (jsonschema.ExtSchema, error) {
	requiredMap := make(map[string]bool, len(requiredFields))
	for _, f := range requiredFields {
		requiredMap[f] = true
	}

	resultUniqueFields := make(map[string]string)

	for _, fieldName := range uniqueFields {
		if !requiredMap[fieldName] {
			return nil, fmt.Errorf(
				`%w: unique property needs to be a required property, "%s" is not in "required"`,
				ErrInvalidUniqueProperty,
				fieldName,
			)
		}

		prop, ok := propertiesMap[fieldName]
		if !ok {
			return nil, fmt.Errorf(
				`%w: missing property definition for unique field "%s"`,
				ErrInvalidUniqueProperty,
				fieldName,
			)
		}

		fieldType, ok := prop.(map[string]interface{})["type"].(string)
		if !ok || !isValidType(fieldType) {
			return nil, fmt.Errorf(
				`%w: invalid type "%s" for unique field "%s"`,
				ErrInvalidUniqueProperty,
				fieldType,
				fieldName,
			)
		}

		resultUniqueFields[fieldName] = fieldType
	}

	return &UniqueConstrainSchema{resultUniqueFields, uc.ERD, uc.ctx, uc.db}, nil
}

// Checks if the provided field type is valid for unique constraints
func isValidType(fieldType string) bool {
	switch fieldType {
	case "string", "number", "integer", "boolean":
		return true
	default:
		return false
	}
}

// helper function to assert string slice type
func assertStringSlice(value interface{}) ([]string, error) {
	values, ok := value.([]interface{})
	if !ok {
		return nil, fmt.Errorf(
			`%w: unable to convert %v to string array`,
			ErrInvalidUniqueProperty,
			reflect.TypeOf(value),
		)
	}

	strs := make([]string, len(values))
	for i, v := range values {
		str, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf(
				`%w: unable to convert %v to string`,
				ErrInvalidUniqueProperty,
				reflect.TypeOf(v),
			)
		}
		strs[i] = str
	}

	return strs, nil
}
