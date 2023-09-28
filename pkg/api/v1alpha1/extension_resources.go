package v1alpha1

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/metal-toolbox/governor-api/internal/models"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/volatiletech/sqlboiler/v4/boil"
	"github.com/volatiletech/sqlboiler/v4/queries/qm"
)

// SystemExtensionResource is the system extension resource response
type SystemExtensionResource struct {
	*models.SystemExtensionResource
	ERD     string `json:"extension_resource_definition"`
	Version string `json:"version"`
}

// // SystemExtensionResourceReq is a request to create a system extension resource
// type SystemExtensionResourceReq struct {
// 	Resource json.RawMessage `json:"resource"`
// }

func findERDForExtensionResource(
	ctx context.Context, exec boil.ContextExecutor,
	extensionSlug, erdSlugPlural, erdVersion string,
) (extension *models.Extension, erd *models.ExtensionResourceDefinition, err error) {
	// fetch extension
	extensionQM := qm.Where("slug = ?", extensionSlug)

	// fetch ERD
	queryMods := []qm.QueryMod{
		qm.Where("slug_plural = ?", erdSlugPlural),
		qm.Where("version = ?", erdVersion),
	}

	extension, err = models.Extensions(
		extensionQM,
		qm.Load(
			models.ExtensionRels.ExtensionResourceDefinitions,
			queryMods...,
		),
	).One(ctx, exec)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrExtensionNotFound
		}

		return
	}

	if len(extension.R.ExtensionResourceDefinitions) < 1 {
		return nil, nil, ErrERDNotFound
	}

	erd = extension.R.ExtensionResourceDefinitions[0]

	return
}

// createSystemExtensionResource creates a system extension resource
func (r *Router) createSystemExtensionResource(c *gin.Context) {
	defer c.Request.Body.Close()

	extensionSlug := c.Param("ex-slug")
	erdSlugPlural := c.Param("erd-slug-plural")
	erdVersion := c.Param("erd-version")

	requestBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		sendError(c, http.StatusBadRequest, err.Error())
		return
	}

	// find ERD
	_, erd, err := findERDForExtensionResource(
		c.Request.Context(), r.DB,
		extensionSlug, erdSlugPlural, erdVersion,
	)
	if err != nil {
		if errors.Is(err, ErrExtensionNotFound) || errors.Is(err, ErrERDNotFound) {
			sendError(c, http.StatusNotFound, err.Error())
			return
		}

		sendError(c, http.StatusBadRequest, err.Error())

		return
	}

	if erd.Scope != ExtensionResourceDefinitionScopeSys {
		sendError(
			c, http.StatusBadRequest,
			fmt.Sprintf(
				"cannot create system resource for %s scoped %s/%s",
				erd.Scope, erd.SlugSingular, erd.Version,
			),
		)

		return
	}

	// schema validator
	schema, err := jsonschema.CompileString("https://governor/s.json", erd.Schema.String())
	if err != nil {
		sendError(c, http.StatusBadRequest, "ERD schema is not valid: "+err.Error())
		return
	}

	// validate payload
	var v interface{}
	if err := json.Unmarshal(requestBody, &v); err != nil {
		sendError(c, http.StatusBadRequest, "unable to bind request: "+err.Error())
		return
	}

	if err := schema.Validate(v); err != nil {
		sendError(c, http.StatusBadRequest, err.Error())
		return
	}

	// insert
	er := &models.SystemExtensionResource{Resource: requestBody}

	tx, err := r.DB.BeginTx(c.Request.Context(), nil)
	if err != nil {
		sendError(c, http.StatusBadRequest, "error starting extension create transaction: "+err.Error())
		return
	}

	if err := erd.AddSystemExtensionResources(c.Request.Context(), tx, true, er); err != nil {
		msg := fmt.Sprintf("error creating %s: %s", erd.Name, err.Error())

		if err := tx.Rollback(); err != nil {
			msg += fmt.Sprintf("error rolling back transaction: %s", err.Error())
		}

		sendError(c, http.StatusBadRequest, msg)

		return
	}

	if err := tx.Commit(); err != nil {
		msg := fmt.Sprintf("error committing extension create: %s", err.Error())

		if err := tx.Rollback(); err != nil {
			msg += fmt.Sprintf("error rolling back transaction: %s", err.Error())
		}

		sendError(c, http.StatusBadRequest, msg)

		return
	}

	resp := &SystemExtensionResource{
		SystemExtensionResource: er,
		ERD:                     erd.SlugSingular,
		Version:                 erd.Version,
	}

	c.JSON(http.StatusAccepted, resp)
}

// listSystemExtensionResource lists system extension resources for an ERD
func (r *Router) listSystemExtensionResource(c *gin.Context) {
	defer c.Request.Body.Close()

	extensionSlug := c.Param("ex-slug")
	erdSlugPlural := c.Param("erd-slug-plural")
	erdVersion := c.Param("erd-version")

	// find ERD
	_, erd, err := findERDForExtensionResource(
		c.Request.Context(), r.DB,
		extensionSlug, erdSlugPlural, erdVersion,
	)
	if err != nil {
		if errors.Is(err, ErrExtensionNotFound) || errors.Is(err, ErrERDNotFound) {
			sendError(c, http.StatusNotFound, err.Error())
			return
		}

		sendError(c, http.StatusBadRequest, err.Error())

		return
	}

	if erd.Scope != ExtensionResourceDefinitionScopeSys {
		sendError(
			c, http.StatusBadRequest,
			fmt.Sprintf(
				"cannot list system resources for %s scoped %s/%s",
				erd.Scope, erd.SlugSingular, erd.Version,
			),
		)

		return
	}

	uriQueries := map[string]string{}
	if err := c.BindQuery(&uriQueries); err != nil {
		sendError(
			c, http.StatusBadRequest,
			fmt.Sprintf("error binding uri queries: %s", err.Error()),
		)

		return
	}

	qms := make([]qm.QueryMod, 0, len(uriQueries))

	for k, v := range uriQueries {
		if k == "deleted" {
			qms = append(qms, qm.WithDeleted())
			continue
		}

		qms = append(qms, qm.Where("resource->? = ?", k, v))
	}

	ers, err := erd.SystemExtensionResources(qms...).All(c.Request.Context(), r.DB)
	if err != nil {
		sendError(
			c, http.StatusBadRequest,
			"error finding extension resources: "+err.Error(),
		)

		return
	}

	c.JSON(http.StatusOK, ers)
}
