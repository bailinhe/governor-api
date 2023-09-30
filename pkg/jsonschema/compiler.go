package jsonschema

import (
	"context"
	"fmt"
	"strings"

	"github.com/metal-toolbox/governor-api/internal/models"
	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/volatiletech/sqlboiler/v4/boil"
)

type Compiler struct {
	jsonschema.Compiler

	extensionID   string
	erdSlugPlural string
	version       string
}

// Option is a functional configuration option for JSON schema compiler
type Option func(c *Compiler)

// NewCompiler configures and creates a new JSON schema compiler
func NewCompiler(
	extensionID, slugPlural, version string,
	opts ...Option,
) *Compiler {
	c := &Compiler{*jsonschema.NewCompiler(), extensionID, slugPlural, version}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// WithUniqueConstrain enables the unique constrain extension for a JSON
// schema. An extra `unique` field can be added to the JSON schema, and the
// Validator will ensure that the combination of every properties in the
// array is unique within the given extension resource definition.
// Note that unique constrain validation will be skipped if db is nil.
func WithUniqueConstrain(
	ctx context.Context,
	extensionResourceDefinition *models.ExtensionResourceDefinition,
	db boil.ContextExecutor,
) Option {
	return func(c *Compiler) {
		c.RegisterExtension(
			"uniqueConstrain",
			JSONSchemaUniqueConstrain,
			&UniqueConstrainCompiler{extensionResourceDefinition, ctx, db},
		)
	}
}

func (c *Compiler) schemaURL() string {
	return fmt.Sprintf(
		"https://governor/extensions/%s/erds/%s/%s/schema.json",
		c.extensionID, c.erdSlugPlural, c.version,
	)
}

// Compile compiles the schema string
func (c *Compiler) Compile(schema string) (*jsonschema.Schema, error) {
	url := c.schemaURL()

	if err := c.AddResource(url, strings.NewReader(schema)); err != nil {
		return nil, err
	}

	return c.Compiler.Compile(url)
}
