package v1alpha1

import (
	"encoding/json"

	"github.com/gin-gonic/gin"
	"github.com/metal-toolbox/governor-api/internal/models"
)

// SystemExtensionResource is the system extension resource response
type SystemExtensionResource struct {
	*models.SystemExtensionResource
}

// SystemExtensionResourceReq is a request to create a system extension resource
type SystemExtensionResourceReq struct {
	Resource json.RawMessage `json:"resource"`
}

// createExtensionResource creates a system extension resource
func (r *Router) createSystemExtensionResource(c *gin.Context) {
}
