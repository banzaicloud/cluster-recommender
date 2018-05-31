package api

import (
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"gopkg.in/go-playground/validator.v8"
)

// NewValidator returns Validate with custom validations configured
func NewValidator(providers []string) *validator.Validate {
	v := validator.New(&validator.Config{TagName: "validate"})
	var providerString = fmt.Sprintf("^%s$", strings.Join(providers, "|"))
	var passwordRegex = regexp.MustCompile(providerString)
	v.RegisterValidation("provider_supported", func(v *validator.Validate, topStruct reflect.Value, currentStruct reflect.Value, field reflect.Value, fieldtype reflect.Type, fieldKind reflect.Kind, param string) bool {
		return passwordRegex.MatchString(field.String())
	})
	return v
}

// ValidatePathParam is a gin middleware handler function that validates a named path parameter with specific Validate tags
func ValidatePathParam(name string, validate *validator.Validate, tags ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		p := c.Param(name)
		for _, tag := range tags {
			err := validate.Field(p, tag)
			if err != nil {
				c.Abort()
				c.JSON(http.StatusBadRequest, map[string]interface{}{
					"code":    "bad_params",
					"message": fmt.Sprintf("invalid %s parameter", name),
					"params":  map[string]string{name: p},
				})
				return
			}
		}
	}
}
