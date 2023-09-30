package jsonschema

import "errors"

var (
	// ErrInvalidUniqueProperty is returned when the schema's unique property
	// is invalid
	ErrInvalidUniqueProperty = errors.New(`property "unique" is invalid`)

	// ErrUniqueConstrainViolation is returned when an object violates the unique
	// constrain
	ErrUniqueConstrainViolation = errors.New("unique constrain violation")
)
